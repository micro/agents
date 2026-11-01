// Package monitor records the health of configured services.
package monitor

import (
	"context"
	"errors"
	"fmt"
	"github.com/micro/agents/services/health"
	"go-micro.dev/v6/client"
	"go-micro.dev/v6/registry"
	"go-micro.dev/v6/store"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/micro/agents/services/systemd"
)

type Monitor struct {
	units     []string
	probe     func(context.Context, string) State
	store     store.Store
	lastError error
	read      func(context.Context, *systemd.StatusRequest, *systemd.Result) error
	check     sync.Mutex
	mu        sync.RWMutex
	states    []State
	events    []Event
}

type State struct {
	Service    string `json:"service"`
	Status     string `json:"status"`
	ObservedAt string `json:"observed_at"`
	Error      string `json:"error,omitempty"`
}
type Event struct {
	State
	Previous string `json:"previous"`
}
type StatusRequest struct{}
type StatusResponse struct {
	Services []State `json:"services"`
}
type HistoryRequest struct{}
type HistoryResponse struct {
	Events []Event `json:"events"`
}

func New(host *systemd.Systemd) *Monitor {
	var list systemd.ListResponse
	host.List(context.Background(), &systemd.ListRequest{}, &list)
	m := &Monitor{units: list.Services, read: host.Status}
	for _, name := range m.units {
		m.states = append(m.states, State{Service: name, Status: "unknown"})
	}
	return m
}

// Run checks immediately and then every ten seconds until canceled. No model is used.
func Run(ctx context.Context, m *Monitor) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if err := m.sample(ctx); err != nil {
			log.Printf("monitor: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Status returns the latest observations, including timestamps and read errors.
func (m *Monitor) Status(_ context.Context, _ *StatusRequest, rsp *StatusResponse) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.lastError != nil {
		return m.lastError
	}
	rsp.Services = append([]State(nil), m.states...)
	return nil
}

// History returns the latest 100 state changes, newest first.
func (m *Monitor) History(_ context.Context, _ *HistoryRequest, rsp *HistoryResponse) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rsp.Events = make([]Event, len(m.events))
	for i := range m.events {
		rsp.Events[i] = m.events[len(m.events)-1-i]
	}
	return nil
}

func (m *Monitor) sample(ctx context.Context) error {
	m.check.Lock()
	defer m.check.Unlock()
	observations := make([]State, 0, len(m.units))
	for _, name := range m.units {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var state State
		if m.probe != nil {
			state = m.probe(ctx, name)
		} else {
			state = m.systemd(ctx, name)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		state.Service = name
		state.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
		observations = append(observations, state)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	events := append([]Event(nil), m.events...)
	var changes []Event
	for i, state := range observations {
		old := m.states[i]
		if old.Status != state.Status || old.Error != state.Error {
			event := Event{State: state, Previous: old.Status}
			events = append(events, event)
			changes = append(changes, event)
		}
	}
	if len(events) > 100 {
		events = events[len(events)-100:]
	}
	if m.store != nil {
		if err := m.store.Write(store.NewRecord("monitor/state", snapshot{States: observations, Events: events})); err != nil {
			m.lastError = fmt.Errorf("save monitoring observations: %w", err)
			return m.lastError
		}
	}
	m.states, m.events, m.lastError = observations, events, nil
	for _, event := range changes {
		log.Printf("monitor: %s %s -> %s %s", event.Service, event.Previous, event.Status, event.Error)
	}
	return nil
}

func (m *Monitor) systemd(ctx context.Context, name string) State {
	var result systemd.Result
	state := State{Status: "unknown"}
	if err := m.read(ctx, &systemd.StatusRequest{Service: name}, &result); err != nil {
		state.Error = err.Error()
		return state
	}
	fields := map[string]string{}
	for _, line := range strings.Split(result.Content, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			fields[key] = value
		}
	}
	switch {
	case fields["LoadState"] == "not-found":
		state.Status = "missing"
	case fields["LoadState"] != "loaded":
		state.Error = "unit is not loaded: " + fields["LoadState"]
	case fields["ActiveState"] == "":
		state.Error = "systemd returned no active state"
	default:
		state.Status = fields["ActiveState"]
	}
	return state
}

type snapshot struct {
	States []State
	Events []Event
}

// NewRPC checks every registered instance through Health.Check and persists observations.
func NewRPC(names []string, reg registry.Registry, cl client.Client, st store.Store) (*Monitor, error) {
	if len(names) == 0 || len(names) > 16 {
		return nil, fmt.Errorf("configure 1–16 service names")
	}
	m := &Monitor{store: st}
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("service name must not be empty")
		}
		if !seen[name] {
			m.units = append(m.units, name)
			m.states = append(m.states, State{Service: name, Status: "unknown"})
			seen[name] = true
		}
	}
	if st != nil {
		records, err := st.Read("monitor/state")
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if len(records) > 0 {
			var saved snapshot
			if err := records[0].Decode(&saved); err != nil {
				return nil, err
			}
			m.events = saved.Events
			if len(m.events) > 100 {
				m.events = m.events[len(m.events)-100:]
			}
			for i, state := range m.states {
				for _, old := range saved.States {
					if state.Service == old.Service {
						m.states[i] = old
						break
					}
				}
			}
		}
	}
	m.probe = func(ctx context.Context, name string) State {
		services, err := reg.GetService(name)
		if errors.Is(err, registry.ErrNotFound) || (err == nil && len(services) == 0) {
			return State{Status: "missing", Error: "service is not registered"}
		}
		if err != nil {
			return State{Status: "unknown", Error: "registry lookup failed"}
		}
		state := State{Status: "healthy"}
		nodes := 0
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		for _, svc := range services {
			for _, node := range svc.Nodes {
				nodes++
				var rsp health.Response
				err := cl.Call(ctx, cl.NewRequest(name, "Health.Check", &health.Request{}), &rsp, client.WithAddress(node.Address), client.WithRetries(0), client.WithRequestTimeout(2*time.Second))
				if err != nil || rsp.Status != "ok" {
					state.Status = "unhealthy"
					state.Error = "one or more registered instances failed Health.Check"
				}
			}
		}
		if nodes == 0 {
			state.Status = "missing"
			state.Error = "service has no registered instances"
		}
		return state
	}
	return m, nil
}
