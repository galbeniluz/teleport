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
	"os"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitational/teleport/lib/utils"
)

func TestMain(m *testing.M) {
	utils.InitLoggerForTests()

	os.Exit(m.Run())
}

type fakePinger struct {
	clock   clockwork.FakeClock
	latency time.Duration
	pingC   chan struct{}
}

func (f fakePinger) Ping(ctx context.Context) error {
	f.clock.Advance(f.latency)
	select {
	case f.pingC <- struct{}{}:
	default:
	}
	return nil
}

type fakeReporter struct {
	statsC chan Statistics
}

func (f fakeReporter) Report(ctx context.Context, stats Statistics) error {
	select {
	case f.statsC <- stats:
	default:
	}

	return nil
}

func TestMonitor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clock := clockwork.NewFakeClock()

	reporter := fakeReporter{
		statsC: make(chan Statistics, 20),
	}

	clientPinger := fakePinger{clock: clock, latency: 10 * time.Second, pingC: make(chan struct{}, 1)}
	serverPinger := fakePinger{clock: clock, latency: 5 * time.Second, pingC: make(chan struct{}, 1)}

	monitor, err := NewMonitor(MonitorConfig{
		ClientPinger:   clientPinger,
		ServerPinger:   serverPinger,
		Reporter:       reporter,
		Clock:          clock,
		PingInterval:   3 * time.Second,
		ReportInterval: 5 * time.Second,
	})
	require.NoError(t, err, "creating monitor")

	// Validate that stats are initially 0 for both legs.
	stats := monitor.GetStats()
	assert.Equal(t, Statistics{}, monitor.GetStats(), "expected initial latency stats to be zero got %v", stats)

	// Start the monitor in a goroutine since it's a blocking loop. The context
	// is terminated when the test ends.
	go func() {
		monitor.Run(ctx)
	}()

	for i := 0; i < 10; i++ {
		// Simulate the ping interval and wait to receive both pings before continuing.
		monitor.ticker.FireNow(pingKey)
		pingTimeout := time.After(15 * time.Second)
		for i := 0; i < 2; i++ {
			select {
			case <-clientPinger.pingC:
			case <-serverPinger.pingC:
			case <-pingTimeout:
				t.Fatal("ping never processed")
			}
		}

		// Simulate the reporting interval and validate the latencies reported
		// are not zero. The exact values are not compared as the mechanism to calculate
		// latency relies on advancing the fake clock which might happen twice prior to
		// the latency being recorded by the monitor due to both ping operations happening
		// simultaneously.
		monitor.ticker.FireNow(reportingKey)
		select {
		case reported := <-reporter.statsC:
			assert.NotEqual(t, stats, reported, "expected reported stats to have latency values")
			assert.NotEqual(t, stats, monitor.GetStats(), "expected retrieved stats to have latency values")
		case <-time.After(15 * time.Second):
			t.Fatal("latency stats never received")
		}
	}
}
