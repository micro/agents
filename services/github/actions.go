package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
)

var workflowName = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*\.ya?ml$`)

// Actions manages one workflow in one repository. The workflow owns release and deployment policy.
type Actions struct {
	github   *GitHub
	workflow string
	ref      string
}

func NewActions(repository, token, workflow, ref string) (*Actions, error) {
	g, err := New(repository, token)
	if err != nil {
		return nil, err
	}
	if !workflowName.MatchString(workflow) {
		return nil, fmt.Errorf("workflow must be a filename such as release.yml")
	}
	if ref == "" {
		ref = "main"
	}
	return &Actions{github: g, workflow: workflow, ref: ref}, nil
}

type WorkflowRequest struct{}
type WorkflowResponse struct {
	Repository string `json:"repository"`
	Workflow   string `json:"workflow"`
	Ref        string `json:"ref"`
	Definition string `json:"definition"`
	URL        string `json:"url"`
}

// Workflow reads the configured workflow at the ref used for dispatch.
func (a *Actions) Workflow(ctx context.Context, req *WorkflowRequest, rsp *WorkflowResponse) error {
	var file Result
	path := "/repos/" + a.github.repository + "/contents/.github/workflows/" + a.workflow + "?ref=" + url.QueryEscape(a.ref)
	if err := a.github.get(ctx, path, "application/vnd.github.raw+json", &file); err != nil {
		return err
	}
	*rsp = WorkflowResponse{Repository: a.github.repository, Workflow: a.workflow, Ref: a.ref, Definition: file.Content, URL: file.URL}
	return nil
}

type RunsRequest struct {
	Page int `json:"page"`
}
type RunRequest struct {
	ID int64 `json:"id"`
}

// Runs lists recent runs of the configured workflow, including status and conclusion.
func (a *Actions) Runs(ctx context.Context, req *RunsRequest, rsp *Result) error {
	return a.github.get(ctx, fmt.Sprintf("%s/runs?per_page=10&page=%d", a.path(), page(req.Page)), "application/vnd.github+json", rsp)
}

// Run reads one run, rejecting runs from other workflows.
func (a *Actions) Run(ctx context.Context, req *RunRequest, rsp *Result) error {
	if req.ID < 1 {
		return fmt.Errorf("run id must be positive")
	}
	var workflow Result
	if err := a.github.get(ctx, a.path(), "application/vnd.github+json", &workflow); err != nil {
		return err
	}
	var expected struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(workflow.Content), &expected); err != nil {
		return err
	}
	var run Result
	if err := a.github.get(ctx, fmt.Sprintf("/repos/%s/actions/runs/%d", a.github.repository, req.ID), "application/vnd.github+json", &run); err != nil {
		return err
	}
	var actual struct {
		WorkflowID int64 `json:"workflow_id"`
	}
	if err := json.Unmarshal([]byte(run.Content), &actual); err != nil {
		return err
	}
	if expected.ID == 0 || actual.WorkflowID != expected.ID {
		return fmt.Errorf("run does not belong to the configured workflow")
	}
	*rsp = run
	return nil
}

type JobsRequest struct {
	ID   int64 `json:"id"`
	Page int   `json:"page"`
}

// Jobs reads job and step conclusions for a run of the configured workflow.
func (a *Actions) Jobs(ctx context.Context, req *JobsRequest, rsp *Result) error {
	var run Result
	if err := a.Run(ctx, &RunRequest{ID: req.ID}, &run); err != nil {
		return err
	}
	return a.github.get(ctx, fmt.Sprintf("/repos/%s/actions/runs/%d/jobs?per_page=20&page=%d", a.github.repository, req.ID, page(req.Page)), "application/vnd.github+json", rsp)
}

type DispatchRequest struct {
	Inputs map[string]string `json:"inputs"`
}
type DispatchResponse struct {
	Accepted bool   `json:"accepted"`
	ID       int64  `json:"id,omitempty"`
	URL      string `json:"url"`
}

// Dispatch starts the configured workflow once. Callers must authorize the action;
// a timeout has an unknown outcome and must not be retried automatically.
func (a *Actions) Dispatch(ctx context.Context, req *DispatchRequest, rsp *DispatchResponse) error {
	if a.github.token == "" {
		return fmt.Errorf("set GITHUB_TOKEN with Actions write permission to dispatch")
	}
	if len(req.Inputs) > 25 {
		return fmt.Errorf("workflow accepts at most 25 inputs")
	}
	body, err := json.Marshal(struct {
		Ref    string            `json:"ref"`
		Inputs map[string]string `json:"inputs,omitempty"`
	}{a.ref, req.Inputs})
	if err != nil {
		return err
	}
	if len(body) > 64*1024 {
		return fmt.Errorf("dispatch inputs exceed 64 KiB")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.github.baseURL+a.path()+"/dispatches", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.github.token)
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "micro-agents-release")
	httpReq.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	res, err := a.github.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("dispatch outcome unknown; inspect runs before trying again: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("GitHub dispatch returned HTTP %d; inspect runs and permissions before trying again", res.StatusCode)
	}
	result := DispatchResponse{Accepted: true, URL: "https://github.com/" + a.github.repository + "/actions/workflows/" + a.workflow}
	if res.StatusCode == http.StatusOK {
		data, err := io.ReadAll(io.LimitReader(res.Body, 64*1024+1))
		if err != nil || len(data) > 64*1024 {
			return fmt.Errorf("dispatch accepted but response unreadable; inspect runs, do not redispatch")
		}
		var run struct {
			ID int64 `json:"workflow_run_id"`
		}
		if err := json.Unmarshal(data, &run); err != nil || run.ID < 1 {
			return fmt.Errorf("dispatch accepted but run id unavailable; inspect runs, do not redispatch")
		}
		result.ID = run.ID
		result.URL = fmt.Sprintf("https://github.com/%s/actions/runs/%d", a.github.repository, run.ID)
	}
	*rsp = result
	return nil
}

func (a *Actions) path() string {
	return "/repos/" + a.github.repository + "/actions/workflows/" + a.workflow
}
