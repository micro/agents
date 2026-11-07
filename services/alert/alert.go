// Package alert records incidents and delivers failure and recovery notifications.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/micro/studio/services/monitor"
	"go-micro.dev/v6/client"
	"go-micro.dev/v6/store"
)

type Alert struct {
	mu      sync.Mutex
	state   state
	store   store.Store
	client  client.Client
	webhook string
	http    *http.Client
}

type Notice struct {
	ID          string    `json:"id"`
	Incident    string    `json:"incident"`
	Service     string    `json:"service"`
	Status      string    `json:"status"`
	Text        string    `json:"text"`
	CreatedAt   string    `json:"created_at"`
	DeliveredAt string    `json:"delivered_at,omitempty"`
	Attempts    int       `json:"attempts"`
	Error       string    `json:"error,omitempty"`
	RetryAt     time.Time `json:"retry_at,omitempty"`
}
type Incident struct {
	ID       string `json:"id"`
	Service  string `json:"service"`
	OpenedAt string `json:"opened_at"`
}
type state struct {
	Active   map[string]Incident
	Failures map[string]int
	Observed map[string]string
	Notices  []Notice
}

type ListRequest struct{}
type ListResponse struct {
	Destination   string     `json:"destination"`
	Incidents     []Incident `json:"incidents"`
	Notifications []Notice   `json:"notifications"`
}

func New(cl client.Client, st store.Store, webhook string) (*Alert, error) {
	if webhook != "" {
		u, err := url.Parse(webhook)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" {
			return nil, fmt.Errorf("ALERT_WEBHOOK must be an HTTP(S) URL without userinfo or fragment")
		}
	}
	a := &Alert{client: cl, store: st, webhook: webhook, http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	records, err := st.Read("alert/state")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if len(records) > 0 {
		if err := records[0].Decode(&a.state); err != nil {
			return nil, err
		}
	}
	if a.state.Active == nil {
		a.state.Active = map[string]Incident{}
	}
	if a.state.Failures == nil {
		a.state.Failures = map[string]int{}
	}
	if a.state.Observed == nil {
		a.state.Observed = map[string]string{}
	}
	return a, nil
}

// List reports open incidents and up to 100 recent notifications, newest first.
func (a *Alert) List(_ context.Context, _ *ListRequest, rsp *ListResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	*rsp = ListResponse{Destination: "journal"}
	if a.webhook != "" {
		rsp.Destination = "webhook"
	}
	for _, incident := range a.state.Active {
		rsp.Incidents = append(rsp.Incidents, incident)
	}
	for i := len(a.state.Notices) - 1; i >= 0; i-- {
		rsp.Notifications = append(rsp.Notifications, a.state.Notices[i])
	}
	return nil
}

// Run checks Monitor every ten seconds. Detection and delivery do not use a model.
func Run(ctx context.Context, a *Alert) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if err := a.poll(ctx); err != nil && ctx.Err() == nil {
			log.Printf("alert: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *Alert) poll(ctx context.Context) error {
	var rsp monitor.StatusResponse
	now := time.Now().UTC()
	err := a.client.Call(ctx, a.client.NewRequest("monitor", "Monitor.Status", &monitor.StatusRequest{}), &rsp, client.WithRetries(0), client.WithRequestTimeout(5*time.Second))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	source := monitor.State{Service: "monitoring", Status: "healthy", ObservedAt: now.Format(time.RFC3339Nano)}
	if err != nil || len(rsp.Services) == 0 {
		source.Status = "unhealthy"
		source.Error = "monitor observations are unavailable"
		rsp.Services = nil
	}
	for _, s := range rsp.Services {
		observed, err := time.Parse(time.RFC3339Nano, s.ObservedAt)
		if err != nil || now.Sub(observed) > time.Minute || observed.After(now.Add(time.Minute)) {
			source.Status = "unhealthy"
			source.Error = "monitor observations are stale"
			rsp.Services = nil
			break
		}
	}
	observations := append(rsp.Services, source)
	observeErr := a.observe(observations, now)
	deliverErr := a.deliver(ctx, now)
	if observeErr != nil {
		return observeErr
	}
	return deliverErr
}

func (a *Alert) observe(observations []monitor.State, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Stage the complete update; a failed save must not consume an observation.
	data, _ := json.Marshal(a.state)
	var next state
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	for _, s := range observations {
		if s.ObservedAt == "" || next.Observed[s.Service] == s.ObservedAt {
			continue
		}
		next.Observed[s.Service] = s.ObservedAt
		healthy := s.Status == "healthy"
		incident, open := next.Active[s.Service]
		if healthy {
			next.Failures[s.Service] = 0
			if open {
				next.Notices = append(next.Notices, notice(incident, "recovered", s, now))
				delete(next.Active, s.Service)
			}
		} else {
			if next.Failures[s.Service] < 2 {
				next.Failures[s.Service]++
			}
			if next.Failures[s.Service] >= 2 && !open {
				incident = Incident{ID: uuid.NewString(), Service: s.Service, OpenedAt: now.Format(time.RFC3339Nano)}
				next.Active[s.Service] = incident
				next.Notices = append(next.Notices, notice(incident, "firing", s, now))
			}
		}
	}
	// Keep pending deliveries; discard only old delivered history to make room.
	for len(next.Notices) > 100 {
		removed := false
		for i, n := range next.Notices {
			if n.DeliveredAt != "" {
				next.Notices = append(next.Notices[:i], next.Notices[i+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			return fmt.Errorf("notification queue is full; cannot record new transitions")
		}
	}
	if err := a.store.Write(store.NewRecord("alert/state", next)); err != nil {
		return fmt.Errorf("save alerts: %w", err)
	}
	a.state = next
	return nil
}

func notice(incident Incident, status string, s monitor.State, now time.Time) Notice {
	return Notice{ID: uuid.NewString(), Incident: incident.ID, Service: s.Service, Status: status,
		Text: fmt.Sprintf("%s: %s (%s). %s", s.Service, status, s.Status, s.Error), CreatedAt: now.Format(time.RFC3339Nano)}
}

func (a *Alert) deliver(ctx context.Context, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, current := range a.state.Notices {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if current.DeliveredAt != "" {
			continue
		}
		// Preserve notification order, including failure before recovery.
		if now.Before(current.RetryAt) {
			break
		}
		current.Attempts++
		err := a.send(ctx, current)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			current.DeliveredAt = now.Format(time.RFC3339Nano)
			current.Error = ""
		} else {
			current.Error = err.Error()
			current.RetryAt = now.Add(time.Minute)
		}
		next := a.state
		next.Notices = append([]Notice(nil), a.state.Notices...)
		next.Notices[i] = current
		if saveErr := a.store.Write(store.NewRecord("alert/state", next)); saveErr != nil {
			return fmt.Errorf("save delivery receipt: %w", saveErr)
		}
		a.state = next
		return err
	}
	return nil
}

func (a *Alert) send(ctx context.Context, n Notice) error {
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	if a.webhook == "" {
		log.Printf("alert: %s", data)
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.webhook, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid webhook request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", n.ID)
	res, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed")
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("webhook returned HTTP %d", res.StatusCode)
	}
	return nil
}
