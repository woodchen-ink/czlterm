// Package vault 通过 Bitwarden 官方 CLI (`bw`) 读取 Vaultwarden 里的密码与私钥。
//
// 登录 (`bw config server` + `bw login`) 由用户在终端里做一次: 涉及两步验证、新设备确认等交互,
// 不在程序里重做。程序只负责解锁: 主密码经环境变量交给 `bw unlock`, 得到的会话密钥只留在内存,
// 通过 BW_SESSION 环境变量传给后续 `bw` 调用, 不进命令行参数 (同用户的其它进程能看到参数)。
//
// 解锁后一次性 `bw list items` 把条目缓存在内存: `bw` 是 node 程序, 每次调用一到两秒,
// 逐条 `bw get` 会让连接按钮明显卡顿。缓存随锁定、自动锁定或退出清空, 不落盘。
package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/platform"
)

// 状态取值, 与 `bw status` 一致。
const (
	StatusUnavailable     = "unavailable" // 找不到 bw
	StatusUnauthenticated = "unauthenticated"
	StatusLocked          = "locked"
	StatusUnlocked        = "unlocked"
)

// Bitwarden 条目类型。
const (
	TypeLogin  = 1
	TypeNote   = 2
	TypeSSHKey = 5
)

// ErrLocked 表示需要先解锁。
var ErrLocked = errors.New("3001 vault is locked")

// Status 是给界面展示的保险库状态。
type Status struct {
	Status    string `json:"status"`
	ServerURL string `json:"serverUrl"`
	UserEmail string `json:"userEmail"`
	BWPath    string `json:"bwPath"`
	LastSync  string `json:"lastSync"`
	// Error 是找不到 bw 或调用失败时的原因, 界面据此提示安装或登录。
	Error string `json:"error"`
}

// ItemSummary 是条目的非敏感摘要, 用于界面里选择条目。
type ItemSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     int    `json:"type"`
	Username string `json:"username"`
	// HasPassword / HasKey 让界面提示这个条目能当哪种凭据用。
	HasPassword bool `json:"hasPassword"`
	HasKey      bool `json:"hasKey"`
}

// Secret 是从条目取出的凭据。只在内存中流转, 不序列化给前端。
type Secret struct {
	Username   string
	Password   string
	PrivateKey string
	// Passphrase 是加密私钥的口令, 取自条目里名为 passphrase 的自定义字段, 没有时用 Password。
	Passphrase string
}

type item struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Notes string `json:"notes"`
	Login *struct {
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"login"`
	SSHKey *struct {
		PrivateKey string `json:"privateKey"`
	} `json:"sshKey"`
	Fields []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"fields"`
}

// Vault 管理 bw 会话与条目缓存。并发安全。
type Vault struct {
	mu        sync.Mutex
	bwPath    string // 设置里手填的路径, 为空时在 PATH 中查找
	session   string
	items     map[string]item
	lastUsed  time.Time
	autoLock  time.Duration
	lockTimer *time.Timer
	// lockGen 每次重设计时器加一; 已触发、正在等锁的旧计时器据此放弃, 不锁掉刚用过或刚解锁的保险库。
	lockGen uint64
	onLock  func()
}

// New 创建 Vault。onLock 在自动锁定时回调, 用于通知界面。
func New(onLock func()) *Vault {
	return &Vault{onLock: onLock}
}

// Configure 更新 bw 路径与自动锁定时长 (0 表示直到退出)。
func (v *Vault) Configure(bwPath string, autoLock time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.bwPath = strings.TrimSpace(bwPath)
	v.autoLock = autoLock
	v.armTimerLocked()
}

// Status 查询 bw 状态。已解锁时直接返回内存状态, 不再调用 bw。
func (v *Vault) Status(ctx context.Context) Status {
	v.mu.Lock()
	path, err := v.resolveLocked()
	unlocked := v.session != ""
	v.mu.Unlock()

	if err != nil {
		return Status{Status: StatusUnavailable, Error: err.Error()}
	}
	out, err := v.run(ctx, path, "", nil, "status")
	if err != nil {
		return Status{Status: StatusUnavailable, BWPath: path, Error: err.Error()}
	}
	var st Status
	if err := json.Unmarshal(lastJSON(out), &st); err != nil {
		return Status{Status: StatusUnavailable, BWPath: path, Error: "3002 unexpected bw status output"}
	}
	st.BWPath = path
	// 不带 BW_SESSION 调 status 时 bw 只会报 locked; 内存里有会话才算本程序已解锁。
	if st.Status == StatusLocked && unlocked {
		st.Status = StatusUnlocked
	}
	if st.Status == StatusUnauthenticated {
		v.Lock()
	}
	return st
}

