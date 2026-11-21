// Package cloudsync 把 czlterm 接到 CZL Connect 的数据同步: 个人多设备同步与团队空间, 强制端到端加密。
//
// 同步协议、出站推送、增量拉取、冲突回调、加解密与密钥生命周期 (开通、恢复码、新设备批准、
// 成员授予与轮换) 都由 SDK github.com/woodchen-ink/go-web-utils/syncsdk 完成; 本包只做:
//
//   - 出站队列即本地空间: SDK 的 Outbox / CursorStore / Applier 落在 internal/space 的 sync.json 与文件上 (local.go)
//   - 线上格式: {"v":1,"data":<连接或脚本>}, 拉到的内容入库前按本机规则重新校验 (codec.go)
//   - 冲突: 服务端版本落地, 本地版本另存为「(冲突副本)」
//   - 空间: 本地标识 me:<用户> / org:<组织> 与服务端空间 id 的对应、只读状态
//
// 服务端只见密文信封与集合名、文档 id (随机串), 看不到主机、用户名与脚本内容。
package cloudsync

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/woodchen-ink/go-web-utils/syncsdk"
	"github.com/woodchen-ink/go-web-utils/syncsdk/e2e"

	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
	"github.com/woodchen-ink/czlterm/desktop/internal/space"
)

// DefaultBaseURL 是同步服务地址 (Connect 主域名下)。
const DefaultBaseURL = "https://connect.czl.net/api/sync/v1"

// 推给界面的事件。
const (
	// EventState 同步状态变了 (开通、解锁、空间增减、某空间同步完成或出错), 载荷 State
	EventState = "cloud:state"
	// EventChanged 某空间拉到了别处的修改, 列表应刷新, 载荷 {space}
	EventChanged = "cloud:changed"
	// EventConflict 推送遇到冲突、本地版本已另存副本, 载荷 ConflictInfo
	EventConflict = "cloud:conflict"
	// EventDevice 本机作为新设备等待批准的进度, 载荷 DeviceProgress
	EventDevice = "cloud:device"
)

// TokenSource 提供 CZL Connect 用户令牌, 由 internal/account 实现。
type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
	InvalidateToken(token string)
}

// Options 配置 Service。
type Options struct {
	// BaseURL 为空用 DefaultBaseURL
	BaseURL string
	Spaces  *space.Registry
	Tokens  TokenSource
	// KeyStore 存端到端加密的本机私钥与空间密钥
	KeyStore syncsdk.KeyStore
	// StateDir 存本机记得的空间列表 (离线启动时空间切换照常), 在 config 下
	StateDir string
	// BlobCacheDir 是附件下载缓存; czlterm 目前不传附件, SDK 仍要求目录
	BlobCacheDir string
	// DeviceName 是新设备授权时给旧设备看的名字, 如「Windows · DESKTOP-1」
	DeviceName string
	// Emit 把事件推给界面, 可为 nil
	Emit func(event string, data any)
	// OnApplied 在拉到的一份文档落地后调用 (丢掉指向旧配置的进程内连接), 可为 nil
	OnApplied func(space, collection, id string)
	Log       *slog.Logger
	// HTTPClient / Retry 测试注入
	HTTPClient *http.Client
	Retry      syncsdk.RetryPolicy
	// Subscribe 为 false 时不开后台订阅, 只在 SyncNow 时同步 (测试用)
	Subscribe        bool
	SubscribeOptions syncsdk.SubscribeOptions
}

