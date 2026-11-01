// Command release runs an agent that releases applications through GitHub Actions.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/micro/agents/services/github"
	micro "go-micro.dev/v6"
	"go-micro.dev/v6/agent"
	"go-micro.dev/v6/client"
	"go-micro.dev/v6/model"
	"go-micro.dev/v6/service"
)

//go:embed instructions.md
var instructions string

func main() {
	repository, workflow, ref := os.Getenv("GITHUB_REPOSITORY"), os.Getenv("RELEASE_WORKFLOW"), os.Getenv("RELEASE_REF")
	if ref == "" {
		ref = "main"
	}
	actions, err := github.NewActions(repository, os.Getenv("GITHUB_TOKEN"), workflow, ref)
	if err != nil {
		log.Fatal(err)
	}
	s := micro.NewService("release", service.Address("127.0.0.1:0"))
	s.Init()
	// A transport retry could start a second deployment after a lost response.
	if err := s.Client().Init(client.Retries(0)); err != nil {
		log.Fatal(err)
	}
	if err := s.Handle(actions); err != nil {
		log.Fatal(err)
	}
	a := micro.NewAgent("release",
		micro.AgentPrompt(instructions),
		micro.AgentMaxSteps(12),
		micro.AgentWithApproval(approval(s.Server().Options().Name, repository, workflow, ref)),
	)
	if err := s.Handle(a); err != nil {
		log.Fatal(err)
	}
	if err := s.Run(); err != nil {
		log.Fatal(err)
	}
}

func approval(name, repository, workflow, ref string) agent.ApprovalFunc {
	prefix := strings.ReplaceAll(name, ".", "_") + "_Actions_"
	return func(ctx context.Context, call model.ToolCall) (agent.ApprovalDecision, error) {
		switch call.Name {
		case prefix + "Workflow", prefix + "Runs", prefix + "Run", prefix + "Jobs":
			return agent.ApprovalDecision{Status: agent.ApprovalApproved}, nil
		case prefix + "Dispatch":
			// Include the fixed target in the approval, without changing tool arguments.
			call.Input = map[string]any{"repository": repository, "workflow": workflow, "ref": ref, "inputs": call.Input["inputs"]}
			return agent.RequestApproval(ctx, call)
		default:
			return agent.ApprovalDecision{Status: agent.ApprovalDenied, Reason: fmt.Sprintf("release does not allow tool %s", call.Name)}, nil
		}
	}
}
