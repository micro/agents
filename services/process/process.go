// Package process reads the local logs written by micro run for this project.
package process

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Process struct {
	logs  string
	dirs  map[string]string
	names []string
}
type ListRequest struct{}
type ListResponse struct {
	Services []string `json:"services"`
}
type LogsRequest struct {
	Service string `json:"service"`
	Lines   int    `json:"lines"`
}
type LogsResponse struct {
	Service   string `json:"service"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

func New(project, logs string, names []string) (*Process, error) {
	project, err := filepath.Abs(project)
	if err != nil {
		return nil, err
	}
	var config struct {
		Services map[string]struct{ Path string }
	}
	data, err := os.ReadFile(filepath.Join(project, "micro.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("read micro.json: %w", err)
		}
	}
	dirs := make(map[string]string, len(names))
	for _, name := range names {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
			return nil, fmt.Errorf("invalid service directory %q", name)
		}
		dir := config.Services[name].Path
		if dir == "" {
			dir = name
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(project, dir)
		}
		dirs[name] = filepath.Clean(dir)
	}
	return &Process{dirs: dirs, logs: logs, names: append([]string(nil), names...)}, nil
}
func (p *Process) List(_ context.Context, _ *ListRequest, rsp *ListResponse) error {
	rsp.Services = append([]string(nil), p.names...)
	return nil
}

// Logs reads up to 200 trailing lines from the exact micro run log for a service.
func (p *Process) Logs(ctx context.Context, req *LogsRequest, rsp *LogsResponse) error {
	allowed := false
	for _, name := range p.names {
		if req.Service == name {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("service is not configured")
	}
	lines := req.Lines
	if lines == 0 {
		lines = 100
	}
	if lines < 1 || lines > 200 {
		return fmt.Errorf("lines must be 1–200")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := p.dirs[req.Service]
	hash := fmt.Sprintf("%x", md5.Sum([]byte(dir)))[:8]
	path := filepath.Join(p.logs, req.Service+"-"+hash+".log")
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("no micro run log for %s: %w", req.Service, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("log must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, opened) {
		return fmt.Errorf("log changed while opening")
	}
	offset := info.Size() - 64*1024
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, 64*1024))
	if err != nil {
		return err
	}
	text := string(data)
	truncated := offset > 0
	if truncated {
		if _, tail, ok := strings.Cut(text, "\n"); ok {
			text = tail
		}
	}
	all := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
		truncated = true
	}
	*rsp = LogsResponse{Service: req.Service, Content: strings.Join(all, "\n"), Truncated: truncated}
	return nil
}