// Service 管一个登录账号的同步。并发安全。登录后 Start, 退出登录 Stop。
type Service struct {
	opts Options

	// startMu 串行化 Start / Stop: 启动时 App 与界面可能同时确认登录, 不能各组装一套引擎互相覆盖。
	startMu sync.Mutex
	// adhoc 跟踪订阅之外的同步 (立即同步、本地编辑后的整轮同步), Stop 时一并等它们收手。
	adhoc sync.WaitGroup

	mu        sync.Mutex
	user      string
	client    *syncsdk.Client
	keys      *syncsdk.KeyManager
	engine    *syncsdk.Engine
	root      context.Context
	cancel    context.CancelFunc
	subs      map[string]*subscription // 服务端空间 id → 后台订阅
	state     State
	spaces    map[string]*SpaceInfo // 本地空间标识 → 状态
	holds     map[string]int64      // 本地空间 → 本轮拉取要停在的游标 (见 adapter.Set)
	enrolling *syncsdk.DeviceEnrollment
	approvals map[string]*syncsdk.DeviceApproval
}

// State 是给界面的同步总状态。
type State struct {
	SignedIn bool `json:"signedIn"`
	// Available 同步服务连得上
	Available bool `json:"available"`
	// KeyStatus 本机端到端加密密钥: unknown / not_enrolled / locked / unlocked
	KeyStatus string `json:"keyStatus"`
	// Fingerprint 本机公钥指纹, 解锁后才有, 供与同事核对
	Fingerprint string `json:"fingerprint"`
	// Error 最近一次总体失败的错误串, 没有为空
	Error  string      `json:"error"`
	Spaces []SpaceInfo `json:"spaces"`
}

