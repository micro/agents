// Command monitor watches the agents' systemd services and explains their state.
package main

import (
	"context"
	_ "embed"
	"log"
	"os"
	"strings"

	"github.com/micro/agents/internal/config"
	"github.com/micro/agents/services/monitor"
	"github.com/micro/agents/services/systemd"
	micro "go-micro.dev/v6"
	"go-micro.dev/v6/service"
)

//go:embed instructions.md
var instructions string

func main() {
	units := config.Services()
	if value := os.Getenv("MONITOR_SERVICES"); value != "" {
		units = strings.Split(value, ",")
	}
	host, err := systemd.New(units)
	if err != nil {
		log.Fatal(err)
	}
	checks := monitor.New(host)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	s := micro.NewService("monitor", service.Address("127.0.0.1:0"),
		service.BeforeStop(func() error { cancel(); <-done; return nil }),
	)
	s.Init()
	if err := s.Handle(checks); err != nil {
		log.Fatal(err)
	}
	a := micro.NewAgent("monitor", micro.AgentPrompt(instructions), micro.AgentMaxSteps(6))
	if err := s.Handle(a); err != nil {
		log.Fatal(err)
	}
	go func() { defer close(done); checks.Run(ctx) }()
	if err := s.Run(); err != nil {
		log.Fatal(err)
	}
}
