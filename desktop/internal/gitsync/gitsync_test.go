package gitsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 两台设备经一个裸仓库同步: 各自新增的连接互相可见; 同一文件两边都改时报冲突且不丢本地提交。
func TestSyncTwoDevices(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", remote).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v %s", err, out)
	}
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	write := func(dir, name, body string) {
		os.MkdirAll(filepath.Join(dir, "connections"), 0o700)
		if err := os.WriteFile(filepath.Join(dir, "connections", name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(dir, name string) bool {
		_, err := os.Stat(filepath.Join(dir, "connections", name))
		return err == nil
	}

	write(a, "1.json", "{}\n")
	if err := Sync(ctx, a, remote, Auth{}); err != nil {
		t.Fatalf("sync a: %v", err)
	}
	write(b, "2.json", "{}\n")
	if err := Sync(ctx, b, remote, Auth{}); err != nil {
		t.Fatalf("sync b (unrelated history): %v", err)
	}
	if !exists(b, "1.json") {
		t.Fatal("b should receive a's connection")
	}
	if err := Sync(ctx, a, remote, Auth{}); err != nil {
		t.Fatalf("sync a again: %v", err)
	}
	if !exists(a, "2.json") {
		t.Fatal("a should receive b's connection")
	}

	write(a, "1.json", "{\"name\":\"a\"}\n")
	write(b, "1.json", "{\"name\":\"b\"}\n")
	if err := Sync(ctx, a, remote, Auth{}); err != nil {
		t.Fatalf("sync a edit: %v", err)
	}
	if err := Sync(ctx, b, remote, Auth{}); err == nil {
		t.Fatal("conflicting edit should fail")
	}
	data, _ := os.ReadFile(filepath.Join(b, "connections", "1.json"))
	if string(data) != "{\"name\":\"b\"}\n" {
		t.Fatalf("local edit lost after conflict: %q", data)
	}
}

func TestIsSSHRemote(t *testing.T) {
	for remote, want := range map[string]bool{
		"git@github.com:you/data.git":      true,
		"ssh://git@host:2222/you/data.git": true,
		"https://github.com/you/data.git":  false,
		"https://user@github.com/x.git":    false,
		"/srv/git/data.git":                false,
	} {
		if got := isSSHRemote(remote); got != want {
			t.Errorf("isSSHRemote(%q) = %v", remote, got)
		}
	}
}
