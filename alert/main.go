// Command alert delivers incident notifications and explains their status.
package main

import (
	"context"
	_ "embed"
	"github.com/micro/studio/services/alert"
	"github.com/micro/studio/services/health"
	micro "go-micro.dev/v6"
	_ "go-micro.dev/v6/cmd/defaults"
	"go-micro.dev/v6/service"
	"go-micro.dev/v6/store"
	"log"
	"os"
)

//go:embed instructions.md
var instructions string

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	s := micro.NewService("alert", service.Store(store.NewStore(store.Database("agents-alert"))), service.Address("127.0.0.1:0"), service.BeforeStop(func() error { cancel(); <-done; return nil }))
	s.Init()
	notifications, err := alert.New(s.Client(), s.Options().Store, os.Getenv("ALERT_WEBHOOK"))
	if err != nil {
		log.Fatal(err)
	}
	if err := s.Handle(&health.Health{}); err != nil {
		log.Fatal(err)
	}
	if err := s.Handle(notifications); err != nil {
		log.Fatal(err)
	}
	a := micro.NewAgent("alert", micro.AgentPrompt(instructions), micro.AgentMaxSteps(6))
	if err := s.Handle(a); err != nil {
		log.Fatal(err)
	}
	go func() { defer close(done); alert.Run(ctx, notifications) }()
	if err := s.Run(); err != nil {
		log.Fatal(err)
	}
}
