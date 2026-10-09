package script

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	s := Script{Name: " restart ", Group: "/ops/", Content: "systemctl restart nginx\r\n"}
	if err := s.Normalize(); err != nil {
		t.Fatal(err)
	}
	if s.Name != "restart" || s.Group != "ops" || s.Content != "systemctl restart nginx\n" || s.UpdatedAt == "" {
		t.Errorf("unexpected %+v", s)
	}
	for _, bad := range []Script{
		{Name: "", Content: "ls"},
		{Name: "x", Content: "  \n"},
		{Name: "x", Content: strings.Repeat("a", MaxContentBytes+1)},
		{Name: "x", Content: "#!/bin/bash;rm\nls"},
		{Name: "x", Content: "#!\nls"},
	} {
		if err := bad.Normalize(); err == nil {
			t.Errorf("expected error for %q", bad.Content)
		}
	}
}

func TestInterpreter(t *testing.T) {
	for content, want := range map[string]string{
		"ls":                         "sh",
		"#!/bin/bash\nls":            "/bin/bash",
		"#! /usr/bin/env  python3\n": "/usr/bin/env python3",
	} {
		got, err := Interpreter(content)
		if err != nil || got != want {
			t.Errorf("Interpreter(%q) = %q, %v; want %q", content, got, err, want)
		}
	}
}

// TestRemoteCommand 在本机 sh 里跑生成的命令, 确认脚本原样执行、临时文件被删、最后 exec 登录 shell。
func TestRemoteCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a posix shell")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	content := "#!/bin/sh\necho \"it's $((1+2))\" > " + out + "\nexit 3\n"
	cmd, err := RemoteCommand(content)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cmd, `"`) {
		t.Errorf("command must not contain double quotes: %s", cmd)
	}
	c := exec.Command("sh", "-c", cmd)
	// 用 true 代替登录 shell, 命令结束即退出。
	c.Env = append(os.Environ(), "SHELL=true", "TMPDIR="+dir)
	got, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, got)
	}
	if !strings.Contains(string(got), "exited with code 3") {
		t.Errorf("missing exit code in output: %s", got)
	}
	data, err := os.ReadFile(out)
	if err != nil || string(data) != "it's 3\n" {
		t.Errorf("script output = %q, %v", data, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
}

func TestStore(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Save(Script{Name: "b", Content: "ls"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Script{Name: "a", Group: "ops", Content: "ls"}); err != nil {
		t.Fatal(err)
	}
	list, errs := s.List()
	if len(errs) != 0 || len(list) != 2 || list[0].Name != "b" || list[1].Group != "ops" {
		t.Errorf("unexpected list %+v %v", list, errs)
	}
	if err := s.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(b.ID); err == nil {
		t.Error("deleted script still readable")
	}
	if _, err := s.Get("../x"); err == nil {
		t.Error("invalid id accepted")
	}
}
