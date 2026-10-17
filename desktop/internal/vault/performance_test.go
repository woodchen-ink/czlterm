package vault

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestUnlockedStatusDoesNotRunCLI 即使CLI路径不可用, 内存解锁状态也必须立即返回。
func TestUnlockedStatusDoesNotRunCLI(t *testing.T) {
	v := New(nil)
	v.session = "session"
	v.bwPath = filepath.Join(t.TempDir(), "missing-bw")
	v.accountStatus = Status{Status: StatusLocked, UserEmail: "test@example.test", ServerURL: "https://example.test"}
	st := v.Status(context.Background())
	if st.Status != StatusUnlocked || st.UserEmail != "test@example.test" || st.Error != "" {
		t.Fatalf("unexpected status: %+v", st)
	}
}

// TestRestoreOnlyLoadsItems 保存会话的成功恢复只允许一次 list, 不能额外启动 status。
func TestRestoreOnlyLoadsItems(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI")
	}
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "valid", false: "invalid"}[valid], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bw")
			script := "#!/bin/sh\n[ \"$1\" = list ] || exit 9\n"
			if valid {
				script += "printf '%s\\n' '[]'\n"
			} else {
				script += "echo 'Vault is locked.' >&2\nexit 1\n"
			}
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			v := New(nil)
			v.Configure(path, 0)
			err := v.Restore(context.Background(), "saved")
			if valid && (err != nil || v.Session() == "") {
				t.Fatalf("restore failed: %v", err)
			}
			if !valid && (err == nil || v.Session() != "") {
				t.Fatal("invalid session was accepted")
			}
		})
	}
}

// BenchmarkUnlockedStatus 测量已解锁状态查询, 不读取系统凭据或真实条目。
func BenchmarkUnlockedStatus(b *testing.B) {
	v := New(nil)
	v.session = "session"
	v.accountStatus = Status{Status: StatusLocked}
	for b.Loop() {
		v.Status(context.Background())
	}
}

// TestRefreshSharesInFlight 验证并发刷新合并且读取缓存不等待网络。
func TestRefreshSharesInFlight(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bw")
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")
	calls := filepath.Join(dir, "calls")
	t.Setenv("CZLTERM_TEST_STARTED", started)
	t.Setenv("CZLTERM_TEST_RELEASE", release)
	t.Setenv("CZLTERM_TEST_CALLS", calls)
	script := `#!/bin/sh
echo "$1" >> "$CZLTERM_TEST_CALLS"
if [ "$1" = sync ]; then
 : > "$CZLTERM_TEST_STARTED"
 while [ ! -f "$CZLTERM_TEST_RELEASE" ]; do sleep 0.01; done
else
 printf '%s\n' '[]'
fi
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	v := New(nil)
	v.Configure(path, 0)
	v.session = "session"
	v.items = map[string]item{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first := make(chan error, 1)
	go func() { first <- v.Refresh(ctx) }()
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("sync not started")
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := v.Search(""); err != nil {
		t.Fatal(err)
	}
	// 已有刷新时, 一个取消的等待者不能取消共享任务。
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := v.Refresh(canceled); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	v.mu.Lock()
	job := v.refreshJob
	v.mu.Unlock()
	if job == nil {
		t.Fatal("no in-flight refresh")
	}
	// 第二个调用取消等待前必须复用同一任务。
	waitCtx, stopWait := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stopWait()
	if err := v.Refresh(waitCtx); err != context.DeadlineExceeded {
		t.Fatalf("wait: %v", err)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "sync\nlist\n" {
		t.Fatalf("duplicate CLI calls: %s", data)
	}
}