// Unlock 用主密码解锁并加载条目。
func (v *Vault) Unlock(ctx context.Context, password string) error {
	if password == "" {
		return errors.New("3003 master password is required")
	}
	v.mu.Lock()
	path, err := v.resolveLocked()
	v.mu.Unlock()
	if err != nil {
		return err
	}

	out, err := v.run(ctx, path, "", []string{"CZLTERM_BW_PASSWORD=" + password},
		"unlock", "--passwordenv", "CZLTERM_BW_PASSWORD", "--raw")
	if err != nil {
		return fmt.Errorf("3004 unlock failed: %w", err)
	}
	session := strings.TrimSpace(string(out))
	if session == "" {
		return errors.New("3004 unlock failed: empty session")
	}
	items, err := v.loadItems(ctx, path, session)
	if err != nil {
		return err
	}

	v.mu.Lock()
	v.session = session
	v.items = items
	v.lastUsed = time.Now()
	v.armTimerLocked()
	v.mu.Unlock()
	return nil
}

// Session 返回当前会话密钥, 未解锁时为空。供「记住解锁」存进系统钥匙串。
func (v *Vault) Session() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.session
}

// Restore 用之前保存的会话密钥恢复解锁状态。会话已失效 (在别处执行过 bw lock / logout、
// 换了服务器) 时返回错误, 调用方应丢弃保存的会话。
func (v *Vault) Restore(ctx context.Context, session string) error {
	if session == "" {
		return ErrLocked
	}
	v.mu.Lock()
	path, err := v.resolveLocked()
	v.mu.Unlock()
	if err != nil {
		return err
	}
	out, err := v.run(ctx, path, session, nil, "status")
	if err != nil {
		return fmt.Errorf("3011 check saved session: %w", err)
	}
	var st Status
	if json.Unmarshal(lastJSON(out), &st) != nil || st.Status != StatusUnlocked {
		return errors.New("3012 saved session is no longer valid")
	}
	items, err := v.loadItems(ctx, path, session)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.session = session
	v.items = items
	v.lastUsed = time.Now()
	v.armTimerLocked()
	v.mu.Unlock()
	return nil
}

// Refresh 从服务器同步并重新加载条目。
func (v *Vault) Refresh(ctx context.Context) error {
	v.mu.Lock()
	path, session := v.bwPathLocked(), v.session
	v.mu.Unlock()
	if session == "" {
		return ErrLocked
	}
	if _, err := v.run(ctx, path, session, nil, "sync"); err != nil {
		return fmt.Errorf("3005 bw sync failed: %w", err)
	}
	items, err := v.loadItems(ctx, path, session)
	if err != nil {
		return err
	}
	v.mu.Lock()
	if v.session == session {
		v.items = items
	}
	v.mu.Unlock()
	return nil
}

// Lock 丢弃会话与缓存。不调用 `bw lock`: 那会让用户在终端里另开的 bw 会话一起失效。
func (v *Vault) Lock() {
	v.mu.Lock()
	v.lockLocked()
}

// lockIfCurrent 是自动锁定计时器的回调: 期间计时器被重设过就什么都不做。
func (v *Vault) lockIfCurrent(gen uint64) {
	v.mu.Lock()
	if gen != v.lockGen {
		v.mu.Unlock()
		return
	}
	v.lockLocked()
}

// lockLocked 在持有 v.mu 时调用, 返回前释放锁, 再在锁外回调 onLock。
func (v *Vault) lockLocked() {
	wasUnlocked := v.session != ""
	v.lockGen++
	v.session = ""
	v.items = nil
	if v.lockTimer != nil {
		v.lockTimer.Stop()
		v.lockTimer = nil
	}
	cb := v.onLock
	v.mu.Unlock()
	if wasUnlocked && cb != nil {
		cb()
	}
}

