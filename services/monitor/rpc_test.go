package monitor

import (
	"context"
	"testing"

	"github.com/micro/studio/services/health"
	"go-micro.dev/v6/broker"
	"go-micro.dev/v6/client"
	"go-micro.dev/v6/registry"
	"go-micro.dev/v6/server"
	"go-micro.dev/v6/store"
)

func TestRPCFailureRecoveryPersists(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	st := store.NewMemoryStore()
	cl := client.NewClient(client.Registry(reg))
	m, err := NewRPC([]string{"support"}, reg, cl, st)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	check := func(want string) {
		t.Helper()
		if err := m.sample(ctx); err != nil {
			t.Fatal(err)
		}
		var rsp StatusResponse
		m.Status(ctx, &StatusRequest{}, &rsp)
		if rsp.Services[0].Status != want {
			t.Fatalf("want %s: %+v", want, rsp)
		}
	}
	check("missing")
	srv := server.NewServer(server.Name("support"), server.Address("127.0.0.1:0"), server.Registry(reg), server.Broker(broker.NewMemoryBroker()))
	if err := srv.Handle(srv.NewHandler(&health.Health{})); err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	check("healthy")
	records, _ := reg.GetService("support")
	extra := &registry.Service{Name: "support", Version: records[0].Version, Nodes: []*registry.Node{{Id: "broken", Address: "127.0.0.1:1"}}}
	if err := reg.Register(extra); err != nil {
		t.Fatal(err)
	}
	check("unhealthy")
	reg.Deregister(extra)
	check("healthy")
	restarted, err := NewRPC([]string{"support"}, reg, cl, st)
	if err != nil {
		t.Fatal(err)
	}
	var history HistoryResponse
	restarted.History(ctx, &HistoryRequest{}, &history)
	if len(history.Events) != 4 || history.Events[0].Previous != "unhealthy" {
		t.Fatal(history)
	}
	srv.Stop()
	m = restarted
	check("missing")
}
