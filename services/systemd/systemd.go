// Package systemd provides read-only inspection of configured local services.
package systemd

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var unitName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*\.service$`)

// Systemd can inspect only the units supplied to New. It never uses a shell.
type Systemd struct {
	units []string
	run   func(context.Context, string, ...string) (string, bool, error)
}

func New(units []string) (*Systemd, error) {
	if len(units) == 0 || len(units) > 16 {
		return nil, fmt.Errorf("configure 1–16 systemd service names")
	}
	s := &Systemd{run: command}
	seen := make(map[string]bool)
	for _, unit := range units {
		unit = strings.TrimSpace(unit)
		if !unitName.MatchString(unit) {
			return nil, fmt.Errorf("invalid service %q: use an exact name such as api.service", unit)
		}
		if !seen[unit] {
			s.units = append(s.units, unit)
			seen[unit] = true
		}
	}
	return s, nil
}

type ListRequest struct{}
type ListResponse struct {
	Services []string `json:"services"`
}
type StatusRequest struct {
	Service string `json:"service"`
}
type LogsRequest struct {
	Service string `json:"service"`
	Minutes int    `json:"minutes"`
	Lines   int    `json:"lines"`
}

// Result contains command output as evidence. Truncated means output exceeded
// the byte limit; journal queries are also limited by the requested time/lines.
type Result struct {
	Service    string `json:"service"`
	ObservedAt string `json:"observed_at"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated"`
}

// List returns the configured services, not an inventory of the whole machine.
func (s *Systemd) List(_ context.Context, _ *ListRequest, rsp *ListResponse) error {
	rsp.Services = append([]string(nil), s.units...)
	return nil
}

// Status reads current state and the most recent process exit information.
func (s *Systemd) Status(ctx context.Context, req *StatusRequest, rsp *Result) error {
	if err := s.allowed(req.Service); err != nil {
		return err
	}
	return s.read(ctx, req.Service, rsp, "systemctl", "show", "--no-pager", "--property=Id,LoadState,ActiveState,SubState,Result,ExecMainCode,ExecMainStatus,NRestarts,ActiveEnterTimestamp,InactiveEnterTimestamp", "--", req.Service)
}

// Logs reads recent journal entries for one configured service on this host.
func (s *Systemd) Logs(ctx context.Context, req *LogsRequest, rsp *Result) error {
	if err := s.allowed(req.Service); err != nil {
		return err
	}
	minutes, lines := req.Minutes, req.Lines
	if minutes == 0 {
		minutes = 60
	}
	if lines == 0 {
		lines = 100
	}
	if minutes < 1 || minutes > 1440 || lines < 1 || lines > 200 {
		return fmt.Errorf("minutes must be 1–1440 and lines must be 1–200")
	}
	return s.read(ctx, req.Service, rsp, "journalctl", "--no-pager", "--utc", "--output=short-iso-precise", "--unit="+req.Service, "--since=-"+strconv.Itoa(minutes)+"min", "--lines="+strconv.Itoa(lines))
}

func (s *Systemd) allowed(unit string) error {
	for _, configured := range s.units {
		if unit == configured {
			return nil
		}
	}
	return fmt.Errorf("service %q is not configured", unit)
}

func (s *Systemd) read(ctx context.Context, unit string, rsp *Result, name string, args ...string) error {
	content, truncated, err := s.run(ctx, name, args...)
	if err != nil {
		return err
	}
	*rsp = Result{Service: unit, ObservedAt: time.Now().UTC().Format(time.RFC3339), Content: content, Truncated: truncated}
	return nil
}

type limitedBuffer struct {
	data      []byte
	limit     int
	truncated bool
}

func (b *limitedBuffer) Len() int       { return len(b.data) }
func (b *limitedBuffer) String() string { return string(b.data) }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > b.limit-b.Len() {
		p = p[:b.limit-b.Len()]
		b.truncated = true
	}
	b.data = append(b.data, p...)
	return n, nil
}

func command(ctx context.Context, name string, args ...string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	out, stderr := &limitedBuffer{limit: 64 * 1024}, &limitedBuffer{limit: 2048}
	cmd.Stdout, cmd.Stderr = out, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		return "", false, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	// Journal permission warnings can accompany a successful exit. Preserve them
	// so the agent cannot mistake inaccessible logs for an empty journal.
	if stderr.Len() != 0 {
		return "", false, fmt.Errorf("%s: %s", name, strings.TrimSpace(stderr.String()))
	}
	return out.String(), out.truncated, nil
}
