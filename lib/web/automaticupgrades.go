// Copyright 2023 Gravitational, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gravitational/trace"
	"github.com/julienschmidt/httprouter"
	"golang.org/x/mod/semver"

	"github.com/gravitational/teleport"
	versionlib "github.com/gravitational/teleport/integrations/kube-agent-updater/pkg/version"
	"github.com/gravitational/teleport/lib/automaticupgrades"
)

const defaultChannelTimeout = 5 * time.Second

// automaticUpgrades implements a version server in the Teleport Proxy.
// It is configured through the Teleport Proxy configuration and tells agent updaters
// which version they should install.
func (h *Handler) automaticUpgrades(w http.ResponseWriter, r *http.Request, p httprouter.Params) (interface{}, error) {
	if h.cfg.AutomaticUpgradesChannels == nil {
		return nil, trace.AccessDenied("This proxy is not configured to server automatic upgrades channels.")
	}

	// The request format is "<channel name>/{version,critical}"
	// As <channel name> might contain "/" we have to split, pop the last part
	// and re-construct the channel name.
	channelAndType := p.ByName("request")

	reqParts := strings.Split(strings.Trim(channelAndType, "/"), "/")
	if len(reqParts) < 2 {
		return nil, trace.BadParameter("path format should be /webapi/automaticupgrades/<channel>/{version,critical}")
	}
	requestType := reqParts[len(reqParts)-1]
	channelName := strings.Join(reqParts[:len(reqParts)-1], "/")

	if channelName == "" {
		return nil, trace.BadParameter("a channel name is required")
	}

	// We check if the channel is configured
	channel, ok := h.cfg.AutomaticUpgradesChannels[channelName]
	if !ok {
		return nil, trace.NotFound("channel %s not found", channelName)
	}

	// Finally, we treat the request based on its type
	switch requestType {
	case "version":
		h.log.Debugf("Agent requesting version for channel %s", channelName)
		return h.automaticUpgradesVersion(w, r, channel)
	case "critical":
		h.log.Debugf("Agent requesting criticality for channel %s", channelName)
		return h.automaticUpgradesCritical(w, r, channel)
	default:
		return nil, trace.BadParameter("requestType path must end by 'version' or 'critical'")
	}
}

// automaticUpgradesVersion handles version requests from upgraders
func (h *Handler) automaticUpgradesVersion(w http.ResponseWriter, r *http.Request, channel *automaticupgrades.Channel) (interface{}, error) {
	ctx, cancel := context.WithTimeout(r.Context(), defaultChannelTimeout)
	defer cancel()

	targetVersion, err := channel.GetVersion(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	// We don't want to tell the updater to upgrade to a new major we don't support yet
	// This is mainly a workaround for Teleport Cloud and might be removed
	// In the future when we'll have better tooling to control version channels.
	targetMajor, err := parseMajorFromVersionString(targetVersion)
	if err != nil {
		return nil, trace.Wrap(err, "failed to process target version")
	}

	teleportMajor, err := parseMajorFromVersionString(teleport.Version)
	if err != nil {
		return nil, trace.Wrap(err, "failed to process teleport version")
	}

	if targetMajor > teleportMajor {
		// TODO: improve the way updaters handle an empty response
		h.log.Debugf("Client hit channel %s, target version (%s) major is above the proxy major (%s). Ignoring update.")
		return nil, nil
	}

	_, err = w.Write([]byte(targetVersion))
	return nil, trace.Wrap(err)
}

// automaticUpgradesCritical handles criticality requests from upgraders
func (h *Handler) automaticUpgradesCritical(w http.ResponseWriter, r *http.Request, channel *automaticupgrades.Channel) (interface{}, error) {
	ctx, cancel := context.WithTimeout(r.Context(), defaultChannelTimeout)
	defer cancel()

	critical, err := channel.GetCritical(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	// TODO: check if true/false is OK or if we should use yes/no
	_, err = w.Write([]byte(strconv.FormatBool(critical)))
	return nil, trace.Wrap(err)
}

func parseMajorFromVersionString(version string) (int, error) {
	version, err := versionlib.EnsureSemver(version)
	if err != nil {
		return 0, trace.Wrap(err, "invalid semver: %s", version)
	}
	majorStr := semver.Major(version)
	if majorStr == "" {
		return 0, trace.BadParameter("cannot detect version major")
	}

	major, err := strconv.Atoi(strings.TrimPrefix(majorStr, "v"))
	return major, trace.Wrap(err, "cannot convert version major to int")
}
