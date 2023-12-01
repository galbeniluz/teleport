package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/google/uuid"
	"github.com/gravitational/teleport/integration/helpers"
	"github.com/gravitational/teleport/integrations/kube-agent-updater/pkg/basichttp"
	"github.com/gravitational/teleport/integrations/kube-agent-updater/pkg/constants"
	"github.com/gravitational/teleport/lib/automaticupgrades"
	"github.com/gravitational/teleport/lib/service/servicecfg"
	"github.com/gravitational/teleport/lib/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
)

func createProxyWithChannels(t *testing.T, channels automaticupgrades.Channels) string {
	require.NoError(t, channels.CheckAndSetDefaults())
	testDir := t.TempDir()

	cfg := helpers.InstanceConfig{
		ClusterName: "root.example.com",
		HostID:      uuid.New().String(),
		NodeName:    helpers.Loopback,
		Log:         utils.NewLoggerForTests(),
	}
	cfg.Listeners = helpers.SingleProxyPortSetup(t, &cfg.Fds)
	rc := helpers.NewInstance(t, cfg)

	var err error
	rcConf := servicecfg.MakeDefaultConfig()
	rcConf.DataDir = filepath.Join(testDir, "data")
	rcConf.Auth.Enabled = true
	rcConf.Proxy.Enabled = true
	rcConf.SSH.Enabled = false
	rcConf.Proxy.DisableWebInterface = true
	rcConf.Version = "v3"
	rcConf.Proxy.AutomaticUpgradesChannels = channels

	err = rc.CreateEx(t, nil, rcConf)
	require.NoError(t, err)
	err = rc.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, rc.StopAll())
	})

	return cfg.Listeners.Web
}

func TestVersionServer(t *testing.T) {
	// Test setup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testVersion := "v12.2.6"
	testVersionMajorTooHigh := "v99.1.3"

	staticChannel := "static/ok"
	staticHighChannel := "static/high"
	forwardChannel := "forward/ok"
	forwardHighChannel := "forward/high"
	forwardPath := "/version-server/"

	upstreamServer := basichttp.NewServerMock(forwardPath + constants.VersionPath)
	upstreamServer.SetResponse(t, http.StatusOK, testVersion)
	upstreamHighServer := basichttp.NewServerMock(forwardPath + constants.VersionPath)
	upstreamHighServer.SetResponse(t, http.StatusOK, testVersionMajorTooHigh)

	channels := automaticupgrades.Channels{
		staticChannel: {
			StaticVersion: testVersion,
		},
		staticHighChannel: {
			StaticVersion: testVersionMajorTooHigh,
		},
		forwardChannel: {
			ForwardURL: upstreamServer.Srv.URL + forwardPath,
		},
		forwardHighChannel: {
			ForwardURL: upstreamHighServer.Srv.URL + forwardPath,
		},
	}

	proxyAddr := createProxyWithChannels(t, channels)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := http.Client{Transport: tr}

	tests := []struct {
		name             string
		channel          string
		expectedResponse string
	}{
		{
			name:             "static version OK",
			channel:          staticChannel,
			expectedResponse: testVersion,
		},
		{
			name:             "static version too high",
			channel:          staticHighChannel,
			expectedResponse: "",
		},
		{
			name:             "forward version OK",
			channel:          forwardChannel,
			expectedResponse: testVersion,
		},
		{
			name:             "forward version too high",
			channel:          forwardHighChannel,
			expectedResponse: "",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			channelUrl, err := url.Parse(
				fmt.Sprintf("https://%s/webapi/automaticupgrades/%s/version", proxyAddr, tt.channel),
			)
			require.NoError(t, err)

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, channelUrl.String(), nil)
			require.NoError(t, err)
			res, err := httpClient.Do(req)
			require.NoError(t, err)
			defer res.Body.Close()

			body, err := io.ReadAll(res.Body)
			require.NoError(t, err)

			require.Equal(t, http.StatusOK, res.StatusCode)
			require.Equal(t, tt.expectedResponse, string(body))
		})
	}
}
