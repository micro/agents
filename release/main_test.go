package main

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/micro/agents/services/github"
	micro "go-micro.dev/v6"
	"go-micro.dev/v6/agent"
	pb "go-micro.dev/v6/agent/proto"
	"go-micro.dev/v6/broker"
	"go-micro.dev/v6/client"
	"go-micro.dev/v6/model"
	"go-micro.dev/v6/registry"
	"go-micro.dev/v6/service"
	"go-micro.dev/v6/store"
)

// Actions is a fixture service with the same dispatch contract as GitHub.
type Actions struct{ calls atomic.Int32 }

func (a *Actions) Dispatch(ctx context.Context, req *github.DispatchRequest, rsp *github.DispatchResponse) error {
	a.calls.Add(1)
	*rsp = github.DispatchResponse{Accepted: true, ID: 42, URL: "https://github.com/micro/example/actions/runs/42"}
	return nil
}

type releaseModel struct{ opts model.Options }

func (m *releaseModel) Init(opts ...model.Option) error {
	for _, o := range opts {
		o(&m.opts)
	}
	return nil
}
func (m *releaseModel) Options() model.Options { return m.opts }
func (m *releaseModel) String() string         { return "release-fixture" }
func (m *releaseModel) Stream(context.Context, *model.Request, ...model.GenerateOption) (model.Stream, error) {
	return nil, model.ErrStreamingUnsupported
}
func (m *releaseModel) Generate(ctx context.Context, req *model.Request, _ ...model.GenerateOption) (*model.Response, error) {
	result := m.opts.ToolHandler(ctx, model.ToolCall{ID: "dispatch", Name: "release_Actions_Dispatch", Input: map[string]any{"inputs": map[string]any{"version": "1.2.3"}}})
	return &model.Response{Reply: result.Content}, nil
}

func TestReleaseApprovalOverChat(t *testing.T) {
	model.Register("release-fixture", func(opts ...model.Option) model.Model { m := &releaseModel{}; _ = m.Init(opts...); return m })
	for _, mode := range []string{"approve", "deny", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			reg := registry.NewMemoryRegistry()
			rpc := client.NewClient(client.Registry(reg), client.Retries(0))
			svc := micro.NewService("release", service.Address("127.0.0.1:0"), service.Registry(reg), service.Client(rpc), service.Broker(broker.NewMemoryBroker()), service.Store(store.NewMemoryStore()))
			actions := &Actions{}
			if err := svc.Handle(actions); err != nil {
				t.Fatal(err)
			}
			a := micro.NewAgent("release", agent.Provider("release-fixture"), micro.AgentWithApproval(approval("release", "micro/example", "release.yml", "main")))
			if err := svc.Handle(a); err != nil {
				t.Fatal(err)
			}
			if err := svc.Start(); err != nil {
				t.Fatal(err)
			}
			defer svc.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stream, err := pb.NewAgentInteractionService("release", rpc).Chat(agent.WithSession(ctx, "fixture"))
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if err := stream.Send(&pb.InteractionRequest{Message: "Release version 1.2.3"}); err != nil {
				t.Fatal(err)
			}
			approved := false
			for {
				event, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if event.Type == "approval" {
					approved = true
					if actions.calls.Load() != 0 {
						t.Fatal("dispatched before approval")
					}
					raw, _ := json.Marshal(event.ToolCall)
					if !strings.Contains(string(raw), "micro/example") || !strings.Contains(string(raw), "release.yml") || !strings.Contains(string(raw), "1.2.3") {
						t.Fatalf("approval lacks target: %s", raw)
					}
					if mode == "disconnect" {
						stream.Close()
						break
					}
					if err := stream.Send(&pb.InteractionRequest{ApprovalId: event.ApprovalId, Approved: mode == "approve"}); err != nil {
						t.Fatal(err)
					}
				}
				if event.Type == "done" {
					break
				}
			}
			if !approved {
				t.Fatal("approval not requested")
			}
			// Stop waits for any in-flight agent work before checking for late dispatches.
			if err := svc.Stop(); err != nil {
				t.Fatal(err)
			}
			want := int32(0)
			if mode == "approve" {
				want = 1
			}
			if got := actions.calls.Load(); got != want {
				t.Fatalf("dispatched %d times, want %d", got, want)
			}
		})
	}
}

func TestApprovalScope(t *testing.T) {
	check := approval("release", "micro/example", "release.yml", "main")
	for _, name := range []string{"release_Actions_Dispatch", "other_Actions_Dispatch", "delegate", "release_Actions_Unknown"} {
		d, err := check(context.Background(), model.ToolCall{Name: name})
		if err != nil || d.Status != agent.ApprovalDenied {
			t.Fatalf("%s: %+v %v", name, d, err)
		}
	}
	for _, name := range []string{"Workflows", "Workflow", "Runs", "Run", "Jobs"} {
		d, err := check(context.Background(), model.ToolCall{Name: "release_Actions_" + name})
		if err != nil || d.Status != agent.ApprovalApproved {
			t.Fatalf("%s: %+v %v", name, d, err)
		}
	}
}
