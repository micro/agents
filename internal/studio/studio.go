// Package studio provides a local web workspace for Go Micro applications.
package studio

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/micro/studio/services/alert"
	"github.com/micro/studio/services/monitor"
	"go-micro.dev/v6/agent"
	pb "go-micro.dev/v6/agent/proto"
	"go-micro.dev/v6/client"
	"go-micro.dev/v6/registry"
)

//go:embed web/*
var assets embed.FS

type workspace struct {
	registry registry.Registry
	client   client.Client
}

type service struct {
	Name      string               `json:"name"`
	Version   string               `json:"version"`
	Instances int                  `json:"instances"`
	Agent     bool                 `json:"agent"`
	Endpoints []*registry.Endpoint `json:"endpoints"`
}

// New serves a single-operator workspace. The listener must be loopback-only.
// Browser-origin checks prevent other sites from using the local RPC bridge.
func New(reg registry.Registry, cl client.Client, address string) http.Handler {
	w := &workspace{registry: reg, client: cl}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/services", w.services)
	mux.HandleFunc("GET /api/health", w.health)
	mux.HandleFunc("GET /api/alerts", w.alerts)
	mux.HandleFunc("POST /api/chat", w.chat)
	files, _ := fs.Sub(assets, "web")
	mux.Handle("GET /", http.FileServer(http.FS(files)))
	_, port, _ := net.SplitHostPort(address)
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		host, requestPort, err := net.SplitHostPort(r.Host)
		ip := net.ParseIP(host)
		if err != nil || requestPort != port || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			http.Error(rw, "Studio accepts local requests only", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); (origin != "" && origin != "http://"+r.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(rw, "Cross-origin requests are not allowed", http.StatusForbidden)
			return
		}
		rw.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		rw.Header().Set("X-Content-Type-Options", "nosniff")
		rw.Header().Set("Referrer-Policy", "no-referrer")
		rw.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(rw, r)
	})
}

func respond(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(value)
}

func (w *workspace) services(rw http.ResponseWriter, r *http.Request) {
	names, err := w.registry.ListServices()
	if err != nil {
		respond(rw, nil, err)
		return
	}
	result := []service{}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name.Name] {
			continue
		}
		seen[name.Name] = true
		versions, err := w.registry.GetService(name.Name)
		if err == registry.ErrNotFound {
			continue
		}
		if err != nil {
			respond(rw, nil, err)
			return
		}
		for _, version := range versions {
			item := service{Name: version.Name, Version: version.Version, Instances: len(version.Nodes), Endpoints: version.Endpoints}
			for _, ep := range version.Endpoints {
				if ep.Name == "Agent.Chat" {
					item.Agent = true
				}
			}
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].Version < result[j].Version
		}
		return result[i].Name < result[j].Name
	})
	respond(rw, result, nil)
}

func (w *workspace) call(ctx context.Context, name, endpoint string, req, rsp any) error {
	return w.client.Call(ctx, w.client.NewRequest(name, endpoint, req), rsp, client.WithRetries(0), client.WithRequestTimeout(2*time.Minute))
}

func (w *workspace) health(rw http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var result monitor.StatusResponse
	err := w.call(ctx, "monitor", "Monitor.Status", &monitor.StatusRequest{}, &result)
	respond(rw, result, err)
}

func (w *workspace) alerts(rw http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var result alert.ListResponse
	err := w.call(ctx, "alert", "Alert.List", &alert.ListRequest{}, &result)
	respond(rw, result, err)
}

func (w *workspace) chat(rw http.ResponseWriter, r *http.Request) {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		http.Error(rw, "Use application/json", http.StatusUnsupportedMediaType)
		return
	}
	var input struct {
		Agent   string `json:"agent"`
		Message string `json:"message"`
		Session string `json:"session"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		http.Error(rw, "Invalid chat request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(rw, "Expected one request", http.StatusBadRequest)
		return
	}
	if _, err := uuid.Parse(input.Session); err != nil || strings.TrimSpace(input.Message) == "" || len(input.Agent) > 256 {
		http.Error(rw, "An agent, message and valid session are required", http.StatusBadRequest)
		return
	}
	versions, err := w.registry.GetService(input.Agent)
	if err != nil {
		respond(rw, nil, err)
		return
	}
	found := false
	for _, version := range versions {
		for _, ep := range version.Endpoints {
			if ep.Name == "Agent.Chat" {
				found = true
			}
		}
	}
	if !found {
		http.Error(rw, "This service does not expose an agent", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	ctx = agent.WithSession(ctx, "studio-"+input.Session)
	var result pb.ChatResponse
	// Approval metadata is deliberately not supplied: agents retain their own policy.
	// A timed-out request is never retried, as its tools may already have run.
	err = w.call(ctx, input.Agent, "Agent.Chat", &pb.ChatRequest{Message: input.Message}, &result)
	respond(rw, &result, err)
}
