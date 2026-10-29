package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadOnlyRepositoryScope(t *testing.T) {
	g, _ := New("micro/go-micro", "test-token")
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected request")
		}
		switch r.URL.Path {
		case "/repos/micro/go-micro/issues/42":
			w.Write([]byte(`{"number":42}`))
		case "/repos/micro/go-micro/issues/42/comments":
			if r.URL.Query().Get("page") != "2" {
				t.Error("page not forwarded")
			}
			w.Header().Set("Link", `<https://api.github.com/next>; rel="next"`)
			w.Write([]byte(`[]`))
		case "/repos/micro/go-micro/contents/docs/hello world.md":
			w.Write([]byte("documentation"))
		case "/search/issues":
			q := r.URL.Query().Get("q")
			if !strings.HasPrefix(q, `repo:micro/go-micro is:issue "`) {
				t.Errorf("unscoped query: %s", q)
			}
			w.Write([]byte(`{"items":[{"repository_url":"https://api.github.com/repos/micro/go-micro"}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer s.Close()
	g.baseURL = s.URL
	var result Result
	ctx := context.Background()
	if err := g.Issue(ctx, &IssueRequest{Number: 42}, &result); err != nil {
		t.Fatal(err)
	}
	if err := g.Comments(ctx, &CommentsRequest{Number: 42, Page: 2}, &result); err != nil || !result.More {
		t.Fatalf("pagination: %+v %v", result, err)
	}
	if err := g.File(ctx, &FileRequest{Path: "docs/hello world.md"}, &result); err != nil {
		t.Fatal(err)
	}
	if err := g.Search(ctx, &SearchRequest{Query: "startup error"}, &result); err != nil {
		t.Fatal(err)
	}
	if err := g.File(ctx, &FileRequest{Path: "../secrets"}, &result); err == nil {
		t.Fatal("accepted traversal")
	}
	if err := g.Issue(ctx, &IssueRequest{}, &result); err == nil {
		t.Fatal("accepted invalid number")
	}
	if calls != 4 {
		t.Fatalf("unexpected HTTP calls: %d", calls)
	}
}

func TestErrorsAndScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"auth", 401, "secret response"},
		{"rate limit", 429, "secret response"},
		{"too large", 200, strings.Repeat("x", 256*1024+1)},
		{"outside repository", 200, `{"items":[{"repository_url":"https://api.github.com/repos/other/private"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer s.Close()
			g, _ := New("micro/go-micro", "")
			g.baseURL = s.URL
			err := g.Search(context.Background(), &SearchRequest{Query: "test"}, new(Result))
			if err == nil || strings.Contains(err.Error(), "secret response") {
				t.Fatalf("unsafe/missing error: %v", err)
			}
		})
	}
}

func TestRedirectDoesNotForwardToken(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	g, _ := New("micro/go-micro", "secret")
	g.baseURL = source.URL
	if err := g.Issue(context.Background(), &IssueRequest{Number: 1}, new(Result)); err == nil {
		t.Fatal("accepted redirect")
	}
	if reached {
		t.Fatal("followed redirect")
	}
}

func TestSearchRepositoryCase(t *testing.T) {
	for _, tc := range []struct {
		name, repositoryURL string
		wantError           bool
	}{
		{"canonical capitalization", "https://api.github.com/repos/Octocat/Hello-World", false},
		{"different repository", "https://api.github.com/repos/Octocat/Other", true},
		{"different owner", "https://api.github.com/repos/Other/Hello-World", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"items":[{"repository_url":%q}]}`, tc.repositoryURL)
			}))
			defer api.Close()
			g, err := New("octocat/hello-world", "")
			if err != nil {
				t.Fatal(err)
			}
			g.baseURL = api.URL
			err = g.Search(context.Background(), &SearchRequest{Query: "startup"}, new(Result))
			if (err != nil) != tc.wantError {
				t.Fatalf("Search error = %v, wantError = %v", err, tc.wantError)
			}
		})
	}
}
