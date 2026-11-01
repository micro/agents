package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultsAndOverrides(t *testing.T) {
	for _, name := range []string{"SUPPORT_REPOSITORIES", "GITHUB_REPOSITORY", "DEVOPS_SERVICES"} {
		t.Setenv(name, "")
	}
	if got := SupportRepositories(); !reflect.DeepEqual(got, []string{"micro/go-micro", "micro/agents"}) {
		t.Fatal(got)
	}
	for _, unit := range Services() {
		data, err := os.ReadFile(filepath.Join("..", "..", "deploy", "systemd", unit))
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(unit, "micro-"), ".service")
		if !strings.Contains(string(data), "ExecStart=/opt/micro-agents/"+name+"\n") {
			t.Fatalf("%s does not run %s", unit, name)
		}
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
