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

package latency

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	"github.com/sirupsen/logrus"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/api/utils/retryutils"
	"github.com/gravitational/teleport/lib/utils/interval"
)

var log = logrus.WithField(trace.Component, "latency")

// Statistics contain latency measurements for both
// legs of a proxied connection.
type Statistics struct {
	// Client measures the round trip time between the client and the Proxy.
	Client int64
	// Server measures the round trip time the Proxy and the target host.
	Server int64
}

// Reporter is an abstraction over how to provide the latency statistics to
// the consumer. Used by the Monitor to provide periodic latency updates.
type Reporter interface {
	Report(ctx context.Context, statistics Statistics) error
}

// ReporterFunc type is an adapter to allow the use of
// ordinary functions as a Reporter. If f is a function
// with the appropriate signature, Reporter(f) is a
// Reporter that calls f.
type ReporterFunc func(ctx context.Context, stats Statistics) error

// Report calls f(ctx, stats).
func (f ReporterFunc) Report(ctx context.Context, stats Statistics) error {
	return f(ctx, stats)
}

// Pinger abstracts the mechanism used to measure the round trip time of
// a connection. All "ping" messages should be responded to before returning
// from [Pinger.Ping].
type Pinger interface {
	Ping(ctx context.Context) error
}

// Monitor periodically pings both legs of a proxied connection and records
// the round trip times so that they may be emitted to consumers.
type Monitor struct {
	clientPinger  Pinger
	serverPinger  Pinger
	reporter      Reporter
	clock         clockwork.Clock
	ticker        *interval.MultiInterval[string]
	clientLatency atomic.Int64
	serverLatency atomic.Int64
}

// MonitorConfig provides required dependencies for the [Monitor].
type MonitorConfig struct {
	// ClientPinger measure the round trip time for client half of the connection.
	ClientPinger Pinger
	// ServerPinger measure the round trip time for server half of the connection.
	ServerPinger Pinger
	// Reporter periodically emits statistics to consumers.
	Reporter Reporter
	// Clock used to measure time.
	Clock clockwork.Clock
	// PingInterval is the frequency at which both legs of the connection are pinged for
	// latency calculations.
	PingInterval time.Duration
	// ReportInterval is the frequency at which the latency information is reported.
	ReportInterval time.Duration
}

// CheckAndSetDefaults ensures required fields are provided and sets
// default values for any omitted optional fields.
func (c *MonitorConfig) CheckAndSetDefaults() error {
	if c.ClientPinger == nil {
		return trace.BadParameter("client pinger not provided to MonitorConfig")
	}

	if c.ServerPinger == nil {
		return trace.BadParameter("server pinger not provided to MonitorConfig")
	}

	if c.Reporter == nil {
		return trace.BadParameter("reporter not provided to MonitorConfig")
	}

	if c.PingInterval <= 0 {
		c.PingInterval = 8 * time.Second
	}

	if c.ReportInterval <= 0 {
		c.ReportInterval = 10 * time.Second
	}

	if c.Clock == nil {
		c.Clock = clockwork.NewRealClock()
	}

	return nil
}

const (
	pingKey      = "ping-ticker"
	reportingKey = "reporting-ticker"
)

var (
	seventhJitter = retryutils.NewShardedSeventhJitter()
	fullJitter    = retryutils.NewShardedFullJitter()
	halfJitter    = retryutils.NewShardedHalfJitter()
)

// NewMonitor creates an unstarted [Monitor] with the provided configuration. To
// begin sampling connection latencies [Monitor.Run] must be called.
func NewMonitor(cfg MonitorConfig) (*Monitor, error) {
	if err := cfg.CheckAndSetDefaults(); err != nil {
		return nil, trace.Wrap(err)
	}

	ticker := interval.NewMulti(
		cfg.Clock,
		interval.SubInterval[string]{
			Key:           pingKey,
			FirstDuration: fullJitter(500 * time.Millisecond),
			Jitter:        seventhJitter,
			Duration:      cfg.PingInterval,
		},
		interval.SubInterval[string]{
			Key:           reportingKey,
			FirstDuration: halfJitter(1500 * time.Millisecond),
			Jitter:        seventhJitter,
			Duration:      cfg.ReportInterval,
		},
	)

	return &Monitor{
		clientPinger: cfg.ClientPinger,
		serverPinger: cfg.ServerPinger,
		reporter:     cfg.Reporter,
		ticker:       ticker,
		clock:        cfg.Clock,
	}, nil
}

// GetStats returns a copy of the last known latency measurements.
func (m *Monitor) GetStats() Statistics {
	return Statistics{
		Client: m.clientLatency.Load(),
		Server: m.serverLatency.Load(),
	}
}

