package vault

import (
	"testing"
	"time"
)

// 已触发但被重设过的自动锁定计时器不能锁掉刚用过的保险库。
func TestStaleAutoLockIgnored(t *testing.T) {
	locked := 0
	v := New(func() { locked++ })
	v.mu.Lock()
	v.session, v.autoLock = "s", time.Hour
	v.armTimerLocked()
	stale := v.lockGen
	v.touchLocked() // 用户刚用过, 计时器重设
	v.mu.Unlock()

	v.lockIfCurrent(stale)
	if v.session == "" || locked != 0 {
		t.Fatal("stale timer locked the vault")
	}
	v.mu.Lock()
	cur := v.lockGen
	v.mu.Unlock()
	v.lockIfCurrent(cur)
	if v.session != "" || locked != 1 {
		t.Fatal("current timer should lock the vault")
	}
	v.Lock()
}

func TestSecretFallbacks(t *testing.T) {
	v := New(nil)
	v.session = "s"
	v.items = map[string]item{
		"a": {ID: "a", Notes: "-----BEGIN OPENSSH PRIVATE KEY-----\nx", Fields: []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{{Name: "passphrase", Value: "pp"}}},
	}
	s, err := v.Secret("a")
	if err != nil || s.Passphrase != "pp" || s.PrivateKey == "" {
		t.Fatalf("unexpected %+v %v", s, err)
	}
	if _, err := v.Secret("missing"); err == nil {
		t.Fatal("missing item should error")
	}
	v.Lock()
	if _, err := v.Secret("a"); err != ErrLocked {
		t.Fatalf("locked vault should return ErrLocked, got %v", err)
	}
}

func TestLastJSON(t *testing.T) {
	out := []byte("A new version is available\n{\"status\":\"locked\"}\n")
	if got := string(lastJSON(out)); got != `{"status":"locked"}` {
		t.Fatalf("got %q", got)
	}
}
