// Command monitor checks the agents' RPC health and explains their state.
package main

import (
	"context"
	_ "embed"
	"log"

	"github.com/micro/agents/internal/config"
	"github.com/micro/agents/services/health"
	"github.com/micro/agents/services/monitor"
	micro "go-micro.dev/v6"
	_ "go-micro.dev/v6/cmd/defaults"
	"go-micro.dev/v6/service"
	"go-micro.dev/v6/store"
)

//go:embed instructions.md
var instructions string

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	s := micro.NewService("monitor", service.Store(store.NewStore(store.Database("agents-monitor"))), service.Address("127.0.0.1:0"),
		service.BeforeStop(func() error { cancel(); <-done; return nil }),
	)
	s.Init()
	if err := s.Handle(&health.Health{}); err != nil {
		log.Fatal(err)
	}
	checks, err := monitor.NewRPC(config.Agents(), s.Options().Registry, s.Client(), s.Options().Store)
	if err != nil {
		log.Fatal(err)
	}
	if err := s.Handle(checks); err != nil {
		log.Fatal(err)
	}
	a := micro.NewAgent("monitor", micro.AgentPrompt(instructions), micro.AgentMaxSteps(6))
	if err := s.Handle(a); err != nil {
		log.Fatal(err)
	}
	go func() { defer close(done); monitor.Run(ctx, checks) }()
	if err := s.Run(); err != nil {
		log.Fatal(err)
	}
}
