// Package config holds defaults shared by the agents in this repository.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func SupportRepositories() []string {
	if value := os.Getenv("SUPPORT_REPOSITORIES"); value != "" {
		return strings.Split(value, ",")
	}
	if value := os.Getenv("GITHUB_REPOSITORY"); value != "" {
		return []string{value}
	}
	return []string{"micro/go-micro", "micro/studio"}
}

// Services uses the same set for DevOps and monitoring unless explicitly overridden.
func Services() []string {
	if value := os.Getenv("DEVOPS_SERVICES"); value != "" {
		return strings.Split(value, ",")
	}
	return []string{"micro@support.service", "micro@devops.service", "micro@release.service", "micro@monitor.service", "micro@alert.service", "micro@studio.service"}
}

// Agents is the default service set, regardless of how the processes are started.
func Agents() []string {
	if value := os.Getenv("MONITOR_SERVICES"); value != "" {
		return strings.Split(value, ",")
	}
	return []string{"support", "devops", "release", "monitor", "alert", "studio"}
}

// Project finds the module root when micro run starts an agent in its directory.
func Project() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("run from a Go project or set DEVOPS_SERVICES for systemd inspection")
		}
		dir = parent
	}
}
