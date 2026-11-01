// Command support runs an agent that drafts responses to GitHub issues.
package main

import (
	_ "embed"
	"log"
	"os"

	"github.com/micro/agents/internal/config"
	"github.com/micro/agents/services/github"
	micro "go-micro.dev/v6"
	"go-micro.dev/v6/service"
)

//go:embed instructions.md
var instructions string

func main() {
	repositories := config.SupportRepositories()
	gh, err := github.New(repositories[0], os.Getenv("GITHUB_TOKEN"), repositories[1:]...)
	if err != nil {
		log.Fatal(err)
	}
	service := micro.NewService("support", service.Address("127.0.0.1:0"))
	service.Init()
	if err := service.Handle(gh); err != nil {
		log.Fatal(err)
	}
	agent := micro.NewAgent("support",
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
