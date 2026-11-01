// Command devops runs an agent that investigates local service failures.
package main

import (
	_ "embed"
	"log"

	"github.com/micro/agents/internal/config"
	"github.com/micro/agents/services/systemd"
	micro "go-micro.dev/v6"
	"go-micro.dev/v6/service"
)

//go:embed instructions.md
var instructions string

func main() {
	host, err := systemd.New(config.Services())
	if err != nil {
		log.Fatal(err)
	}
	service := micro.NewService("devops", service.Address("127.0.0.1:0"))
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
