package process

import (
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopedBoundedLogs(t *testing.T) {
	root := t.TempDir()
	logs := t.TempDir()
	p, err := New(root, logs, []string{"support"})
	if err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", md5.Sum([]byte(filepath.Join(root, "support"))))[:8]
	path := filepath.Join(logs, "support-"+hash+".log")
	if err := os.WriteFile(path, []byte(strings.Repeat("old\n", 20000)+"recent failure\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var rsp LogsResponse
	if err := p.Logs(context.Background(), &LogsRequest{Service: "support", Lines: 1}, &rsp); err != nil {
		t.Fatal(err)
	}
	if rsp.Content != "recent failure" || !rsp.Truncated {
		t.Fatal(rsp)
	}
	if err := p.Logs(context.Background(), &LogsRequest{Service: "../private"}, &rsp); err == nil {
		t.Fatal("accepted traversal")
	}
	os.Remove(path)
	os.Symlink(filepath.Join(root, "secret"), path)
	if err := p.Logs(context.Background(), &LogsRequest{Service: "support"}, &rsp); err == nil {
		t.Fatal("followed symlink")
	}
}
