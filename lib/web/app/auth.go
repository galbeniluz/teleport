/*
Copyright 2022 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package app

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gravitational/trace"
	"github.com/julienschmidt/httprouter"

	"github.com/gravitational/teleport/api/types"
	apievents "github.com/gravitational/teleport/api/types/events"
	"github.com/gravitational/teleport/lib/auth"
	"github.com/gravitational/teleport/lib/events"
	"github.com/gravitational/teleport/lib/httplib"
	"github.com/gravitational/teleport/lib/utils"
)

type fragmentRequest struct {
	StateValue         string `json:"state_value"`
	CookieValue        string `json:"cookie_value"`
	SubjectCookieValue string `json:"subject_cookie_value"`
}

// startAppAuthExchange will do two actions depending on the following:
//
//	1): On initiating auth exchange (indicated by an empty "state" query param)
//	    we create a crypto safe random token and send it back as part of a "state"
//	    query param in the redirection URL, as well as in a cookie with attributes
//	    that makes the cookie unaccesible and hard to tamper with. We use this
//	    "double submit cookie" method to protect the entire auth exchange flow
//	    from CSRF.
//
//	2): If the "state" query param is present, we will serve a blank HTML page
//	    that has inline JS that contains logic to complete the auth exchange.
func (h *Handler) startAppAuthExchange(w http.ResponseWriter, r *http.Request, p httprouter.Params) error {
	q := r.URL.Query()

	// Initiate auth exchange.
	if q.Get("state") == "" {
		// secretToken is the token we will look for in both the cookie
		// and in the request "state" query param.
		secretToken, err := utils.CryptoRandomHex(auth.TokenLenBytes)
		if err != nil {
			h.log.WithError(err).Debugf("Failed to generate and encode random numbers.")
			return trace.AccessDenied("access denied")
		}

		// cookieIdentifier is used to uniquely identify this cookie
		// that will be used to store this secret token.
		//
		// This prevents a race condition (state token mismatch error)
		// where we can overwrite existing cookie (with the same name) with a
		// different token value eg: launch app in multiple tabs in quick succession
		cookieIdentifier, err := utils.CryptoRandomHex(auth.TokenLenBytes)
		if err != nil {
			h.log.WithError(err).Debugf("Failed to generate and encode random numbers.")
			return trace.AccessDenied("access denied")
		}

		h.setAuthStateCookie(w, secretToken, cookieIdentifier)

		webLauncherURLParams := launcherURLParams{
			clusterName: q.Get("cluster"),
			publicAddr:  q.Get("addr"),
			arn:         q.Get("arn"),
			path:        q.Get("path"),
			// The state token concats both the secret token and the cookie ID.
			// The server will break this token to its individual parts:
			//   - secretToken to compare against the one stored in cookie
			//   - cookieIdentifier to look up cookie sent by browser.
			stateToken: fmt.Sprintf("%s_%s", secretToken, cookieIdentifier),
		}
		return h.redirectToLauncher(w, r, webLauncherURLParams)
	}

	// Continue the auth exchange.

	nonce, err := utils.CryptoRandomHex(auth.TokenLenBytes)
	if err != nil {
		h.log.WithError(err).Debugf("Failed to generate and encode random numbers.")
		return trace.AccessDenied("access denied")
	}
	SetRedirectPageHeaders(w.Header(), nonce)

	// Serving the HTML page.
	fmt.Fprintf(w, appRedirectionJs, nonce)
	return nil
}

// completeAppAuthExchange completes the auth exchange flow started by "startAppAuthExchange" handler
// by validating the values passed in the request body, and upon success sets cookies required
// for the current app session. User should be able to interact with the app now.
func (h *Handler) completeAppAuthExchange(w http.ResponseWriter, r *http.Request, p httprouter.Params) error {
	httplib.SetNoCacheHeaders(w.Header())
	var req fragmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return trace.Wrap(err)
	}

	tokens := strings.Split(req.StateValue, "_")
	if len(tokens) != 2 {
		h.log.Warn("Request failed: request state token is not in the expected format")
		return trace.AccessDenied("access denied")
	}
	secretToken := tokens[0]
	cookieID := tokens[1]

	// Validate that the caller-provided state token matches the stored state token (CSRF check)
	stateCookie, err := r.Cookie(getAuthStateCookieName(cookieID))
	if err != nil || stateCookie.Value == "" {
		h.log.Warn("Request failed: state cookie is not set.")
		return trace.AccessDenied("access denied")
	}
	if subtle.ConstantTimeCompare([]byte(secretToken), []byte(stateCookie.Value)) != 1 {
		h.log.Warn("Request failed: state token does not match.")
		return trace.AccessDenied("access denied")
	}

	// Prevent reuse of the same state token.
	clearAuthStateCookie(w, cookieID)

	// Validate that the caller is asking for a session that exists and that they have the secret
	// session token for.
	ws, err := h.c.AccessPoint.GetAppSession(r.Context(), types.GetAppSessionRequest{
		SessionID: req.CookieValue,
	})
	if err != nil {
		h.log.Warn("Request failed: session does not exist.")
		return trace.AccessDenied("access denied")
	}
	if err := checkSubjectToken(req.SubjectCookieValue, ws); err != nil {
		h.log.Warnf("Request failed: %v.", err)
		h.c.AuthClient.EmitAuditEvent(h.closeContext, &apievents.AuthAttempt{
			Metadata: apievents.Metadata{
				Type: events.AuthAttemptEvent,
				Code: events.AuthAttemptFailureCode,
			},
			UserMetadata: apievents.UserMetadata{
				Login: ws.GetUser(),
				User:  "unknown",
			},
			ConnectionMetadata: apievents.ConnectionMetadata{
				LocalAddr:  r.Host,
				RemoteAddr: r.RemoteAddr,
			},
			Status: apievents.Status{
				Success: false,
				Error:   err.Error(),
			},
		})
		return trace.AccessDenied("access denied")
	}

	// Set the "Set-Cookie" header on the response.
	// Set Same-Site policy for the session cookies to None in order to
	// support redirects that identity providers do during SSO auth.
	// Otherwise the session cookie won't be sent and the user will
	// get redirected to the application launcher.
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Set-Cookie/SameSite
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    req.CookieValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     SubjectCookieName,
		Value:    ws.GetBearerToken(),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
	})

	return nil
}

func checkSubjectToken(subjectCookieValue string, ws types.WebSession) error {
	if subjectCookieValue == "" {
		return trace.AccessDenied("subject session token is not set")
	}
	if subtle.ConstantTimeCompare([]byte(subjectCookieValue), []byte(ws.GetBearerToken())) != 1 {
		return trace.AccessDenied("subject session token does not match")
	}
	return nil
}

func (h *Handler) setAuthStateCookie(w http.ResponseWriter, cookieValue string, cookieID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     getAuthStateCookieName(cookieID),
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
		MaxAge:   60, // Expire in 1 minute.
	})
}

func clearAuthStateCookie(w http.ResponseWriter, cookieID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     getAuthStateCookieName(cookieID),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteNoneMode,
		MaxAge:   -1,
	})
}

func getAuthStateCookieName(cookieID string) string {
	return fmt.Sprintf("%s_%s", AuthStateCookieName, cookieID)
}
