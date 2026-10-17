package vault

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestColdAccountSummary 不因账户摘要缺失阻塞解锁状态, 后台成功后补齐邮箱。
func TestColdAccountSummary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bw")
	release := filepath.Join(dir, "release")
	t.Setenv("CZLTERM_ACCOUNT_RELEASE", release)
	script := `#!/bin/sh
if [ "$1" = list ]; then printf '%s\n' '[]'; exit 0; fi
while [ ! -f "$CZLTERM_ACCOUNT_RELEASE" ]; do sleep 0.01; done
printf '%s\n' '{"status":"locked","userEmail":"test@example.test"}'
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0600) })
	v := New(nil)
	v.Configure(path, 0)
	if err := v.Restore(context.Background(), "session"); err != nil {
		t.Fatal(err)
	}
	status := make(chan Status, 1)
	go func() { status <- v.Status(context.Background()) }()
	select {
	case st := <-status:
		if st.Status != StatusUnlocked {
			t.Fatal(st)
		}
	case <-time.After(time.Second):
		t.Fatal("account query blocked unlock")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		v.mu.Lock()
		pending := v.accountPending
		v.mu.Unlock()
		if !pending {
			break
		}
		select {
		case <-deadline:
			t.Fatal("account query did not finish")
		case <-time.After(time.Millisecond):
		}
	}
	if st := v.Status(context.Background()); st.UserEmail != "test@example.test" || st.Status != StatusUnlocked {
		t.Fatalf("missing account summary: %+v", st)
	}
	v.Configure(filepath.Join(dir, "different-bw"), 0)
	v.mu.Lock()
	cached := v.accountStatus
	v.mu.Unlock()
	if cached.UserEmail != "" {
		t.Fatal("CLI path change retained old account")
	}
	v.Lock()
}
