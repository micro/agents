package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	micro "go-micro.dev/v6"
	"go-micro.dev/v6/broker"
	"go-micro.dev/v6/client"
	"go-micro.dev/v6/registry"
	"go-micro.dev/v6/selector"
	"go-micro.dev/v6/service"
)

func TestServiceWithAgent(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"number":42,"title":"Cannot start service"}`))
	}))
	defer api.Close()
	gh, _ := New("micro/go-micro", "")
	gh.baseURL = api.URL
	reg := registry.NewMemoryRegistry()
	cl := client.NewClient(client.Registry(reg), client.Selector(selector.NewSelector(selector.Registry(reg))))
	svc := micro.NewService("support", service.Address("127.0.0.1:0"), service.Registry(reg), service.Client(cl), service.Broker(broker.NewMemoryBroker()))
	if err := svc.Handle(gh); err != nil {
		t.Fatal(err)
	}
	if err := svc.Handle(micro.NewAgent("support", micro.AgentProvider("openai"))); err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result Result
	req := cl.NewRequest("support", "GitHub.Issue", &IssueRequest{Number: 42})
	// Use the actual bound address so the test does not depend on discovery timing.
	if err := cl.Call(ctx, req, &result, client.WithAddress(svc.Server().Options().Address)); err != nil {
		t.Fatal(err)
	}
	if result.Content != `{"number":42,"title":"Cannot start service"}` {
		t.Fatalf("unexpected RPC response: %+v", result)
	}
}
