package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestLockCancelsPendingUnlock 暂停真实 Restore/Unlock 的条目加载, 确保之后的锁定优先。
func TestLockCancelsPendingUnlock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bw uses a POSIX shell")
	}
	for _, restore := range []bool{true, false} {
		name := "unlock"
		if restore {
			name = "restore"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			started, release := filepath.Join(dir, "started"), filepath.Join(dir, "release")
			t.Setenv("CZLTERM_TEST_STARTED", started)
			t.Setenv("CZLTERM_TEST_RELEASE", release)
			path := filepath.Join(dir, "bw")
			script := `#!/bin/sh
case "$1" in
status) printf '%s\n' '{"status":"unlocked"}' ;;
unlock) printf '%s\n' 'saved-session' ;;
list)
  : > "$CZLTERM_TEST_STARTED"
  while [ ! -f "$CZLTERM_TEST_RELEASE" ]; do sleep 0.01; done
  printf '%s\n' '[{"id":"secret-item","name":"test"}]'
  ;;
esac
`
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			v := New(nil)
			v.Configure(path, 0)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				if restore {
					result <- v.Restore(ctx, "saved-session")
				} else {
					result <- v.Unlock(ctx, "password")
				}
			}()
			for {
				if _, err := os.Stat(started); err == nil {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("fake bw did not load items")
				case <-time.After(time.Millisecond):
				}
			}
			v.Lock()
			if err := os.WriteFile(release, nil, 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, ErrLocked) {
					t.Fatalf("pending %s returned %v, want ErrLocked", name, err)
				}
			case <-ctx.Done():
				t.Fatal("pending unlock did not finish")
			}
			if v.Session() != "" || v.items != nil {
				t.Fatal("pending unlock undid the lock")
			}
		})
	}
}

// TestNewerUnlockWins 验证即使未发生锁定, 旧解锁任务也不能覆盖新会话。
func TestNewerUnlockWins(t *testing.T) {
	v := New(nil)
	v.sessionGen = 2
	if err := v.commitSession(1, "old", nil); !errors.Is(err, ErrLocked) {
		t.Fatalf("stale commit = %v", err)
	}
	if err := v.commitSession(2, "new", map[string]item{}); err != nil {
		t.Fatal(err)
	}
	if v.Session() != "new" {
		t.Fatal("current commit did not install the new session")
	}
	v.Lock()
}
