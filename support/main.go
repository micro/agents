// Command support runs an agent that drafts responses to GitHub issues.
package main

import (
	_ "embed"
	"log"
	"os"

	"github.com/micro/agents/services/github"
	micro "go-micro.dev/v6"
)

//go:embed instructions.md
var instructions string

func main() {
	repository := os.Getenv("GITHUB_REPOSITORY")
	if repository == "" {
		log.Fatal("set GITHUB_REPOSITORY to owner/name")
	}
	gh, err := github.New(repository, os.Getenv("GITHUB_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}
	service := micro.NewService("support")
	service.Init()
	if err := service.Handle(gh); err != nil {
		log.Fatal(err)
	}
	agent := micro.NewAgent("support",
		micro.AgentPrompt(instructions+"\nConfigured repository: "+repository),
		micro.AgentMaxSteps(12),
	)
	if err := service.Handle(agent); err != nil {
		log.Fatal(err)
	}
	if err := service.Run(); err != nil {
		log.Fatal(err)
	}
}