// SpaceInfo 是一个同步空间在本机的状态。
type SpaceInfo struct {
	// Key 本地空间标识 (me:<用户> / org:<组织>)
	Key string `json:"key"`
	// Kind personal / org
	Kind string `json:"kind"`
	// OrgID 组织空间的组织公开标识
	OrgID string `json:"orgId,omitempty"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	// ReadOnly 查看者、订阅到期、组织超席位, 或已不在这个组织 (Removed)
	ReadOnly bool `json:"readOnly"`
	// Removed 服务端已不再列出这个空间 (被移出、团队解散): 本机副本保留只读
	Removed bool `json:"removed"`
	// Status idle / ok / error / waiting_key
	Status   string `json:"status"`
	Error    string `json:"error"`
	Pending  int    `json:"pending"`
	SyncedAt string `json:"syncedAt"`
	// Usage / Quota 服务端统计的字节数, 0 为未知或不限
	UsageBytes int64 `json:"usageBytes"`
	QuotaBytes int64 `json:"quotaBytes"`
}

// New 创建 Service, 不联网。
func New(opts Options) *Service {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	s := &Service{opts: opts}
	s.reset()
	opts.Spaces.OnLocalEdit(s.onLocalEdit)
	return s
}

func (s *Service) reset() {
	s.subs = map[string]*subscription{}
	s.spaces = map[string]*SpaceInfo{}
	s.holds = map[string]int64{}
	s.approvals = map[string]*syncsdk.DeviceApproval{}
	s.state = State{}
}

// User 返回当前登录账号的用户 id, 未启动为空。
func (s *Service) User() string { return s.userID() }

func (s *Service) userID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.user
}

func (s *Service) engineRef() *syncsdk.Engine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.engine
}

func (s *Service) emit(event string, data any) {
	if s.opts.Emit != nil {
		s.opts.Emit(event, data)
	}
}

// docFailed 记下一条文档没能同步 (内容认不出、解不开), 只进日志: 其余照常, 界面不为单条打扰用户。
func (s *Service) docFailed(space, collection, id string, err error) {
	s.opts.Log.Warn("cloudsync doc", "space", space, "doc", collection+"/"+id, "err", err)
}

// ─── 本地空间标识 ──────────────────────────────────────────────────────────

// LocalKey 由服务端空间 id 生成本地空间标识: 个人 me:<用户>, 团队 org:<组织>@<用户>。
// 团队也带上账号: 同一台电脑上两个账号在同一个团队时, 各自的本机副本与未上传修改互不相干。
func LocalKey(userID, remote string) string {
	if remote == syncsdk.PersonalSpace {
		return space.PersonalPrefix + userID
	}
	return space.OrgPrefix + remote + "@" + userID
}

// OrgKey 是本账号某个团队的本地空间标识。
func OrgKey(userID, orgID string) string { return LocalKey(userID, orgID) }

// remoteOf 由本地空间标识取服务端空间 id。
func remoteOf(local string) (string, bool) {
	if rest, ok := strings.CutPrefix(local, space.PersonalPrefix); ok && rest != "" {
		return syncsdk.PersonalSpace, true
	}
	if rest, ok := strings.CutPrefix(local, space.OrgPrefix); ok {
		org, _, _ := strings.Cut(rest, "@")
		if org != "" {
			return org, true
		}
	}
	return "", false
}

// ─── 生命周期 ──────────────────────────────────────────────────────────────

// Start 为登录账号准备同步: 组装 SDK、载入本机记得的空间, 后台联网刷新。同一账号重复调用无副作用。
func (s *Service) Start(userID string) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	s.mu.Lock()
	if s.user == userID && s.engine != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	s.stop(false)

	client, err := syncsdk.NewClient(syncsdk.ClientOptions{
		BaseURL: s.opts.BaseURL, Tokens: tokenAdapter{s.opts.Tokens}, HTTPClient: s.opts.HTTPClient,
		Retry: s.opts.Retry, UserAgent: "czlterm",
	})
	if err != nil {
		return errcode.ErrSyncUnavailable.Wrap(err)
	}
	keys, err := syncsdk.NewKeyManager(client, s.opts.KeyStore, syncsdk.KeyManagerOptions{Namespace: keyNamespace(userID)})
	if err != nil {
		return errcode.ErrSyncUnavailable.Wrap(err)
	}
	a := adapter{s: s, user: userID}
	engine, err := syncsdk.NewEngine(syncsdk.EngineOptions{
		Client: client, Keys: keys, Outbox: a, Cursors: a, Applier: a, Resolver: a.resolve,
		BlobCacheDir: s.opts.BlobCacheDir,
		OnRejected: func(_ context.Context, item syncsdk.OutboxItem, err error) bool {
			return s.onRejected(userID, item, err)
		},
	})
	if err != nil {
		return errcode.ErrSyncUnavailable.Wrap(err)
	}

	root, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.user, s.client, s.keys, s.engine, s.root, s.cancel = userID, client, keys, engine, root, cancel
	s.state = State{SignedIn: true, KeyStatus: "unknown"}
	for _, sp := range s.loadKnown(userID) {
		sp := sp
		sp.Status = "idle"
		s.spaces[sp.Key] = &sp
	}
	s.applyVisibleLocked()
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(root, 30*time.Second)
		defer cancel()
		s.Refresh(ctx)
	}()
	return nil
}

// Stop 停掉全部订阅。forget 为 true 时 (退出登录) 一并删掉本机保存的私钥与空间密钥,
// 下次登录要用恢复码或已登录设备批准重新解锁; 本地已同步的数据留在磁盘上, 换回这个账号还在。
func (s *Service) Stop(forget bool) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	s.stop(forget)
}

func (s *Service) stop(forget bool) {
	s.mu.Lock()
	cancel, keys, subs := s.cancel, s.keys, s.subs
	var remotes []string
	for key := range s.spaces {
		if r, ok := remoteOf(key); ok {
			remotes = append(remotes, r)
		}
	}
	s.user, s.client, s.keys, s.engine, s.cancel, s.enrolling = "", nil, nil, nil, nil, nil
	s.reset()
	s.opts.Spaces.SetVisible(nil)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// 等后台同步收手: 还在落地的一页写完再走, 不和下一个账号交叠
	for _, sub := range subs {
		sub.wait()
	}
	waitTimeout(&s.adhoc, 15*time.Second)
	if forget && keys != nil {
		if err := keys.Forget(context.Background(), remotes...); err != nil {
			s.opts.Log.Warn("cloudsync forget keys", "err", err)
		}
	}
	s.emit(EventState, s.State())
}

// keyNamespace 区分同一台电脑上多个账号的密钥。
func keyNamespace(userID string) string { return "czlterm/" + userID }

type tokenAdapter struct{ t TokenSource }

func (a tokenAdapter) Token(ctx context.Context) (string, error) { return a.t.AccessToken(ctx) }
func (a tokenAdapter) InvalidateToken(tok string)                { a.t.InvalidateToken(tok) }

// applyVisibleLocked 把当前账号认识的空间告诉 Registry (列表与按 id 查找只看这些), 并同步只读状态。
func (s *Service) applyVisibleLocked() {
	keys := make([]string, 0, len(s.spaces))
	for key, sp := range s.spaces {
		keys = append(keys, key)
		if local, err := s.opts.Spaces.Ensure(key); err == nil {
			local.SetReadOnly(sp.ReadOnly)
		} else {
			s.opts.Log.Warn("cloudsync ensure space", "space", key, "err", err)
		}
	}
	s.opts.Spaces.SetVisible(keys)
}

// ─── 状态与空间 ────────────────────────────────────────────────────────────

// State 返回当前状态 (不联网)。
func (s *Service) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	st.Spaces = make([]SpaceInfo, 0, len(s.spaces))
	for _, sp := range s.spaces {
		info := *sp
		if local := s.opts.Spaces.Get(sp.Key); local != nil {
			info.Pending = local.PendingCount()
		}
		st.Spaces = append(st.Spaces, info)
	}
	sort.Slice(st.Spaces, func(i, j int) bool {
		a, b := st.Spaces[i], st.Spaces[j]
		if a.Kind != b.Kind {
			return a.Kind == syncsdk.SpaceTypePersonal
		}
		return a.Name < b.Name
	})
	return st
}

// Space 返回本机认识的一个同步空间, 不认识返回 nil。
func (s *Service) Space(key string) *SpaceInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sp, ok := s.spaces[key]; ok {
		cp := *sp
		return &cp
	}
	return nil
}

// Refresh 联网刷新: 服务可用性、本机密钥状态、可访问的空间; 解锁后为每个空间开后台订阅。
// 连不上同步服务不算错误, 状态里 Available 为 false。
func (s *Service) Refresh(ctx context.Context) State {
	s.mu.Lock()
	client, keys, user := s.client, s.keys, s.user
	s.mu.Unlock()
	if client == nil {
		return s.State()
	}
	remote, err := client.Spaces(ctx)
	if err == nil {
		var status syncsdk.KeyStatus
		if status, err = keys.Status(ctx); err == nil {
			s.applyRemote(user, remote, status)
			return s.State()
		}
	}
	s.setState(func(st *State) { st.Available, st.Error = false, classify(err).Error() })
	return s.State()
}

func (s *Service) applyRemote(user string, remote []syncsdk.Space, status syncsdk.KeyStatus) {
	fp := ""
	if status == syncsdk.KeyStatusUnlocked {
		if pub, err := s.keys.PublicKey(context.Background()); err == nil {
			fp = pub.Fingerprint()
		}
	}
	s.mu.Lock()
	if s.user != user {
		s.mu.Unlock()
		return // 刷新途中换了账号
	}
	s.state = State{SignedIn: true, Available: true, KeyStatus: status.String(), Fingerprint: fp}
	seen := map[string]bool{}
	for _, r := range remote {
		key := LocalKey(user, r.ID)
		seen[key] = true
		sp := s.spaces[key]
		if sp == nil {
			sp = &SpaceInfo{Key: key, Status: "idle"}
			s.spaces[key] = sp
		}
		sp.Kind, sp.Name, sp.Role, sp.Removed = r.Type, r.Name, r.Role, false
		if r.Type == syncsdk.SpaceTypeOrg {
			sp.OrgID = r.ID
		}
		sp.ReadOnly = r.ReadOnly || r.Role == syncsdk.RoleViewer
		sp.UsageBytes, sp.QuotaBytes = r.Usage.Bytes, r.Quota.Bytes
	}
	for key, sp := range s.spaces {
		if !seen[key] {
			sp.Removed, sp.ReadOnly = true, true
			if r, ok := remoteOf(key); ok {
				s.unsubscribeLocked(r)
			}
		}
	}
	s.applyVisibleLocked()
	if status == syncsdk.KeyStatusUnlocked && s.opts.Subscribe {
		for _, r := range remote {
			s.subscribeLocked(r.ID)
		}
	}
	known := s.knownLocked()
	s.mu.Unlock()

	s.saveKnown(user, known)
	s.emit(EventState, s.State())
}

func (s *Service) setState(fn func(*State)) {
	s.mu.Lock()
	fn(&s.state)
	s.mu.Unlock()
	s.emit(EventState, s.State())
}

// subscription 是一个空间的后台订阅。done 在订阅协程退出 (进行中的那轮同步也结束) 后关闭。
type subscription struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// wait 取消订阅并等协程退出, 最多 15 秒 (网络卡住时不让退出登录跟着卡住)。
func (sub *subscription) wait() {
	sub.cancel()
	select {
	case <-sub.done:
	case <-time.After(15 * time.Second):
	}
}

func (s *Service) subscribeLocked(remote string) {
	if _, ok := s.subs[remote]; ok || s.engine == nil {
		return
	}
	ctx, cancel := context.WithCancel(s.root)
	sub := &subscription{cancel: cancel, done: make(chan struct{})}
	s.subs[remote] = sub
	engine, key := s.engine, LocalKey(s.user, remote)
	opts := s.opts.SubscribeOptions
	opts.OnSync = func(res syncsdk.SyncResult, err error) { s.onSync(key, res, err) }
	go func() {
		defer close(sub.done)
		_ = engine.Subscribe(ctx, remote, opts)
	}()
}

func (s *Service) unsubscribeLocked(remote string) *subscription {
	sub, ok := s.subs[remote]
	if !ok {
		return nil
	}
	sub.cancel()
	delete(s.subs, remote)
	return sub
}

// ─── 拉取失败时游标停住 ──────────────────────────────────────────────────

func (s *Service) holdBefore(local string, rev int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h, ok := s.holds[local]; !ok || rev-1 < h {
		s.holds[local] = rev - 1
	}
}

func (s *Service) hold(local string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.holds[local]
	return h, ok
}

func (s *Service) clearHold(local string) {
	s.mu.Lock()
	delete(s.holds, local)
	s.mu.Unlock()
}

// SyncNow 立即同步一个空间 (界面「立即同步」、测试)。随账号的生命周期: 退出登录时被取消, Stop 等它结束。
func (s *Service) SyncNow(ctx context.Context, key string) error {
	remote, ok := remoteOf(key)
	s.mu.Lock()
	eng, root := s.engine, s.root
	if eng != nil {
		s.adhoc.Add(1)
	}
	s.mu.Unlock()
	if !ok || eng == nil || s.Space(key) == nil {
		if eng != nil {
			s.adhoc.Done()
		}
		return errcode.ErrSpaceUnknown
	}
	defer s.adhoc.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(root, cancel)
	defer stop()
	res, err := eng.Sync(ctx, remote)
	s.onSync(key, res, err)
	return classify(err)
}

// SyncAll 立即同步全部还在的空间, 返回第一个错误。
func (s *Service) SyncAll(ctx context.Context) error {
	var first error
	for _, key := range s.ActiveSpaces() {
		if err := s.SyncNow(ctx, key); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *Service) onSync(key string, res syncsdk.SyncResult, err error) {
	s.mu.Lock()
	sp := s.spaces[key]
	if sp == nil {
		s.mu.Unlock()
		return
	}
	switch {
	case err == nil:
		sp.Status, sp.Error, sp.SyncedAt = "ok", "", time.Now().UTC().Format(time.RFC3339)
	case errors.Is(err, syncsdk.ErrKeyNotGranted):
		sp.Status, sp.Error = "waiting_key", ""
	default:
		sp.Status, sp.Error = "error", classify(err).Error()
		if ae := (*syncsdk.APIError)(nil); errors.As(err, &ae) && ae.Code == codeReadOnly {
			sp.ReadOnly = true
			if local := s.opts.Spaces.Get(key); local != nil {
				local.SetReadOnly(true)
			}
		}
	}
	s.mu.Unlock()
	if res.Pull.Reset {
		s.emit(EventChanged, map[string]any{"space": key})
	}
	s.emit(EventState, s.State())
}

// onRejected 处理服务端逐条永久拒绝的写入。角色不够写这个集合 (40311)、集合没登记 (40310) 的再推也没用:
// 放弃这条修改, 基线清零、游标归零, 下次拉取用服务端版本把本地改回来。其余 (超大小、超配额) 留着下次再试。
func (s *Service) onRejected(userID string, item syncsdk.OutboxItem, err error) bool {
	local := LocalKey(userID, item.Space)
	s.docFailed(local, item.Collection, item.ID, err)
	var ae *syncsdk.APIError
	if !errors.As(err, &ae) || (ae.Code != codeRoleTooLow && ae.Code != codeNotRegistered) {
		return false
	}
	sp := s.opts.Spaces.Get(local)
	if sp == nil {
		return false
	}
	if item.BaseRev == 0 {
		// 从没上传过的新文档: 服务端没有可拉回的版本, 放弃上传、留在本机, 不必全量重拉
		return true
	}
	if err := sp.ForgetRev(item.Collection, item.ID); err != nil {
		return false
	}
	_ = sp.SetCursor(0, wireVersion)
	return true
}

// 服务端业务码 (CZL Connect /api/sync/v1)。SDK 只按 HTTP 状态归类, 细分在这里。
const (
	codeNotMember     = 40301
	codeReadOnly      = 40302
	codeNotEnabled    = 40303
	codeUserDisabled  = 40304
	codeNotRegistered = 40310
	codeRoleTooLow    = 40311
)

// classify 把 SDK 错误换成本程序的错误码。
func classify(err error) error {
	var ec *errcode.Error
	var ae *syncsdk.APIError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ec):
		return err
	case errors.As(err, &ae) && ae.Code == codeNotEnabled:
		return errcode.ErrSyncNotEnabled.Wrap(err)
	case errors.As(err, &ae) && ae.Code == codeUserDisabled:
		return errcode.ErrSyncAccountDisabled.Wrap(err)
	case errors.As(err, &ae) && ae.Code == codeNotMember:
		return errcode.ErrSpaceUnknown.Wrap(err)
	case errors.Is(err, syncsdk.ErrLocked), errors.Is(err, syncsdk.ErrNotEnrolled):
		return errcode.ErrSyncLocked.Wrap(err)
	case errors.Is(err, syncsdk.ErrForbidden):
		return errcode.ErrSyncReadOnly.Wrap(err)
	case errors.Is(err, syncsdk.ErrRequestExpired), errors.Is(err, syncsdk.ErrRequestRejected):
		return errcode.ErrDeviceRequest.Wrap(err)
	case errors.Is(err, e2e.ErrInvalidRecoveryCode), errors.Is(err, e2e.ErrDecrypt):
		return errcode.ErrSyncRecoveryCode.Wrap(err)
	case syncsdk.IsTransient(err), errors.Is(err, syncsdk.ErrNotFound), errors.Is(err, syncsdk.ErrUnauthorized):
		return errcode.ErrSyncUnavailable.Wrap(err)
	default:
		return errcode.ErrSyncRequest.Wrap(err)
	}
}

// ─── 本机记得的空间 ────────────────────────────────────────────────────────

func (s *Service) knownPath(userID string) string {
	return filepath.Join(s.opts.StateDir, "spaces-"+keyDigest(userID)+".json")
}

func (s *Service) loadKnown(userID string) []SpaceInfo {
	var out []SpaceInfo
	raw, err := os.ReadFile(s.knownPath(userID))
	if err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func (s *Service) knownLocked() []SpaceInfo {
	out := make([]SpaceInfo, 0, len(s.spaces))
	for _, sp := range s.spaces {
		cp := *sp
		cp.Status, cp.Error, cp.Pending = "", "", 0
		out = append(out, cp)
	}
	return out
}

func (s *Service) saveKnown(userID string, spaces []SpaceInfo) {
	if userID == "" || s.opts.StateDir == "" {
		return
	}
	raw, _ := json.Marshal(spaces)
	_ = os.MkdirAll(s.opts.StateDir, 0o700)
	path := s.knownPath(userID)
	if err := os.WriteFile(path+".tmp", raw, 0o600); err == nil {
		_ = os.Rename(path+".tmp", path)
	}
}

// ForgetSpace 清掉本机一个空间: 停订阅、删本地数据与空间密钥、从记得的列表里去掉。
// 离开团队、团队解散后, 或清理已被移出的团队副本时调用; 调用方先确认用户接受丢掉未上传的修改。
func (s *Service) ForgetSpace(ctx context.Context, key string) error {
	remote, ok := remoteOf(key)
	if !ok {
		return errcode.ErrSpaceUnknown
	}
	s.mu.Lock()
	sub := s.unsubscribeLocked(remote)
	s.mu.Unlock()
	if sub != nil {
		sub.wait()
	}
	if err := s.opts.Spaces.Remove(key); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.spaces, key)
	user := s.user
	s.applyVisibleLocked()
	known := s.knownLocked()
	s.mu.Unlock()
	// 空间密钥名与 SDK 的约定一致: <命名空间>/space/<空间>
	if err := s.opts.KeyStore.Delete(ctx, keyNamespace(user)+"/space/"+remote); err != nil {
		s.opts.Log.Warn("cloudsync forget space key", "err", err)
	}
	s.saveKnown(user, known)
	s.emit(EventState, s.State())
	return nil
}

// ActiveSpaces 返回本账号还能推送的同步空间 (没被移出的)。
func (s *Service) ActiveSpaces() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for key, sp := range s.spaces {
		if !sp.Removed {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// UnpushedCount 返回还能推上去的未上传修改条数 (不含只读与已被移出的空间), 退出登录前确认用。
func (s *Service) UnpushedCount() int {
	n := 0
	for _, key := range s.ActiveSpaces() {
		sp := s.opts.Spaces.Get(key)
		if info := s.Space(key); sp != nil && info != nil && !info.ReadOnly {
			n += sp.PendingCount()
		}
	}
	return n
}

// ─── 本地写入 ──────────────────────────────────────────────────────────────

// onLocalEdit 由 Registry 在同步空间写入后调用: 唤醒该空间的推送。内容以本地文件为准,
// 传给 SDK 的值只是占位。id 为空 (批量搬移) 时直接同步一轮。
func (s *Service) onLocalEdit(key, collection, id string) {
	remote, ok := remoteOf(key)
	eng := s.engineRef()
	if !ok || eng == nil || LocalKey(s.userID(), remote) != key {
		return
	}
	if id == "" {
		go func() { _ = s.SyncNow(context.Background(), key) }()
		return
	}
	if err := eng.Save(context.Background(), remote, collection, id, 0, json.RawMessage("null")); err != nil {
		s.opts.Log.Warn("cloudsync poke", "space", key, "doc", collection+"/"+id, "err", err)
	}
}

// waitTimeout 等 wg 归零, 最多 d (网络卡住时不让退出登录跟着卡住)。
func waitTimeout(wg *sync.WaitGroup, d time.Duration) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
}