// Search 按名称或用户名搜索条目, 只返回登录与 SSH 密钥条目的摘要。query 为空时返回全部。
func (v *Vault) Search(query string) ([]ItemSummary, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.session == "" {
		return nil, ErrLocked
	}
	v.touchLocked()
	q := strings.ToLower(strings.TrimSpace(query))
	out := []ItemSummary{}
	for _, it := range v.items {
		s := summarize(it)
		if !s.HasPassword && !s.HasKey {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(s.Name), q) && !strings.Contains(strings.ToLower(s.Username), q) {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// Summary 返回单个条目的摘要; 已锁定或条目不存在时 ok 为 false。
func (v *Vault) Summary(id string) (ItemSummary, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	it, ok := v.items[id]
	if !ok {
		return ItemSummary{}, false
	}
	return summarize(it), true
}

// Secret 取条目里的凭据。
func (v *Vault) Secret(id string) (Secret, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.session == "" {
		return Secret{}, ErrLocked
	}
	it, ok := v.items[id]
	if !ok {
		return Secret{}, fmt.Errorf("3006 vault item not found, refresh the vault or pick another item")
	}
	v.touchLocked()
	var s Secret
	if it.Login != nil {
		s.Username, s.Password = it.Login.Username, it.Login.Password
	}
	if it.SSHKey != nil {
		s.PrivateKey = it.SSHKey.PrivateKey
	}
	for _, f := range it.Fields {
		switch strings.ToLower(f.Name) {
		case "passphrase":
			s.Passphrase = f.Value
		case "private_key", "privatekey", "ssh_key":
			if s.PrivateKey == "" {
				s.PrivateKey = f.Value
			}
		case "username":
			if s.Username == "" {
				s.Username = f.Value
			}
		}
	}
	// 安全笔记里直接贴私钥也是常见存法。
	if s.PrivateKey == "" && strings.Contains(it.Notes, "PRIVATE KEY-----") {
		s.PrivateKey = it.Notes
	}
	if s.Passphrase == "" {
		s.Passphrase = s.Password
	}
	return s, nil
}

func summarize(it item) ItemSummary {
	s := ItemSummary{ID: it.ID, Name: it.Name, Type: it.Type}
	if it.Login != nil {
		s.Username = it.Login.Username
		s.HasPassword = it.Login.Password != ""
	}
	s.HasKey = it.SSHKey != nil && it.SSHKey.PrivateKey != "" || strings.Contains(it.Notes, "PRIVATE KEY-----")
	for _, f := range it.Fields {
		switch strings.ToLower(f.Name) {
		case "private_key", "privatekey", "ssh_key":
			s.HasKey = s.HasKey || f.Value != ""
		}
	}
	return s
}

func (v *Vault) loadItems(ctx context.Context, path, session string) (map[string]item, error) {
	out, err := v.run(ctx, path, session, nil, "list", "items")
	if err != nil {
		return nil, fmt.Errorf("3007 list vault items: %w", err)
	}
	var list []item
	if err := json.Unmarshal(lastJSON(out), &list); err != nil {
		return nil, errors.New("3008 unexpected bw list output")
	}
	m := make(map[string]item, len(list))
	for _, it := range list {
		m[it.ID] = it
	}
	return m, nil
}

// touchLocked 记录最近一次使用, 自动锁定按空闲时长计。
func (v *Vault) touchLocked() {
	v.lastUsed = time.Now()
	v.armTimerLocked()
}

func (v *Vault) armTimerLocked() {
	if v.lockTimer != nil {
		v.lockTimer.Stop()
		v.lockTimer = nil
	}
	v.lockGen++
	if v.session == "" || v.autoLock <= 0 {
		return
	}
	gen := v.lockGen
	v.lockTimer = time.AfterFunc(v.autoLock, func() { v.lockIfCurrent(gen) })
}

func (v *Vault) bwPathLocked() string {
	p, _ := v.resolveLocked()
	return p
}

// resolveLocked 返回 bw 可执行文件路径。
func (v *Vault) resolveLocked() (string, error) {
	if v.bwPath != "" {
		if _, err := os.Stat(v.bwPath); err != nil {
			return "", fmt.Errorf("3009 bw not found at %s", v.bwPath)
		}
		return v.bwPath, nil
	}
	p, err := exec.LookPath("bw")
	if err != nil {
		return "", errors.New("3010 bw CLI not found in PATH, install it or set its path in settings")
	}
	return p, nil
}

// run 执行 bw 子命令。会话密钥与额外变量只经环境变量传入。
func (v *Vault) run(ctx context.Context, path, session string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := platform.Command(ctx, path, append(args, "--nointeraction")...)
	cmd.Env = append(os.Environ(), "BW_NOINTERACTION=true", "NODE_NO_WARNINGS=1")
	if session != "" {
		cmd.Env = append(cmd.Env, "BW_SESSION="+session)
	}
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		// bw 的错误输出是一行英文说明 (如 Invalid master password.), 不含凭据, 可以带给界面。
		return nil, errors.New(firstLine(msg))
	}
	return stdout.Bytes(), nil
}

// lastJSON 取输出里最后一段 JSON: 新版 bw 偶尔会在前面打印更新提示。
func lastJSON(out []byte) []byte {
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		l := bytes.TrimSpace(lines[i])
		if len(l) > 0 && (l[0] == '{' || l[0] == '[') {
			return l
		}
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
