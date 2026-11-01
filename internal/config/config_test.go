package config

import (
	"encoding/json"
	"os"
	"reflect"

	"testing"
)

func TestDefaultsAndOverrides(t *testing.T) {
	for _, name := range []string{"SUPPORT_REPOSITORIES", "GITHUB_REPOSITORY", "DEVOPS_SERVICES"} {
		t.Setenv(name, "")
	}
	if got := SupportRepositories(); !reflect.DeepEqual(got, []string{"micro/go-micro", "micro/agents"}) {
		t.Fatal(got)
	}
	data, err := os.ReadFile("../../micro.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Services map[string]struct{ Path string }
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MONITOR_SERVICES", "")
	for _, name := range Agents() {
		if cfg.Services[name].Path != "./"+name {
			t.Fatalf("missing service %s", name)
		}
	}
	if len(Services()) != len(cfg.Services) {
		t.Fatal("systemd defaults differ from service set")
	}
	t.Setenv("GITHUB_REPOSITORY", "owner/legacy")
	if got := SupportRepositories(); !reflect.DeepEqual(got, []string{"owner/legacy"}) {
		t.Fatal(got)
	}
	t.Setenv("SUPPORT_REPOSITORIES", "owner/one,owner/two")
	if got := SupportRepositories(); !reflect.DeepEqual(got, []string{"owner/one", "owner/two"}) {
		t.Fatal(got)
	}
	t.Setenv("DEVOPS_SERVICES", "api.service,worker.service")
	if got := Services(); !reflect.DeepEqual(got, []string{"api.service", "worker.service"}) {
		t.Fatal(got)
	}
}
