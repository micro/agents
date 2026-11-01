// Package config holds defaults shared by the agents in this repository.
package config

import (
	"os"
	"strings"
)

func SupportRepositories() []string {
	if value := os.Getenv("SUPPORT_REPOSITORIES"); value != "" {
		return strings.Split(value, ",")
	}
	if value := os.Getenv("GITHUB_REPOSITORY"); value != "" {
		return []string{value}
	}
	return []string{"micro/go-micro", "micro/agents"}
}

// Services uses the same set for DevOps and monitoring unless explicitly overridden.
func Services() []string {
	if value := os.Getenv("DEVOPS_SERVICES"); value != "" {
		return strings.Split(value, ",")
	}
	return []string{"micro-support.service", "micro-devops.service", "micro-release.service", "micro-monitor.service"}
}
