// Package monitor records the state of configured systemd services.
package monitor

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/micro/agents/services/systemd"
)

type Monitor struct {
	units  []string
	read   func(context.Context, *systemd.StatusRequest, *systemd.Result) error
	check  sync.Mutex
	mu     sync.RWMutex
	states []State
	events []Event
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

// Run checks immediately and then once a minute until canceled. No model is used.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		m.sample(ctx)
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
	rsp.Services = append([]State(nil), m.states...)
	return nil
}

// History returns the latest 100 state changes, newest first. History is in memory.
func (m *Monitor) History(_ context.Context, _ *HistoryRequest, rsp *HistoryResponse) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rsp.Events = make([]Event, len(m.events))
	for i := range m.events {
		rsp.Events[i] = m.events[len(m.events)-1-i]
	}
	return nil
}

func (m *Monitor) sample(ctx context.Context) {
	m.check.Lock()
	defer m.check.Unlock()
	for i, name := range m.units {
		if ctx.Err() != nil {
			return
		}
		var result systemd.Result
		err := m.read(ctx, &systemd.StatusRequest{Service: name}, &result)
		if ctx.Err() != nil {
			return
		}
		state := State{Service: name, Status: "unknown", ObservedAt: time.Now().UTC().Format(time.RFC3339)}
		if err != nil {
			state.Error = err.Error()
		} else {
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
		}
		m.mu.Lock()
		old := m.states[i]
		m.states[i] = state
		if old.Status != state.Status || old.Error != state.Error {
			m.events = append(m.events, Event{State: state, Previous: old.Status})
			if len(m.events) > 100 {
				m.events = append([]Event(nil), m.events[len(m.events)-100:]...)
			}
			log.Printf("monitor: %s %s -> %s %s", name, old.Status, state.Status, state.Error)
		}
		m.mu.Unlock()
	}
}
