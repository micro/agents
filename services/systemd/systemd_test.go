package systemd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go-micro.dev/v6/broker"
	"go-micro.dev/v6/client"
	"go-micro.dev/v6/registry"
	"go-micro.dev/v6/server"
)

func TestScopeAndArguments(t *testing.T) {
	for _, units := range [][]string{nil, {"--all"}, {"*.service"}, {"api.service;id"}, {"../api.service"}, {"api.timer"}} {
		if _, err := New(units); err == nil {
			t.Fatalf("accepted %v", units)
		}
	}
	s, err := New([]string{"api.service", " api.service ", "worker@one.service"})
	if err != nil {
		t.Fatal(err)
	}
	var list ListResponse
	_ = s.List(context.Background(), &ListRequest{}, &list)
	if !reflect.DeepEqual(list.Services, []string{"api.service", "worker@one.service"}) {
		t.Fatal(list)
	}
	calls := 0
	s.run = func(_ context.Context, name string, args ...string) (string, bool, error) {
		calls++
		if calls == 1 {
			if name != "systemctl" || args[len(args)-1] != "api.service" || args[len(args)-2] != "--" {
				t.Fatalf("%s %v", name, args)
			}
			return "ActiveState=failed\nExecMainStatus=1\n", false, nil
		}
		want := []string{"--no-pager", "--utc", "--output=short-iso-precise", "--unit=api.service", "--since=-60min", "--lines=100"}
		if name != "journalctl" || !reflect.DeepEqual(args, want) {
			t.Fatalf("%s %v", name, args)
		}
		return "2026-11-01T09:00:00+00:00 config missing", true, nil
	}
	var result Result
	if err := s.Status(context.Background(), &StatusRequest{Service: "api.service"}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Service != "api.service" || !strings.Contains(result.Content, "failed") || result.ObservedAt == "" {
		t.Fatal(result)
	}
	if err := s.Logs(context.Background(), &LogsRequest{Service: "api.service"}, &result); err != nil || !result.Truncated {
		t.Fatalf("%+v %v", result, err)
	}
	for _, req := range []LogsRequest{{Service: "other.service"}, {Service: "api.service", Minutes: -1}, {Service: "api.service", Lines: 201}, {Service: "api.service", Minutes: 1441}} {
		if err := s.Logs(context.Background(), &req, &result); err == nil {
			t.Fatalf("accepted %+v", req)
		}
	}
	if err := s.Status(context.Background(), &StatusRequest{Service: "--all"}, &result); err == nil {
		t.Fatal("accepted unconfigured service")
	}
	if calls != 2 {
		t.Fatalf("invalid request executed a command: %d", calls)
	}
}

func TestCommandLimitsAndErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write("printf 'permission denied' >&2\nexit 1\n")
	if _, _, err := command(context.Background(), path); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("lost failure: %v", err)
	}
	write("printf 'not seeing other users logs' >&2\nexit 0\n")
	if _, _, err := command(context.Background(), path); err == nil {
		t.Fatal("permission warning treated as empty logs")
	}
	write("exec sleep 30\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, _, err := command(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
	write("printf '%s' \"$1\"\n")
	out, cut, err := command(context.Background(), path, strings.Repeat("x", 70*1024))
	if err != nil || !cut || len(out) != 64*1024 {
		t.Fatalf("size=%d cut=%v err=%v", len(out), cut, err)
	}
}

func TestSystemdOverRPC(t *testing.T) {
	s, err := New([]string{"api.service"})
	if err != nil {
		t.Fatal(err)
	}
	s.run = func(context.Context, string, ...string) (string, bool, error) {
		return "ActiveState=failed\n", false, nil
	}
	reg := registry.NewMemoryRegistry()
	host := server.NewServer(server.Name("devops"), server.Address("127.0.0.1:0"), server.Registry(reg), server.Broker(broker.NewMemoryBroker()))
	if err := host.Handle(host.NewHandler(s)); err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer host.Stop()
	cl := client.NewClient(client.Registry(reg))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var rsp Result
	if err := cl.Call(ctx, cl.NewRequest("devops", "Systemd.Status", &StatusRequest{Service: "api.service"}), &rsp); err != nil {
		t.Fatal(err)
	}
	if rsp.Content != "ActiveState=failed\n" {
		t.Fatal(rsp)
	}
	if err := cl.Call(ctx, cl.NewRequest("devops", "Systemd.Status", &StatusRequest{Service: "private.service"}), &rsp); err == nil {
		t.Fatal("RPC bypassed scope")
	}
}
