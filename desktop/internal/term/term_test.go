package term

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// shellSpec 返回一条打印 marker 后以 code 退出的命令。
func shellSpec(marker string, code string) Spec {
	if runtime.GOOS == "windows" {
		return Spec{Program: "cmd.exe", Args: []string{"/c", "echo " + marker + " & exit " + code}}
	}
	return Spec{Program: "/bin/sh", Args: []string{"-c", "echo " + marker + "; exit " + code}}
}

func waitDone(t *testing.T, s *Session) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("session did not exit")
	}
}

func TestSessionRunsAndCleansUp(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "proxy.json")
	if err := os.WriteFile(tmp, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	exited := make(chan Info, 1)
	m := NewManager(func(i Info) { exited <- i })
	spec := shellSpec("czl-marker", "3")
	spec.ConnID, spec.Title, spec.CleanupFiles = "c1", "box", []string{tmp}
	s, err := m.Create(spec, 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)

	info := <-exited
	if !info.Exited || info.ExitCode != 3 || info.ConnID != "c1" || info.Title != "box" {
		t.Fatalf("unexpected info %+v", info)
	}
	replay, sub := s.Attach()
	if sub != nil {
		t.Fatal("attach after exit should not subscribe")
	}
	if !strings.Contains(string(replay), "czl-marker") {
		t.Fatalf("replay missing output: %q", replay)
	}
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup file still exists: %v", err)
	}
	if got := m.Running(); got != 0 {
		t.Fatalf("running = %d", got)
	}
	if err := m.Close(info.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(info.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after close: %v", err)
	}
}

func TestCloseKillsRunningSession(t *testing.T) {
	m := NewManager(nil)
	spec := Spec{Program: "/bin/sh", Args: []string{"-c", "sleep 30"}}
	if runtime.GOOS == "windows" {
		spec = Spec{Program: "cmd.exe", Args: []string{"/c", "ping -n 30 127.0.0.1 >nul"}}
	}
	s, err := m.Create(spec, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m.Running() != 1 {
		t.Fatalf("running = %d", m.Running())
	}
	start := time.Now()
	m.CloseAll(context.Background())
	waitDone(t, s)
	if time.Since(start) > 5*time.Second {
		t.Fatal("close took too long")
	}
	if len(m.List()) != 0 {
		t.Fatal("sessions left after CloseAll")
	}
}

func TestBuildEnvOverrides(t *testing.T) {
	env := buildEnv([]string{"PATH=/bin", "SSH_AUTH_SOCK=/old", "TERM=dumb"}, []string{"SSH_AUTH_SOCK=/new"})
	joined := strings.Join(env, "\n")
	for _, want := range []string{"PATH=/bin", "SSH_AUTH_SOCK=/new", "TERM=xterm-256color"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("env missing %s: %v", want, env)
		}
	}
	for _, bad := range []string{"SSH_AUTH_SOCK=/old", "TERM=dumb"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("env kept %s: %v", bad, env)
		}
	}
}

// 键盘输入经 Write 进入伪终端, 命令能读到。
func TestWriteReachesCommand(t *testing.T) {
	m := NewManager(nil)
	spec := Spec{Program: "/bin/sh", Args: []string{"-c", "read x; echo GOT$x"}}
	if runtime.GOOS == "windows" {
		spec = Spec{Program: "cmd.exe", Args: []string{"/v:on", "/c", "set /p x=&& echo GOT!x!"}}
	}
	s, err := m.Create(spec, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer m.CloseAll(context.Background())
	time.Sleep(300 * time.Millisecond)
	if err := s.Write([]byte("hello\r")); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	if replay, _ := s.Attach(); !strings.Contains(string(replay), "GOThello") {
		t.Fatalf("input not echoed: %q", replay)
	}
}
