package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestMultipleRepositories(t *testing.T) {
	g, err := New("micro/go-micro", "", " micro/studio ")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/issues" {
			query := r.URL.Query().Get("q")
			repo := "micro/go-micro"
			if strings.HasPrefix(query, "repo:micro/studio ") {
				repo = "micro/studio"
			}
			fmt.Fprintf(w, `{"items":[{"repository_url":"https://api.github.com/repos/%s"}]}`, repo)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/repos/micro/studio/") {
			t.Error(r.URL)
		}
		fmt.Fprint(w, "{}")
	}))
	defer srv.Close()
	g.baseURL = srv.URL
	ctx := context.Background()
	var repos RepositoriesResponse
	g.Repositories(ctx, &RepositoriesRequest{}, &repos)
	if len(repos.Repositories) != 2 || repos.Repositories[1] != "micro/studio" {
		t.Fatal(repos)
	}
	for _, repo := range []string{"micro/studio", "unconfigured/repo"} {
		calls := []func() error{
			func() error { return g.Issues(ctx, &IssuesRequest{Repository: repo}, &Result{}) },
			func() error { return g.Issue(ctx, &IssueRequest{Repository: repo, Number: 1}, &Result{}) },
			func() error { return g.Comments(ctx, &CommentsRequest{Repository: repo, Number: 1}, &Result{}) },
			func() error { return g.Search(ctx, &SearchRequest{Repository: repo, Query: "startup"}, &Result{}) },
			func() error { return g.File(ctx, &FileRequest{Repository: repo, Path: "README.md"}, &Result{}) },
		}
		for _, call := range calls {
			err := call()
			if (err == nil) != (repo == "micro/studio") {
				t.Fatalf("%s: %v", repo, err)
			}
		}
	}
	// Requests for different repositories must not mutate the shared default.
	var wg sync.WaitGroup
	for _, repo := range []string{"", "micro/studio", "MICRO/STUDIO"} {
		wg.Add(1)
		go func(repo string) {
			defer wg.Done()
			if err := g.Issues(ctx, &IssuesRequest{Repository: repo}, &Result{}); err != nil {
				t.Error(err)
			}
		}(repo)
	}
	wg.Wait()
	if g.repository != "micro/go-micro" {
		t.Fatal(g.repository)
	}
	if _, err := New("micro/go-micro", "", "../private"); err == nil {
		t.Fatal("accepted invalid repository")
	}
}
