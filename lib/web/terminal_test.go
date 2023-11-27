/*
 * Teleport
 * Copyright (C) 2023  Gravitational, Inc.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package web_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"github.com/gorilla/websocket"
	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	tracessh "github.com/gravitational/teleport/api/observability/tracing/ssh"
	"github.com/gravitational/teleport/api/utils/retryutils"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/utils"
	"github.com/gravitational/teleport/lib/web"
)

// TestTerminalReadFromClosedConn verifies that Teleport recovers
// from a closed websocket connection.
// See https://github.com/gravitational/teleport/issues/21334
func TestTerminalReadFromClosedConn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var upgrader = websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("couldn't upgrade websocket connection: %v", err)
		}

		envelope := web.Envelope{
			Type:    defaults.WebsocketRaw,
			Payload: "hello",
		}
		b, err := proto.Marshal(&envelope)
		if err != nil {
			t.Errorf("could not marshal envelope: %v", err)
		}
		conn.WriteMessage(websocket.BinaryMessage, b)
	}))
	t.Cleanup(server.Close)

	u := strings.Replace(server.URL, "http:", "ws:", 1)
	conn, resp, err := websocket.DefaultDialer.Dial(u, nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	stream := web.NewTerminalStream(context.Background(), conn, utils.NewLoggerForTests())

	// close the stream before we attempt to read from it,
	// this will produce a net.ErrClosed error on the read
	require.NoError(t, stream.Close())

	_, err = io.Copy(io.Discard, stream)
	require.NoError(t, err)
}

type fakeSSHConn struct {
	latency    func() time.Duration
	replySentC chan struct{}
	clock      clockwork.FakeClock

	ssh.Conn
}

func (f fakeSSHConn) RemoteAddr() net.Addr {
	return &utils.NetAddr{
		Addr:        "127.0.0.1",
		AddrNetwork: "tcp",
	}
}

func (f fakeSSHConn) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	if !wantReply {
		return true, nil, nil
	}

	// advance the clock prior replying to introduce "latency" into the connection.
	f.clock.Advance(f.latency())
	f.replySentC <- struct{}{}
	return true, nil, nil
}

type pingHandler func(pingPayload string, pongHandler func(pongPayload string) error) error

type fakeWS struct {
	web.WSConn

	pingHandler pingHandler
	pongSentC   chan struct{}

	messages chan message
	pongFn   func(payload string) error
	latency  func() time.Duration
}

type message struct {
	mType int
	data  []byte
}

func (f *fakeWS) SetReadLimit(limit int64) {}

func (f *fakeWS) SetReadDeadline(t time.Time) error {
	return nil
}

func (f *fakeWS) SetPongHandler(h func(appData string) error) {
	f.pongFn = h
}

func (f *fakeWS) WriteControl(messageType int, data []byte, deadline time.Time) error {
	if messageType == websocket.PingMessage && f.pongFn != nil {
		if f.pingHandler != nil {
			err := f.pingHandler(string(data), f.pongFn)
			if err == nil {
				f.pongSentC <- struct{}{}
			}
			return trace.Wrap(err)
		}

		data := string(data)
		nanos, err := strconv.ParseInt(data, 10, 64)
		if err != nil {
			_ = f.pongFn(data)
			f.pongSentC <- struct{}{}
			return trace.Wrap(err, "parsing ping payload")
		}

		then := time.Unix(0, nanos)
		_ = f.pongFn(strconv.FormatInt(then.Add(-f.latency()).UnixNano(), 10))
		f.pongSentC <- struct{}{}

		return nil
	}

	if messageType == websocket.CloseMessage {
		return trace.Wrap(f.Close())
	}

	return nil
}

func (f *fakeWS) WriteMessage(messageType int, data []byte) error {
	f.messages <- message{
		mType: messageType,
		data:  data,
	}

	return nil
}

func (f *fakeWS) ReadMessage() (messageType int, p []byte, err error) {
	msg := <-f.messages
	return msg.mType, msg.data, nil
}

func TestSSHSessionMonitor(t *testing.T) {
	clock := clockwork.NewFakeClock()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	jitter := retryutils.NewFullJitter()
	latencyGenerator := func() time.Duration {
		return jitter(500 * time.Millisecond)
	}

	fakeWSConn := &fakeWS{
		latency:   latencyGenerator,
		pongSentC: make(chan struct{}, 1),
		messages:  make(chan message, 100),
	}
	latencyC := make(chan web.SSHSessionLatencyStats)
	latencyHandler := func(ctx context.Context, envelope web.Envelope) {
		var stats web.SSHSessionLatencyStats
		if err := json.Unmarshal([]byte(envelope.Payload), &stats); err == nil {
			latencyC <- stats
		}
	}

	fakeSSH := fakeSSHConn{
		latency:    latencyGenerator,
		replySentC: make(chan struct{}, 1),
		clock:      clock,
	}

	wsStream := web.NewWStream(ctx, fakeWSConn, utils.NewLoggerForTests(), map[string]web.WSHandlerFunc{defaults.WebsocketLatency: latencyHandler})
	sshClient := &tracessh.Client{Client: &ssh.Client{Conn: fakeSSH}}

	closedC := make(chan struct{})
	monitor, err := web.NewSSHSessionMonitor(web.SSHSessionMonitorConfig{
		WSStream:          wsStream,
		SSHClient:         sshClient,
		Clock:             clock,
		KeepAliveInterval: 10 * time.Second,
		PingInterval:      3 * time.Second,
		ReportInterval:    4 * time.Second,
		OnClose: func() error {
			close(closedC)
			return nil
		},
	})
	require.NoError(t, err, "creating session monitor")

	// Validate that stats are empty prior to the monitor sending any ping/keepalive messages.
	var zeroStats web.SSHSessionLatencyStats
	assert.Empty(t, cmp.Diff(zeroStats, monitor.GetStats()), "expected initial stats to be zero")

	// Start the monitor in another goroutine that is terminated when the
	// context is canceled.
	go func() {
		monitor.Run(ctx)
	}()

	// Run through a few intervals of collecting latency from both ends of the
	// connection and reporting gathered statistics.
	var missed int
	for i := 0; i < 100; i++ {
		// Advance the clock to initiate ssh keepalive messages and websocket pings
		clock.BlockUntil(1)
		clock.Advance(3 * time.Second)

		// Wait for a response to be sent
		for j := 0; j < 2; j++ {
			select {
			case <-fakeSSH.replySentC:
			case <-fakeWSConn.pongSentC:
			case <-closedC:
				t.Fatal("websocket connection was closed by server")
			}
		}

		// Advance the clock to initiate sending the latency statistics
		clock.BlockUntil(1)
		clock.Advance(1 * time.Second)

		// Wait for the latency measurement to be received. In the event,
		// something went wrong, and the websocket was closed then abort the test.
		// Allow some measurements to be missed since adjusting the time above
		// may not be perfect.
		select {
		case <-closedC:
			t.Fatal("websocket connection was closed by server")
		case stats := <-latencyC:
			// Validate that latency statistics are greater than zero.
			assert.NotEmpty(t, cmp.Diff(zeroStats, stats))
		default:
			missed++
		}

	}

	// Ensure that we received some latency metrics.
	require.NotEqual(t, 100, missed)
}

func TestTerminalIdle(t *testing.T) {
	cases := []struct {
		name              string
		keepAliveInterval time.Duration
		ping              pingHandler
		pingInterval      time.Duration
		closedAssertion   require.BoolAssertionFunc
	}{
		{
			name:              "OK - keep alive interval greater than ping interval",
			keepAliveInterval: 10 * time.Second,
			pingInterval:      3 * time.Second,
			closedAssertion:   require.False,
		},
		{
			name:              "OK -keep alive interval less than ping interval",
			keepAliveInterval: 1 * time.Second,
			pingInterval:      3 * time.Second,
			closedAssertion:   require.False,
		},
		{
			name:              "NOK - keep alive interval greater than ping interval",
			keepAliveInterval: 10 * time.Second,
			pingInterval:      3 * time.Second,
			ping: func(pingPayload string, pongHandler func(pongPayload string) error) error {
				return trace.ConnectionProblem(nil, "connection closed")
			},
			closedAssertion: require.True,
		},
		{
			name:              "NOK -keep alive interval less than ping interval",
			keepAliveInterval: 3 * time.Second,
			pingInterval:      5 * time.Second,
			ping: func(pingPayload string, pongHandler func(pongPayload string) error) error {
				return trace.ConnectionProblem(nil, "connection closed")
			},
			closedAssertion: require.True,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)

			latencyGenerator := func() time.Duration {
				return 0
			}

			fakeWSConn := &fakeWS{
				latency:     latencyGenerator,
				pongSentC:   make(chan struct{}, 1),
				pingHandler: test.ping,
				messages:    make(chan message, 100),
			}

			fakeSSH := fakeSSHConn{
				latency:    latencyGenerator,
				replySentC: make(chan struct{}, 1),
				clock:      clock,
			}

			wsStream := web.NewWStream(ctx, fakeWSConn, utils.NewLoggerForTests(), nil)
			sshClient := &tracessh.Client{Client: &ssh.Client{Conn: fakeSSH}}

			closedC := make(chan struct{})

			monitor, err := web.NewSSHSessionMonitor(web.SSHSessionMonitorConfig{
				WSStream:          wsStream,
				SSHClient:         sshClient,
				Clock:             clock,
				KeepAliveInterval: test.keepAliveInterval,
				PingInterval:      test.pingInterval,
				OnClose: func() error {
					close(closedC)
					return nil
				},
			})
			require.NoError(t, err, "creating session monitor")

			// Start the monitor in another goroutine that is terminated when the
			// context is canceled.
			go func() {
				monitor.Run(ctx)
			}()

			var closed bool

			step := min(test.pingInterval, test.keepAliveInterval)
			// Advance the clock more than enough times to trip the keepalive failure scenario.
			for i := 0; i < 15 && !closed; i++ {
				clock.BlockUntil(3)
				clock.Advance(step)

				select {
				case <-fakeWSConn.pongSentC:
				case <-closedC:
					closed = true
				case <-time.After(15 * time.Second):
					t.Fatal("timeout waiting for websocket activity")
				}
			}
			test.closedAssertion(t, closed)
		})
	}
}
