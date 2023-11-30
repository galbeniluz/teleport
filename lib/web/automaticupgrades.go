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

func (h *Handler) automaticUpgrades(w http.ResponseWriter, r *http.Request, p httprouter.Params) (interface{}, error) {
	if h.cfg.AutomaticUpgradesChannels == nil {
		return nil, trace.AccessDenied("This proxy is not configured to server automatic upgrades channels.")
	}

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

	channel, ok := h.cfg.AutomaticUpgradesChannels[channelName]
	if !ok {
		return nil, trace.NotFound("channel %s not found", channelName)
	}

	switch requestType {
	case "version":
		h.log.Debug("Agent requesting version for channel %s", channelName)
		return h.automaticUpgradesVersion(w, r, channel)
	case "critical":
		h.log.Debug("Agent requesting criticality for channel %s", channelName)
		return h.automaticUpgradesCritical(w, r, channel)
	default:
		return nil, trace.BadParameter("requestType path must end by 'version' or 'critical'")
	}
}

func (h *Handler) automaticUpgradesVersion(w http.ResponseWriter, r *http.Request, channel *automaticupgrades.Channel) (interface{}, error) {
	ctx, cancel := context.WithTimeout(r.Context(), defaultChannelTimeout)
	defer cancel()

	targetVersion, err := channel.GetVersion(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	targetMajor, err := parseMajorFromVersionString(targetVersion)
	if err != nil {
		return nil, trace.Wrap(err, "failed to process target version")
	}

	teleportMajor, err := parseMajorFromVersionString(teleport.Version)
	if err != nil {
		return nil, trace.Wrap(err, "failed to process teleport version")
	}

	if targetMajor > teleportMajor {
		h.log.Debug("Client hit channel %s, target version (%s) major is above the proxy major (%s). Ignoring update.")
		return nil, nil
	}

	_, err = w.Write([]byte(targetVersion))
	return nil, trace.Wrap(err)
}

func (h *Handler) automaticUpgradesCritical(w http.ResponseWriter, r *http.Request, channel *automaticupgrades.Channel) (interface{}, error) {
	ctx, cancel := context.WithTimeout(r.Context(), defaultChannelTimeout)
	defer cancel()

	critical, err := channel.GetCritical(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}

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
