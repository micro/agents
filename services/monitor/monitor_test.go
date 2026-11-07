package monitor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/micro/studio/services/systemd"
	"go-micro.dev/v6/broker"
	"go-micro.dev/v6/client"
	"go-micro.dev/v6/registry"
	"go-micro.dev/v6/server"
)

func fixture(t *testing.T) *Monitor {
	t.Helper()
	host, err := systemd.New([]string{"micro-support.service"})
	if err != nil {
		t.Fatal(err)
	}
	return New(host)
}

func TestObservationsAndHistory(t *testing.T) {
	m := fixture(t)
	content := "LoadState=loaded\nActiveState=active\n"
	var readErr error
	m.read = func(_ context.Context, req *systemd.StatusRequest, rsp *systemd.Result) error {
		if req.Service != "micro-support.service" {
			t.Error(req.Service)
		}
		rsp.Content = content
		return readErr
	}
	ctx := context.Background()
	var status StatusResponse
	m.Status(ctx, &StatusRequest{}, &status)
	if status.Services[0].Status != "unknown" || status.Services[0].ObservedAt != "" {
		t.Fatal(status)
	}
	for _, tc := range []struct {
		content, want string
		err           error
	}{
		{content, "active", nil},
		{"LoadState=loaded\nActiveState=failed\n", "failed", nil},
		{"LoadState=not-found\nActiveState=inactive\n", "missing", nil},
		{"", "unknown", errors.New("permission denied")},
		{"LoadState=loaded\n", "unknown", nil},
		{content, "active", nil},
	} {
		content, readErr = tc.content, tc.err
		m.sample(ctx)
		m.Status(ctx, &StatusRequest{}, &status)
		if status.Services[0].Status != tc.want || status.Services[0].ObservedAt == "" {
			t.Fatalf("%+v", status)
		}
	}
	var history HistoryResponse
	m.History(ctx, &HistoryRequest{}, &history)
	if len(history.Events) != 6 || history.Events[0].Status != "active" || history.Events[0].Previous != "unknown" {
		t.Fatal(history)
	}
	m.sample(ctx)
	m.History(ctx, &HistoryRequest{}, &history)
	if len(history.Events) != 6 {
		t.Fatal("unchanged state generated another event")
	}
	// Returned snapshots must not expose mutable internal state.
	status.Services[0].Status = "corrupt"
	history.Events[0].Status = "corrupt"
	m.Status(ctx, &StatusRequest{}, &status)
	m.History(ctx, &HistoryRequest{}, &history)
	if status.Services[0].Status != "active" || history.Events[0].Status != "active" {
		t.Fatal("snapshot aliases internal data")
	}
	for i := 0; i < 110; i++ {
		readErr = fmt.Errorf("error %d", i)
		m.sample(ctx)
	}
	m.History(ctx, &HistoryRequest{}, &history)
	if len(history.Events) != 100 || history.Events[0].Error != "error 109" {
		t.Fatal("history bound/order")
	}
}

func TestCancellationAndConcurrentReads(t *testing.T) {
	m := fixture(t)
	entered := make(chan struct{})
	m.read = func(ctx context.Context, _ *systemd.StatusRequest, _ *systemd.Result) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Run(ctx, m); close(done) }()
	<-entered
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop")
	}
	var status StatusResponse
	m.Status(context.Background(), &StatusRequest{}, &status)
	if status.Services[0].ObservedAt != "" {
		t.Fatal("shutdown recorded as a failure")
	}
	m.read = func(_ context.Context, _ *systemd.StatusRequest, rsp *systemd.Result) error {
		rsp.Content = "LoadState=loaded\nActiveState=active"
		return nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.sample(context.Background())
			m.Status(context.Background(), &StatusRequest{}, &StatusResponse{})
			m.History(context.Background(), &HistoryRequest{}, &HistoryResponse{})
		}()
	}
	wg.Wait()
}

func TestMonitorOverRPC(t *testing.T) {
	m := fixture(t)
	m.read = func(_ context.Context, _ *systemd.StatusRequest, rsp *systemd.Result) error {
		rsp.Content = "LoadState=loaded\nActiveState=failed"
		return nil
	}
	m.sample(context.Background())
	reg := registry.NewMemoryRegistry()
	srv := server.NewServer(server.Name("monitor"), server.Address("127.0.0.1:0"), server.Registry(reg), server.Broker(broker.NewMemoryBroker()))
	if err := srv.Handle(srv.NewHandler(m)); err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	cl := client.NewClient(client.Registry(reg))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var rsp StatusResponse
	if err := cl.Call(ctx, cl.NewRequest("monitor", "Monitor.Status", &StatusRequest{}), &rsp); err != nil {
		t.Fatal(err)
	}
	if len(rsp.Services) != 1 || rsp.Services[0].Status != "failed" {
		t.Fatal(rsp)
	}
	var history HistoryResponse
	if err := cl.Call(ctx, cl.NewRequest("monitor", "Monitor.History", &HistoryRequest{}), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.Events) != 1 || history.Events[0].Status != "failed" {
		t.Fatal(history)
	}
}

// Every probe must start before a slow service finishes. Sequential sampling
// would accumulate each service's timeout and publish stale observations.
func TestSampleChecksAllServicesConcurrently(t *testing.T) {
	m := &Monitor{}
	for i := 0; i < 16; i++ {
		name := fmt.Sprintf("service-%d", i)
		m.units = append(m.units, name)
		m.states = append(m.states, State{Service: name, Status: "unknown"})
	}
	started := make(chan string, len(m.units))
	release := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.probe = func(ctx context.Context, name string) State {
		started <- name
		select {
		case <-release:
		case <-ctx.Done():
		}
		return State{Status: "unhealthy", Error: name}
	}
	done := make(chan error, 1)
	go func() { done <- m.sample(ctx) }()
	seen := map[string]bool{}
	for range m.units {
		select {
		case name := <-started:
			seen[name] = true
		case <-ctx.Done():
			<-done
			t.Fatal("slow probes prevented other services from being checked")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(seen) != len(m.units) {
		t.Fatal("not all services were checked")
	}
	var status StatusResponse
	if err := m.Status(ctx, &StatusRequest{}, &status); err != nil {
		t.Fatal(err)
	}
	for i, state := range status.Services {
		if state.Service != m.units[i] || state.Error != m.units[i] || state.Status != "unhealthy" {
			t.Fatalf("observation assigned to wrong service: %+v", state)
		}
		observed, err := time.Parse(time.RFC3339Nano, state.ObservedAt)
		if err != nil || time.Since(observed) > time.Minute {
			t.Fatalf("stale observation: %+v", state)
		}
	}
}
