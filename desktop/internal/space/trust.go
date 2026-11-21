package space

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
)

// 团队空间的连接谁都能改。有写权限的成员若把一条带凭据 (保险库条目或保存的凭据) 的共享连接
// 改指到自己的主机, 别人连接时就会把凭据交给那台机器。所以团队连接的「去哪、带什么凭据」
// 由本人确认: 本机记下确认过的指纹 (trust.json, 只在本机, 不同步), 本人保存时自动确认;
// 别人改过、或别人新建的带凭据连接, 第一次连接前要求确认 (错误码 6416)。

// trust 是一个团队空间里本人确认过的连接指纹。
type trust struct {
	path string
	mu   sync.Mutex
	m    map[string]string // 连接 id → 指纹
}

func openTrust(dir string) *trust {
	t := &trust{path: filepath.Join(dir, "trust.json"), m: map[string]string{}}
	if raw, err := os.ReadFile(t.path); err == nil {
		_ = json.Unmarshal(raw, &t.m)
	}
	if t.m == nil {
		t.m = map[string]string{}
	}
	return t
}

// fingerprint 覆盖连接会把凭据交给谁: 协议、主机、端口、用户名、凭据来源与引用、跳板机。
func fingerprint(c conn.Connection) string {
	h := sha256.New()
	for _, f := range []string{c.Protocol, c.Host, strconv.Itoa(c.Port), c.Username, c.Auth, c.VaultItem, c.KeyItem, c.Credential, c.Jump} {
		h.Write([]byte(f))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func (t *trust) set(c conn.Connection) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.m[c.ID] = fingerprint(c)
	raw, _ := json.Marshal(t.m)
	if err := os.WriteFile(t.path+".tmp", raw, 0o600); err == nil {
		_ = os.Rename(t.path+".tmp", t.path)
	}
}

func (t *trust) ok(c conn.Connection) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.m[c.ID] == fingerprint(c)
}

// NeedsConfirm 报告这条连接连接前是否要本人确认: 团队空间里带凭据、且当前内容不是本人确认过的。
// 不带凭据 (系统默认认证) 的连接没有可泄露的东西, 不打扰。
func (s *Space) NeedsConfirm(c conn.Connection) bool {
	if s.trust == nil || c.Auth == conn.AuthSystem || c.Auth == "" {
		return false
	}
	return !s.trust.ok(c)
}

// Confirm 记下本人确认了这条连接的当前内容。
func (s *Space) Confirm(c conn.Connection) {
	if s.trust != nil {
		s.trust.set(c)
	}
}
