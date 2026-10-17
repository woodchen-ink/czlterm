package vault

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestStatusUsesCurrentSession 控制 bw 返回时机, 覆盖状态查询与恢复/锁定并发的情况。
func TestStatusUsesCurrentSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bw uses a POSIX shell")
	}
	for _, tc := range []struct {
		name, before, after, cliStatus, want string
	}{
		{"restored during status", "", "restored", StatusLocked, StatusUnlocked},
		{"locked during status", "previous", "", StatusLocked, StatusLocked},
		{"external session is not ours", "", "", StatusUnlocked, StatusLocked},
		{"stale logout cannot clear restored session", "", "restored", StatusUnauthenticated, StatusUnlocked},
		{"current logout clears session", "previous", "previous", StatusUnauthenticated, StatusUnauthenticated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			started, release := filepath.Join(dir, "started"), filepath.Join(dir, "release")
			t.Setenv("CZLTERM_TEST_STARTED", started)
			t.Setenv("CZLTERM_TEST_RELEASE", release)
			path := filepath.Join(dir, "bw")
			script := "#!/bin/sh\n: > \"$CZLTERM_TEST_STARTED\"\nwhile [ ! -f \"$CZLTERM_TEST_RELEASE\" ]; do sleep 0.01; done\nprintf '%s\\n' '{\"status\":\"" + tc.cliStatus + "\"}'\n"
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			v := New(nil)
			v.Configure(path, 0)
			v.session = tc.before
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := make(chan Status, 1)
			go func() { result <- v.Status(ctx) }()
			for {
				if _, err := os.Stat(started); err == nil {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("fake bw did not start")
				case <-time.After(time.Millisecond):
				}
			}
			v.mu.Lock()
			if v.session != tc.after {
				v.sessionGen++
			}
			v.session = tc.after
			v.mu.Unlock()
			if err := os.WriteFile(release, nil, 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-result:
				if got.Status != tc.want {
					t.Fatalf("status = %q, want %q (%s)", got.Status, tc.want, got.Error)
				}
			case <-ctx.Done():
				t.Fatal("status did not finish")
			}
		})
	}
}
