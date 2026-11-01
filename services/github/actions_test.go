package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func actionsFixture(t *testing.T, handler http.HandlerFunc) *Actions {
	t.Helper()
	a, err := NewActions("micro/example", "fixture-token", "release.yml", "release/stable")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	a.github.baseURL = srv.URL
	return a
}

func TestActionsScope(t *testing.T) {
	for _, name := range []string{"", "../release.yml", "release.yml?ref=evil", "a/b.yml"} {
		if _, err := NewActions("micro/example", "", name, ""); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	requests := 0
	a := actionsFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		switch r.URL.Path {
		case "/repos/micro/example/contents/.github/workflows/release.yml":
			if r.URL.Query().Get("ref") != "release/stable" {
				t.Error(r.URL)
			}
			fmt.Fprint(w, "on: workflow_dispatch")
		case "/repos/micro/example/actions/workflows/release.yml":
			fmt.Fprint(w, `{"id":8}`)
		case "/repos/micro/example/actions/workflows/release.yml/runs":
			if r.URL.Query().Get("page") != "2" {
				t.Error(r.URL)
			}
			w.Header().Set("Link", `<https://api.github.com/next>; rel="next"`)
			fmt.Fprint(w, `{"workflow_runs":[{"id":42,"status":"completed","conclusion":"failure"}]}`)
		case "/repos/micro/example/actions/runs/42":
			fmt.Fprint(w, `{"id":42,"workflow_id":8,"status":"completed","conclusion":"failure"}`)
		case "/repos/micro/example/actions/runs/43":
			fmt.Fprint(w, `{"id":43,"workflow_id":9}`)
		case "/repos/micro/example/actions/runs/42/jobs":
			if r.URL.Query().Get("page") != "2" {
				t.Error(r.URL)
			}
			fmt.Fprint(w, `{"jobs":[{"name":"deploy","conclusion":"failure"}]}`)
		default:
			t.Error(r.URL)
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	var workflow WorkflowResponse
	if err := a.Workflow(ctx, &WorkflowRequest{}, &workflow); err != nil || workflow.Ref != "release/stable" || workflow.Definition != "on: workflow_dispatch" {
		t.Fatalf("%+v %v", workflow, err)
	}
	var rsp Result
	if err := a.Runs(ctx, &RunsRequest{Page: 2}, &rsp); err != nil || !rsp.More {
		t.Fatalf("%+v %v", rsp, err)
	}
	if err := a.Jobs(ctx, &JobsRequest{ID: 42, Page: 2}, &rsp); err != nil || !strings.Contains(rsp.Content, "deploy") {
		t.Fatalf("%+v %v", rsp, err)
	}
	if err := a.Jobs(ctx, &JobsRequest{ID: 43}, &rsp); err == nil {
		t.Fatal("read jobs for another workflow")
	}
	before := requests
	if err := a.Run(ctx, &RunRequest{}, &rsp); err == nil || requests != before {
		t.Fatal("invalid id made request")
	}
}

func TestDispatch(t *testing.T) {
	for _, status := range []int{200, 204, 403, 422, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			a := actionsFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/repos/micro/example/actions/workflows/release.yml/dispatches" {
					t.Error(r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer fixture-token" || r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
					t.Error("missing credentials or API version")
				}
				var body struct {
					Ref    string
					Inputs map[string]string
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Ref != "release/stable" || body.Inputs["version"] != "1.2.3" {
					t.Errorf("%+v", body)
				}
				w.WriteHeader(status)
				if status == 200 {
					fmt.Fprint(w, `{"workflow_run_id":42,"html_url":"https://untrusted.invalid"}`)
				}
			})
			var rsp DispatchResponse
			err := a.Dispatch(context.Background(), &DispatchRequest{Inputs: map[string]string{"version": "1.2.3"}}, &rsp)
			if calls != 1 {
				t.Fatalf("dispatch sent %d times", calls)
			}
			if status == 200 || status == 204 {
				if err != nil || !rsp.Accepted {
					t.Fatalf("%+v %v", rsp, err)
				}
				if status == 200 && (rsp.ID != 42 || rsp.URL != "https://github.com/micro/example/actions/runs/42") {
					t.Fatal(rsp)
				}
				if status == 204 && rsp.ID != 0 {
					t.Fatal("invented run id")
				}
			} else if err == nil {
				t.Fatal("accepted failed dispatch")
			}
		})
	}
}

func TestDispatchLimitsAndUncertainResults(t *testing.T) {
	calls := 0
	a := actionsFixture(t, func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, "invalid JSON") })
	var rsp DispatchResponse
	if err := a.Dispatch(context.Background(), &DispatchRequest{}, &rsp); err == nil || !strings.Contains(err.Error(), "accepted") || calls != 1 {
		t.Fatalf("%v calls=%d", err, calls)
	}
	inputs := map[string]string{"huge": strings.Repeat("x", 64*1024)}
	if err := a.Dispatch(context.Background(), &DispatchRequest{Inputs: inputs}, &rsp); err == nil || calls != 1 {
		t.Fatal("sent oversized request")
	}
	inputs = map[string]string{}
	for i := 0; i < 26; i++ {
		inputs[fmt.Sprint(i)] = "x"
	}
	if err := a.Dispatch(context.Background(), &DispatchRequest{Inputs: inputs}, &rsp); err == nil || calls != 1 {
		t.Fatal("sent too many inputs")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Dispatch(ctx, &DispatchRequest{}, &rsp); err == nil || calls != 1 {
		t.Fatal("sent canceled request")
	}
	a.github.token = ""
	if err := a.Dispatch(context.Background(), &DispatchRequest{}, &rsp); err == nil || calls != 1 {
		t.Fatal("sent unauthenticated request")
	}
}

func TestDispatchDoesNotFollowRedirect(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	a := actionsFixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if err := a.Dispatch(context.Background(), &DispatchRequest{}, &DispatchResponse{}); err == nil || reached {
		t.Fatal("followed dispatch redirect")
	}
}
