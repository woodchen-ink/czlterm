package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/deeplink"
	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
	"github.com/woodchen-ink/czlterm/desktop/internal/secret"
)

// OfflineGrace 是联系不上登录服务时, 凭本地登录记录继续放行的最长时间
// (从上次与服务端确认有效算起)。断网时仍能看到同步空间与团队;
// 但也不能永远不验证, 否则注销的账号可以一直用下去。
const OfflineGrace = 30 * 24 * time.Hour

// pendingTTL 是一次登录从打开浏览器到回调的最长等待时间。
const pendingTTL = 10 * time.Minute

// refreshSkew 提前多久视为 access token 即将过期。
const refreshSkew = 5 * time.Minute

// session 是落盘的登录记录。整体加密后写入 config/account.dat (internal/secret: 系统密钥库里的随机密钥做 AES-GCM)。
type session struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	// VerifiedAt 上次与服务端确认登录有效的时间, 离线宽限从这里算
	VerifiedAt time.Time `json:"verifiedAt"`
	User       User      `json:"user"`
}

// Status 是给界面的登录状态。
type Status struct {
	LoggedIn bool `json:"loggedIn"`
	// Offline 本次没联系上登录服务, 凭本地登录记录放行
	Offline bool  `json:"offline"`
	User    *User `json:"user"`
}

type pendingLogin struct {
	state    string
	verifier string
	started  time.Time
}

// Manager 管理登录流程与本地登录记录。并发安全。
type Manager struct {
	c    *client
	path string
	now  func() time.Time

	mu      sync.Mutex
	pending *pendingLogin
	// stale 是收到 401 的 access token, 下次 AccessToken 遇到它就强制续期
	stale string

	// refreshMu 串行化续期: refresh token 用一次可能就换新, 并发续期会让其中一方拿到作废的 token
	refreshMu sync.Mutex

	// onExpired 在后台续期被服务端拒绝、登录记录已清掉时调用 (界面退回登录页), 见 OnExpired
	onExpired func()
}

// OnExpired 设置登录失效时的回调: 同步、组织、收款接口在后台取令牌时续期被拒, 清掉登录记录后调用。
// 启动时的 Status 走自己的返回值, 不触发它。
func (m *Manager) OnExpired(fn func()) {
	m.mu.Lock()
	m.onExpired = fn
	m.mu.Unlock()
}

// NewManager 创建管理器, path 为登录记录文件路径。
func NewManager(cfg Config, path string) *Manager {
	return &Manager{c: newClient(cfg), path: path, now: time.Now}
}

// Begin 发起一次登录, 返回要在系统浏览器里打开的授权地址。
// 再次调用会作废上一次未完成的登录 (用户关掉了浏览器页再点一次)。
// Configured 报告是否填了客户端 id; 没填时 Begin 返回 ErrAuthNotConfigured。
func (m *Manager) Configured() bool { return m.c.cfg.ClientID != "" }

func (m *Manager) Begin() (string, error) {
	if !m.Configured() {
		return "", errcode.ErrAuthNotConfigured
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := &pendingLogin{state: randomToken(16), verifier: newVerifier(), started: m.now()}
	m.pending = p
	return m.c.authorizeURL(p.state, challengeS256(p.verifier)), nil
}

// Cancel 放弃进行中的登录。
func (m *Manager) Cancel() {
	m.mu.Lock()
	m.pending = nil
	m.mu.Unlock()
}

// IsCallback 判断参数是否为登录回调 czlterm://oauth/callback。同一协议还有加入团队等链接
// (见 internal/deeplink), 只认回调这一条, 别的链接不进登录流程。
func IsCallback(arg string) bool {
	l, ok := deeplink.Parse(arg)
	return ok && l.Kind == deeplink.KindOAuth
}

// Complete 处理回调 URL: 校验 state, 用授权码换 token, 取账号信息并落盘。
//
// state 对上了才消耗这次登录: 任意网页都能拉起 czlterm://oauth/callback, 伪造的回调不能把进行中的登录作废。
func (m *Manager) Complete(ctx context.Context, rawURL string) (*User, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errcode.ErrAuthDenied.Wrap(err)
	}
	q := u.Query()
	m.mu.Lock()
	p := m.pending
	switch {
	case p == nil || m.now().Sub(p.started) > pendingTTL:
		m.pending = nil
		m.mu.Unlock()
		return nil, errcode.ErrAuthNotPending
	case q.Get("state") != p.state:
		m.mu.Unlock()
		return nil, errcode.ErrAuthState
	}
	m.pending = nil
	m.mu.Unlock()
	if e := q.Get("error"); e != "" {
		return nil, errcode.ErrAuthDenied.Wrap(errors.New(e + ": " + q.Get("error_description")))
	}
	code := q.Get("code")
	if code == "" {
		return nil, errcode.ErrAuthDenied.Wrap(errors.New("callback without code"))
	}

	tr, err := m.c.exchange(ctx, code, p.verifier)
	if err != nil {
		if errors.Is(err, errRejected) {
			return nil, errcode.ErrAuthDenied.Wrap(err)
		}
		return nil, err
	}
	s := m.sessionFrom(tr, nil)
	// 没有账号标识就没法区分同步空间, 取不到用户信息算登录失败, 不留半截登录记录
	user, err := m.c.userInfo(ctx, tr.AccessToken)
	if err != nil {
		return nil, err
	}
	if user.ID == "" {
		return nil, errcode.ErrAuthResponse
	}
	s.User = *user
	if err := m.save(s); err != nil {
		return nil, err
	}
	return &s.User, nil
}

