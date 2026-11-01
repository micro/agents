// Package github exposes repository and workflow operations as Go Micro endpoints.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// GitHub is restricted to its configured repositories. Its endpoints never write to GitHub.
type GitHub struct {
	repository   string
	repositories []string
	token        string
	client       *http.Client
	baseURL      string
}

func New(repository, token string, additional ...string) (*GitHub, error) {
	repositories := append([]string{repository}, additional...)
	if len(repositories) > 16 {
		return nil, fmt.Errorf("configure at most 16 repositories")
	}
	for i, name := range repositories {
		name = strings.TrimSpace(name)
		if !repositoryName.MatchString(name) {
			return nil, fmt.Errorf("repository must be owner/name")
		}
		for _, part := range strings.Split(name, "/") {
			if part == "." || part == ".." {
				return nil, fmt.Errorf("repository must not contain dot segments")
			}
		}
		repositories[i] = name
	}
	repository = repositories[0]
	return &GitHub{repository: repository, repositories: repositories, token: token, baseURL: "https://api.github.com", client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

type IssuesRequest struct {
	Repository string `json:"repository"`
	Page       int    `json:"page"`
}

type IssueRequest struct {
	Repository string `json:"repository"`
	Number     int    `json:"number"`
}
type CommentsRequest struct {
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	Page       int    `json:"page"`
}
type SearchRequest struct {
	Repository string `json:"repository"`
	Query      string `json:"query"`
	Page       int    `json:"page"`
}
type FileRequest struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
}

// Result contains GitHub JSON (or raw file text) and the source URL.
// More indicates another page is available; pass the next page explicitly.
type Result struct {
	Content string `json:"content"`
	URL     string `json:"url"`
	More    bool   `json:"more"`
}

// Issues lists recently updated open issues, excluding pull requests.
func (g *GitHub) Issues(ctx context.Context, req *IssuesRequest, rsp *Result) error {
	g, err := g.scope(req.Repository)
	if err != nil {
		return err
	}
	query := "repo:" + g.repository + " is:issue is:open"
	return g.get(ctx, fmt.Sprintf("/search/issues?q=%s&sort=updated&order=desc&per_page=10&page=%d", url.QueryEscape(query), page(req.Page)), "application/vnd.github+json", rsp)
}

func (g *GitHub) Issue(ctx context.Context, req *IssueRequest, rsp *Result) error {
	g, err := g.scope(req.Repository)
	if err != nil {
		return err
	}
	if req.Number < 1 {
		return fmt.Errorf("issue number must be positive")
	}
	return g.get(ctx, "/repos/"+g.repository+"/issues/"+strconv.Itoa(req.Number), "application/vnd.github+json", rsp)
}
func (g *GitHub) Comments(ctx context.Context, req *CommentsRequest, rsp *Result) error {
	g, err := g.scope(req.Repository)
	if err != nil {
		return err
	}
	if req.Number < 1 {
		return fmt.Errorf("issue number must be positive")
	}
	return g.get(ctx, fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=30&page=%d", g.repository, req.Number, page(req.Page)), "application/vnd.github+json", rsp)
}
func (g *GitHub) Search(ctx context.Context, req *SearchRequest, rsp *Result) error {
	g, err := g.scope(req.Repository)
	if err != nil {
		return err
	}
	// Quote each word so user/model input cannot inject GitHub search qualifiers.
	words := strings.Fields(req.Query)
	if len(words) == 0 || len(req.Query) > 256 {
		return fmt.Errorf("provide 1–256 characters of search terms")
	}
	for i, word := range words {
		words[i] = strconv.Quote(word)
	}
	query := "repo:" + g.repository + " is:issue " + strings.Join(words, " ")
	return g.get(ctx, fmt.Sprintf("/search/issues?q=%s&per_page=10&page=%d", url.QueryEscape(query), page(req.Page)), "application/vnd.github+json", rsp)
}

// File reads a file or lists a directory at the repository's default branch.
func (g *GitHub) File(ctx context.Context, req *FileRequest, rsp *Result) error {
	g, err := g.scope(req.Repository)
	if err != nil {
		return err
	}
	parts := strings.Split(req.Path, "/")
	for i, p := range parts {
		if p == ".." || p == "." {
			return fmt.Errorf("path must not contain dot segments")
		}
		parts[i] = url.PathEscape(p)
	}
	return g.get(ctx, "/repos/"+g.repository+"/contents/"+strings.Join(parts, "/"), "application/vnd.github.raw+json", rsp)
}
func page(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
func (g *GitHub) get(ctx context.Context, path, accept string, rsp *Result) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "micro-agents-support")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	res, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub returned HTTP %d; check repository access, token permissions and rate limits", res.StatusCode)
	}
	const limit = 256 * 1024
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return err
	}
	if len(body) > limit {
		return fmt.Errorf("GitHub response exceeds 256 KiB; request a smaller file or narrower search")
	}
	// Defense in depth: search results must remain in the configured repository.
	if strings.HasPrefix(path, "/search/issues?") {
		var result struct {
			Items []struct {
				RepositoryURL string `json:"repository_url"`
			} `json:"items"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return err
		}
		for _, item := range result.Items {
			if !strings.EqualFold(item.RepositoryURL, "https://api.github.com/repos/"+g.repository) {
				return fmt.Errorf("search returned a result outside the configured repository")
			}
		}
	}
	rsp.Content = string(body)
	rsp.URL = "https://api.github.com" + path
	rsp.More = strings.Contains(res.Header.Get("Link"), `rel="next"`)
	return nil
}

// Repositories lists the allowed repositories; the first is used when omitted.
func (g *GitHub) Repositories(_ context.Context, _ *RepositoriesRequest, rsp *RepositoriesResponse) error {
	rsp.Repositories = append([]string(nil), g.repositories...)
	return nil
}

type RepositoriesRequest struct{}
type RepositoriesResponse struct {
	Repositories []string `json:"repositories"`
}

func (g *GitHub) scope(repository string) (*GitHub, error) {
	if repository == "" {
		return g, nil
	}
	for _, allowed := range g.repositories {
		if strings.EqualFold(repository, allowed) {
			scoped := *g
			scoped.repository = allowed
			return &scoped, nil
		}
	}
	return nil, fmt.Errorf("repository %q is not configured", repository)
}