// Run periodically records round trip times. It should not be called
// more than once.
func (m *Monitor) Run(ctx context.Context) {
	defer m.ticker.Stop()

	clientC, serverC := make(chan time.Time, 1), make(chan time.Time, 1)
	go m.pingLoop(ctx, clientC, m.clientPinger, &m.clientLatency)
	go m.pingLoop(ctx, serverC, m.serverPinger, &m.serverLatency)

	for {
		select {
		case tick := <-m.ticker.Next():
			switch tick.Key {
			case pingKey:
				// Ping the client
				select {
				case clientC <- tick.Time:
				case <-ctx.Done():
					return
				default:
				}

				// Ping the server
				select {
				case serverC <- tick.Time:
				case <-ctx.Done():
					return
				default:
				}
			case reportingKey:
				if err := m.reporter.Report(ctx, m.GetStats()); err != nil {
					log.WithError(err).Warn("failed to report latency stats")
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (m *Monitor) pingLoop(ctx context.Context, pingC <-chan time.Time, pinger Pinger, latency *atomic.Int64) {
	for {
		select {
		case <-ctx.Done():
		case then := <-pingC:
			if err := pinger.Ping(ctx); err != nil {
				log.WithError(err).Warn("unexpected failure sending ping")
			} else {
				latency.Store(m.clock.Now().Sub(then).Milliseconds())
			}
		}
	}
}

// SSHClient is the subset of the [ssh.Client] required by the [SSHPinger].
type SSHClient interface {
	SendRequest(ctx context.Context, name string, wantReply bool, payload []byte) (bool, []byte, error)
}

// SSHPinger is a [Pinger] implementation that measures the latency of an
// SSH connection. To calculate round trip time, a keepalive@openssh.com request
// is sent.
type SSHPinger struct {
	clt   SSHClient
	clock clockwork.Clock
}

// NewSSHPinger creates a new [SSHPinger] with the provided configuration.
func NewSSHPinger(clock clockwork.Clock, clt SSHClient) (*SSHPinger, error) {
	if clt == nil {
		return nil, trace.BadParameter("ssh client not provided to SSHPinger")
	}

	if clock == nil {
		clock = clockwork.NewRealClock()
	}

	return &SSHPinger{
		clt:   clt,
		clock: clock,
	}, nil
}

// Ping sends a keepalive@openssh.com request via the provided [SSHClient].
func (s *SSHPinger) Ping(ctx context.Context) error {
	_, _, err := s.clt.SendRequest(ctx, teleport.KeepAliveReqType, true, nil)
	return trace.Wrap(err, "sending request %s", teleport.KeepAliveReqType)
}

// WebSocket is the subset of [websocket.Conn] required by the [WebSocketPinger].
type WebSocket interface {
	WriteControl(messageType int, data []byte, deadline time.Time) error
	PongHandler() func(appData string) error
	SetPongHandler(h func(appData string) error)
}

// WebSocketPinger is a [Pinger] implementation that measures the latency of a
// websocket connection.
type WebSocketPinger struct {
	ws    WebSocket
	pongC chan string
	clock clockwork.Clock
}

// NewWebsocketPinger creates a [WebSocketPinger] with the provided configuration.
func NewWebsocketPinger(clock clockwork.Clock, ws WebSocket) (*WebSocketPinger, error) {
	if ws == nil {
		return nil, trace.BadParameter("web socket not provided to WebSocketPinger")
	}

	if clock == nil {
		clock = clockwork.NewRealClock()
	}

	pinger := &WebSocketPinger{
		ws:    ws,
		clock: clock,
		pongC: make(chan string, 1),
	}

	handler := ws.PongHandler()
	ws.SetPongHandler(func(payload string) error {
		select {
		case pinger.pongC <- payload:
		default:
		}

		if handler == nil {
			return nil
		}

		return trace.Wrap(handler(payload))
	})

	return pinger, nil
}

// Ping writes a ping control message and waits for the corresponding pong control message
// to be received before returning. The random identifier in the ping message is expected
// to be returned in the pong payload so that we can determine the true round trip time for
// a ping/pong message pair.
func (s *WebSocketPinger) Ping(ctx context.Context) error {
	// websocketPingMessage denotes a ping control message.
	const websocketPingMessage = 9

	payload := uuid.NewString()
	deadline := s.clock.Now().Add(2 * time.Second)
	if err := s.ws.WriteControl(websocketPingMessage, []byte(payload), deadline); err != nil {
		return trace.Wrap(err, "sending ping message")
	}

	for {
		select {
		case pong := <-s.pongC:
			if pong == payload {
				return nil
			}
		case <-ctx.Done():
			return trace.Wrap(ctx.Err())
		}
	}
}
