// Command devops runs an agent that investigates local service failures.
package main

import (
	_ "embed"
	"log"
	"os"
	"strings"

	"github.com/micro/agents/services/systemd"
	micro "go-micro.dev/v6"
)

//go:embed instructions.md
var instructions string

func main() {
	units := os.Getenv("DEVOPS_SERVICES")
	if units == "" {
		log.Fatal("set DEVOPS_SERVICES to comma-separated service names, e.g. api.service,worker.service")
	}
	host, err := systemd.New(strings.Split(units, ","))
	if err != nil {
		log.Fatal(err)
	}
	service := micro.NewService("devops")
	service.Init()
	if err := service.Handle(host); err != nil {
		log.Fatal(err)
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
