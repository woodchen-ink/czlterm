// Package settings 读写本机设置 (`config/settings.json`)。
//
// 设置只属于这台机器 (终端、编辑器、bw 路径因系统而异), 不放进同步的数据目录。
package settings

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Settings 是全部本机设置。
type Settings struct {
	Terminal string `json:"terminal"`
	Editor   string `json:"editor"`
	// EditorPath 非空时忽略 Editor, 直接用该程序打开文件。
	EditorPath string `json:"editorPath"`
	// VNCViewerPath 是 Windows / Linux 上 VNC 查看器的路径, 为空时自动查找 TigerVNC。
	VNCViewerPath string `json:"vncViewerPath"`

	// BWPath 是 bw CLI 路径, 为空时在 PATH 中查找。
	BWPath string `json:"bwPath"`
	// VaultAutoLockMinutes 是保险库空闲多久后自动锁定, 0 表示直到退出。
	VaultAutoLockMinutes int `json:"vaultAutoLockMinutes"`
	// VaultRemember 为 true 时把 bw 会话密钥存进系统钥匙串, 重启后免输主密码;
	// 手动锁定或空闲超时锁定时删除。
	VaultRemember bool `json:"vaultRemember"`

	// GitRemote 是同步用的私有仓库地址, 为空时只做本地提交。
	GitRemote string `json:"gitRemote"`
	// GitUsername 是 HTTPS 仓库的用户名, 可选; 对应的令牌 / SSH 私钥存在系统密钥库 (internal/secret)。
	GitUsername string `json:"gitUsername"`
	// GitAutoSync 为 true 时每次修改连接后自动同步。
	GitAutoSync bool `json:"gitAutoSync"`

	MCPEnabled bool `json:"mcpEnabled"`
	// MCPAllowAccess 允许 AI 连到服务器上列目录、读文件 (全部 SSH 连接); 关闭时只能列出连接。
	// MCPAllowWrite 允许 AI 写文件; MCPAllowExec 允许 AI 执行命令。两者都以 MCPAllowAccess 为前提。
	MCPAllowAccess bool   `json:"mcpAllowAccess"`
	MCPAllowWrite  bool   `json:"mcpAllowWrite"`
	MCPAllowExec   bool   `json:"mcpAllowExec"`
	MCPToken       string `json:"mcpToken"`
}

// Store 管理设置文件。
type Store struct {
	path string
	mu   sync.Mutex
	cur  Settings
}

// Open 读取设置, 文件不存在时使用默认值。defaults 由调用方按系统给出。
func Open(configDir string, defaults Settings) (*Store, error) {
	s := &Store{path: filepath.Join(configDir, "settings.json"), cur: defaults}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("8001 read settings: %w", err)
	}
	if err := json.Unmarshal(data, &s.cur); err != nil {
		return s, fmt.Errorf("8002 parse settings: %w", err)
	}
	migrate(data, &s.cur)
	return s, nil
}

// migrate 补齐旧版设置文件里没有的新字段, 让升级后的行为与升级前一致。
func migrate(raw []byte, cur *Settings) {
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return
	}
	// mcpAllowAccess 是后加的总开关, 旧版里写文件 / 执行命令本身就隐含了访问服务器。
	if _, ok := keys["mcpAllowAccess"]; !ok && (cur.MCPAllowWrite || cur.MCPAllowExec) {
		cur.MCPAllowAccess = true
	}
}

// Get 返回当前设置的副本。
func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// Update 在锁内修改设置并落盘。
func (s *Store) Update(fn func(*Settings)) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cur
	fn(&next)
	next.BWPath = strings.TrimSpace(next.BWPath)
	next.EditorPath = strings.TrimSpace(next.EditorPath)
	next.VNCViewerPath = strings.TrimSpace(next.VNCViewerPath)
	next.GitRemote = strings.TrimSpace(next.GitRemote)
	next.GitUsername = strings.TrimSpace(next.GitUsername)
	if next.VaultAutoLockMinutes < 0 {
		next.VaultAutoLockMinutes = 0
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return s.cur, fmt.Errorf("8003 encode settings: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return s.cur, fmt.Errorf("8004 write settings: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return s.cur, fmt.Errorf("8004 write settings: %w", err)
	}
	s.cur = next
	return next, nil
}

// NewToken 生成 MCP 令牌。
func NewToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
