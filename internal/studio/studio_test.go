package studio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	pb "go-micro.dev/v6/agent/proto"
	"go-micro.dev/v6/broker"
	"go-micro.dev/v6/client"
	"go-micro.dev/v6/metadata"
	"go-micro.dev/v6/registry"
	"go-micro.dev/v6/server"
)

type Agent struct {
	mu       sync.Mutex
	sessions []string
}

func (a *Agent) Chat(ctx context.Context, req *pb.ChatRequest, rsp *pb.ChatResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, _ := metadata.Get(ctx, "Micro-Session")
	a.sessions = append(a.sessions, id)
	rsp.Reply = req.Message
	rsp.RunId = "test-run"
	rsp.ToolCalls = []*pb.ToolCall{{Name: "test.Read", Input: "{}", Result: "read result"}}
	return nil
}

func TestWorkspaceThroughRPC(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	a := &Agent{}
	srv := server.NewServer(server.Name("example"), server.Address("127.0.0.1:0"), server.Registry(reg), server.Broker(broker.NewMemoryBroker()))
	if err := srv.Handle(srv.NewHandler(a)); err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	handler := New(reg, client.NewClient(client.Registry(reg)), "127.0.0.1:8080")
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		handler.ServeHTTP(rw, req)
		return rw
	}
	inventory := request("GET", "/api/services", "")
	var services []service
	if inventory.Code != 200 || json.Unmarshal(inventory.Body.Bytes(), &services) != nil || len(services) != 1 || !services[0].Agent || services[0].Instances != 1 {
		t.Fatal(inventory.Body.String())
	}
	sessions := []string{"7cba214f-e270-4d79-893d-fafedb0bef19", "7cba214f-e270-4d79-893d-fafedb0bef19", "bc3a2f2b-5e54-4fde-9463-5ce9077b6ddc"}
	for _, id := range sessions {
		result := request("POST", "/api/chat", `{"agent":"example","message":"hello","session":"`+id+`"}`)
		var reply pb.ChatResponse
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &reply) != nil || reply.Reply != "hello" || reply.RunId != "test-run" || len(reply.ToolCalls) != 1 {
			t.Fatal(result.Code, result.Body.String())
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sessions) != 3 || a.sessions[0] != a.sessions[1] || a.sessions[1] == a.sessions[2] || a.sessions[0] != "studio-"+sessions[0] {
		t.Fatal(a.sessions)
	}
	if r := request("POST", "/api/chat", `{"agent":"example","message":"hello"}`); r.Code != 400 {
		t.Fatal(r.Code)
	}
	if r := request("GET", "/api/alerts", ""); r.Code != 502 {
		t.Fatal("missing Alert must report an error", r.Code)
	}
	if r := request("GET", "/", ""); r.Code != 200 || !strings.Contains(r.Body.String(), "Micro Studio") {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestLocalBrowserBoundary(t *testing.T) {
	handler := New(registry.NewMemoryRegistry(), client.NewClient(), "127.0.0.1:8080")
	for _, tc := range []struct {
		host, origin, fetch, content string
		want                         int
	}{
		{"evil.example:8080", "", "", "application/json", 403},
		{"localhost:8080", "https://evil.example", "", "application/json", 403},
		{"localhost:8080", "", "cross-site", "application/json", 403},
		{"localhost:8080", "null", "", "application/json", 403},
		{"localhost:8080", "http://localhost:8080", "", "text/plain", 415},
		{"localhost:8081", "", "", "application/json", 403},
		{"localhost:8080", "http://localhost:8080", "", "application/json", 400},
	} {
		req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/api/chat", strings.NewReader(`{}`))
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Sec-Fetch-Site", tc.fetch)
		req.Header.Set("Content-Type", tc.content)
		rw := httptest.NewRecorder()
		handler.ServeHTTP(rw, req)
		if rw.Code != tc.want {
			t.Fatalf("%+v: got %d", tc, rw.Code)
		}
	}
}
