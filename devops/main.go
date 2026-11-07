// Command devops runs an agent that investigates local service failures.
package main

import (
	"context"
	_ "embed"
	"github.com/micro/studio/services/monitor"
	"github.com/micro/studio/services/process"
	"go-micro.dev/v6/client"
	"log"
	"os"
	"path/filepath"

	"github.com/micro/studio/internal/config"
	"github.com/micro/studio/services/health"
	"github.com/micro/studio/services/systemd"
	micro "go-micro.dev/v6"
	_ "go-micro.dev/v6/cmd/defaults"
	"go-micro.dev/v6/service"
	"go-micro.dev/v6/store"
)

//go:embed instructions.md
var instructions string

func main() {
	service := micro.NewService("devops", service.Store(store.NewStore(store.Database("agents-devops"))), service.Address("127.0.0.1:0"))
	service.Init()
	if err := service.Handle(&health.Health{}); err != nil {
		log.Fatal(err)
	}
	if os.Getenv("DEVOPS_SERVICES") != "" {
		host, err := systemd.New(config.Services())
		if err != nil {
			log.Fatal(err)
		}
		if err := service.Handle(host); err != nil {
			log.Fatal(err)
		}
	} else {
		project, err := config.Project()
		if err != nil {
			log.Fatal(err)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		logs, err := process.New(project, filepath.Join(home, "micro", "logs"), config.Agents())
		if err != nil {
			log.Fatal(err)
		}
		if err := service.Handle(logs); err != nil {
			log.Fatal(err)
		}
		// Reuse Monitor's RPC observations rather than maintaining a second health loop.
		if err := service.Handle(&Monitor{client: service.Client()}); err != nil {
			log.Fatal(err)
		}
	}
	agent := micro.NewAgent("devops",
		micro.AgentPrompt(instructions),
		micro.AgentMaxSteps(24),
	)
	if err := service.Handle(agent); err != nil {
		log.Fatal(err)
	}
	if err := service.Run(); err != nil {
		log.Fatal(err)
	}
}

// Monitor forwards observations from the monitoring service for local investigations.
type Monitor struct{ client client.Client }

func (m *Monitor) Status(ctx context.Context, req *monitor.StatusRequest, rsp *monitor.StatusResponse) error {
	return m.client.Call(ctx, m.client.NewRequest("monitor", "Monitor.Status", req), rsp, client.WithRetries(0))
}