// Status 读取本地登录记录并按需续期。
//
// access token 还有效就不联网, 启动不被网络拖慢; 快过期时用 refresh token 续期:
// 服务端明确拒绝就清掉记录要求重新登录, 联系不上则在宽限期内放行。
func (m *Manager) Status(ctx context.Context) (Status, error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	s, err := m.load()
	if err != nil || s == nil {
		// 记录损坏 (比如换了 Windows 账号解不开) 等同未登录
		return Status{}, nil
	}
	now := m.now()
	if now.Before(s.ExpiresAt.Add(-refreshSkew)) {
		return Status{LoggedIn: true, User: &s.User}, nil
	}

	tr, err := m.c.refresh(ctx, s.RefreshToken)
	switch {
	case err == nil:
		next := m.sessionFrom(tr, s)
		if user, err := m.c.userInfo(ctx, tr.AccessToken); err == nil {
			next.User = *user
		}
		if err := m.save(next); err != nil {
			return Status{}, err
		}
		return Status{LoggedIn: true, User: &next.User}, nil
	case errors.Is(err, errRejected):
		_ = m.clear()
		return Status{}, nil
	case now.Sub(s.VerifiedAt) < OfflineGrace:
		return Status{LoggedIn: true, Offline: true, User: &s.User}, nil
	default:
		return Status{}, nil
	}
}

// AccessToken 返回可用的 access token, 快过期或被判无效时先续期。供同步服务与组织接口调用,
// 业务数据不经登录服务。没登录、续期被拒返回 ErrAuthExpired; 联系不上登录服务时返回网络错误。
func (m *Manager) AccessToken(ctx context.Context) (string, error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	s, err := m.load()
	if err != nil || s == nil {
		return "", errcode.ErrAuthExpired
	}
	m.mu.Lock()
	stale := m.stale != "" && m.stale == s.AccessToken
	m.mu.Unlock()
	if !stale && m.now().Before(s.ExpiresAt.Add(-refreshSkew)) {
		return s.AccessToken, nil
	}
	tr, err := m.c.refresh(ctx, s.RefreshToken)
	if errors.Is(err, errRejected) {
		// 与 Status 一致: 服务端明确拒绝就清掉记录, 并通知界面退回登录页
		_ = m.clear()
		m.mu.Lock()
		fn := m.onExpired
		m.mu.Unlock()
		if fn != nil {
			go fn()
		}
		return "", errcode.ErrAuthExpired
	}
	if err != nil {
		return "", err
	}
	next := m.sessionFrom(tr, s)
	if err := m.save(next); err != nil {
		return "", err
	}
	return next.AccessToken, nil
}

// InvalidateToken 在服务端回 401 时调用 (同步 SDK 的 TokenInvalidator), 下次取 token 强制续期。
func (m *Manager) InvalidateToken(token string) {
	m.mu.Lock()
	m.stale = token
	m.mu.Unlock()
}

// Logout 删除本地登录记录。
func (m *Manager) Logout() error {
	m.Cancel()
	return m.clear()
}

// sessionFrom 由 token 响应生成新记录。refresh 响应可能不回新的 refresh token,
// 此时沿用旧的; 账号信息也先沿用, 取到新的再覆盖。
func (m *Manager) sessionFrom(tr *tokenResponse, prev *session) *session {
	now := m.now()
	ttl := time.Duration(tr.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	s := &session{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    now.Add(ttl),
		VerifiedAt:   now,
	}
	if prev != nil {
		if s.RefreshToken == "" {
			s.RefreshToken = prev.RefreshToken
		}
		s.User = prev.User
	}
	return s
}

// sealVault / openVault 是登录记录加解密的入口, 指向 internal/secret。
// 测试换成内存实现: macOS 的钥匙串在 SSH 与 CI 会话里是锁着的。
var (
	sealVault = secret.Protect
	openVault = secret.Unprotect
)

func (m *Manager) load() (*session, error) {
	sealed, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errcode.ErrAuthVault.Wrap(err)
	}
	plain, err := openVault(sealed)
	if err != nil {
		return nil, errcode.ErrAuthVault.Wrap(err)
	}
	var s session
	if err := json.Unmarshal(plain, &s); err != nil {
		return nil, errcode.ErrAuthVault.Wrap(err)
	}
	return &s, nil
}

func (m *Manager) save(s *session) error {
	plain, err := json.Marshal(s)
	if err != nil {
		return errcode.ErrAuthVault.Wrap(err)
	}
	sealed, err := sealVault(plain)
	if err != nil {
		return errcode.ErrAuthVault.Wrap(err)
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return errcode.ErrAuthVault.Wrap(err)
	}
	tmp := m.path + ".part"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return errcode.ErrAuthVault.Wrap(err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return errcode.ErrAuthVault.Wrap(err)
	}
	return nil
}

func (m *Manager) clear() error {
	if err := os.Remove(m.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errcode.ErrAuthVault.Wrap(err)
	}
	return nil
}
