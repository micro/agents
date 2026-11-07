package alert

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/micro/studio/services/monitor"
	"go-micro.dev/v6/client"
	"go-micro.dev/v6/store"
)

func observation(status string, n int) []monitor.State {
	return []monitor.State{{Service: "support", Status: status, ObservedAt: time.Unix(int64(n), 0).UTC().Format(time.RFC3339Nano)}}
}
func TestFailureRecoveryAndRestart(t *testing.T) {
	st := store.NewMemoryStore()
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		w.WriteHeader(204)
	}))
	defer srv.Close()
	a, err := New(nil, st, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, n := range []int{1, 1} {
		if err := a.observe(observation("unhealthy", n), now); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.state.Notices) != 0 {
		t.Fatal("one repeated observation fired")
	}
	if err := a.observe(observation("unhealthy", 2), now); err != nil {
		t.Fatal(err)
	}
	if len(a.state.Notices) != 1 {
		t.Fatal("failure not queued")
	}
	id := a.state.Notices[0].ID
	a, err = New(nil, st, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.observe(observation("unhealthy", 3), now); err != nil {
		t.Fatal(err)
	}
	if len(a.state.Notices) != 1 || a.state.Notices[0].ID != id {
		t.Fatal("restart duplicated failure")
	}
	if err := a.deliver(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if err := a.deliver(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != id {
		t.Fatal(keys)
	}
	if err := a.observe(observation("healthy", 4), now); err != nil {
		t.Fatal(err)
	}
	if len(a.state.Active) != 0 || len(a.state.Notices) != 2 || a.state.Notices[1].Incident != a.state.Notices[0].Incident {
		t.Fatal(a.state)
	}
	if err := a.deliver(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatal(keys)
	}
	reloaded, err := New(nil, st, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.deliver(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatal("redelivered persisted receipt")
	}
}
func TestRetryAndRedirect(t *testing.T) {
	calls := 0
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if calls == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()
	a, _ := New(nil, store.NewMemoryStore(), srv.URL)
	now := time.Now()
	a.observe(observation("missing", 1), now)
	a.observe(observation("missing", 2), now)
	if err := a.deliver(context.Background(), now); err == nil {
		t.Fatal("ignored failed delivery")
	}
	if err := a.deliver(context.Background(), now.Add(time.Second)); err != nil || calls != 1 {
		t.Fatal("retried too soon")
	}
	if err := a.deliver(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != keys[1] {
		t.Fatal("retry changed identity")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed redirect") }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	a.webhook = redirect.URL
	if err := a.send(context.Background(), Notice{ID: "test"}); err == nil {
		t.Fatal("redirect accepted")
	}
}

type failingStore struct {
	store.Store
	fail bool
}

func (s *failingStore) Write(r *store.Record, opts ...store.WriteOption) error {
	if s.fail {
		return errors.New("disk full")
	}
	return s.Store.Write(r, opts...)
}
func TestFailedSaveDoesNotConsumeObservation(t *testing.T) {
	st := &failingStore{Store: store.NewMemoryStore()}
	a, _ := New(nil, st, "")
	now := time.Now()
	if err := a.observe(observation("missing", 1), now); err != nil {
		t.Fatal(err)
	}
	st.fail = true
	if err := a.observe(observation("missing", 2), now); err == nil {
		t.Fatal("ignored failed save")
	}
	if len(a.state.Notices) != 0 {
		t.Fatal("committed unsaved alert")
	}
	st.fail = false
	if err := a.observe(observation("missing", 2), now); err != nil {
		t.Fatal(err)
	}
	if len(a.state.Notices) != 1 {
		t.Fatal("lost observation")
	}
}

type statusClient struct {
	client.Client
	states []monitor.State
	err    error
}

func (c *statusClient) Call(_ context.Context, _ client.Request, rsp interface{}, _ ...client.CallOption) error {
	rsp.(*monitor.StatusResponse).Services = c.states
	return c.err
}
func TestStaleSourceDoesNotResolveIncidents(t *testing.T) {
	cl := &statusClient{Client: client.NewClient(), states: observation("healthy", 1)}
	a, _ := New(cl, store.NewMemoryStore(), "")
	now := time.Now()
	a.observe(observation("unhealthy", 1), now)
	a.observe(observation("unhealthy", 2), now)
	if err := a.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.state.Active["support"]; !ok {
		t.Fatal("stale sample resolved incident")
	}
	if err := a.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.state.Active["monitoring"]; !ok {
		t.Fatal("missing stale monitoring alert")
	}
}
