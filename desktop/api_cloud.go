package main

// 空间与云同步绑定。空间: 本机 (永远在)、个人云同步、团队; 列表、编辑、脚本都看当前空间,
// 终端、MCP、跳板链按连接 id 在全部可见空间里找。云同步强制端到端加密, 密钥操作 (开通、恢复码、
// 新设备批准) 都在这里转给 internal/cloudsync。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/billing"
	"github.com/woodchen-ink/czlterm/desktop/internal/cloudsync"
	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
	"github.com/woodchen-ink/czlterm/desktop/internal/settings"
	"github.com/woodchen-ink/czlterm/desktop/internal/space"
)

// eventSpaceChanged 当前空间换了 (用户切换、离开团队后退回本机), 载荷空间标识。
const eventSpaceChanged = "space:changed"

// currentSpace 返回当前空间; 记下的空间已不可见 (退出登录、离开团队) 时回到本机。
func (a *App) currentSpace() *space.Space {
	if key := a.settings.Get().Space; key != "" && a.spaces.Visible(key) {
		if sp := a.spaces.Get(key); sp != nil {
			return sp
		}
	}
	return a.spaces.Local()
}

// allConnections 返回全部可见空间的连接 (MCP 列表用)。
func (a *App) allConnections() []conn.Connection {
	var out []conn.Connection
	for _, sp := range a.spaces.All() {
		list, _ := sp.Conns.List()
		out = append(out, list...)
	}
	return out
}

// findConn 按 id 找连接与它所在的空间, 当前空间优先 (不同空间可能有同 id 的连接)。
func (a *App) findConn(id string) (conn.Connection, *space.Space, error) {
	return a.spaces.FindConnection(a.currentSpace().Key, id)
}

// findConnectionByName 按 id 或名称 (不区分大小写) 在全部可见空间里找连接, 供 MCP 按人类可读的名字定位。
func (a *App) findConnectionByName(name string) (conn.Connection, *space.Space, error) {
	if c, sp, err := a.findConn(name); err == nil {
		return c, sp, nil
	}
	type hit struct {
		c  conn.Connection
		sp *space.Space
	}
	var hits []hit
	for _, sp := range a.spaces.All() {
		list, _ := sp.Conns.List()
		for _, c := range list {
			if strings.EqualFold(c.Name, name) {
				hits = append(hits, hit{c, sp})
			}
		}
	}
	switch len(hits) {
	case 1:
		return hits[0].c, hits[0].sp, nil
	case 0:
		return conn.Connection{}, nil, fmt.Errorf("2012 connection %q not found", name)
	default:
		return conn.Connection{}, nil, fmt.Errorf("2013 connection name %q is ambiguous, use the id", name)
	}
}

// SpaceView 是空间切换器里的一项。
type SpaceView struct {
	Key string `json:"key"`
	// Kind local / personal / org
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	OrgID string `json:"orgId"`
	Role  string `json:"role"`
	// ReadOnly 不能写; Need 说明只读是因为订阅 (subscribe / seats), 为空表示另有原因或可写
	ReadOnly bool   `json:"readOnly"`
	Need     string `json:"need"`
	Removed  bool   `json:"removed"`
	Pending  int    `json:"pending"`
	// Status idle / ok / error / waiting_key; 本机空间为空
	Status   string `json:"status"`
	Error    string `json:"error"`
	SyncedAt string `json:"syncedAt"`
	// Connections / Scripts 是本机副本里的条数
	Connections int `json:"connections"`
	Scripts     int `json:"scripts"`
}

// count 填上空间里的连接与脚本条数。
func (a *App) count(v *SpaceView) {
	if sp := a.spaces.Get(v.Key); sp != nil {
		c, _ := sp.Conns.List()
		s, _ := sp.Scripts.List()
		v.Connections, v.Scripts = len(c), len(s)
	}
}

// SpacesView 是空间列表与当前空间。
type SpacesView struct {
	Current string      `json:"current"`
	Spaces  []SpaceView `json:"spaces"`
}

// ListSpaces 返回本机可切换的空间, 本机在前、个人同步其次、团队按名称。
func (a *App) ListSpaces() (SpacesView, error) {
	if err := a.ready(); err != nil {
		return SpacesView{}, err
	}
	out := SpacesView{Current: a.currentSpace().Key, Spaces: []SpaceView{{Key: space.Local, Kind: "local"}}}
	bill := a.BillingState()
	for _, sp := range a.cloud.State().Spaces {
		if !a.spaces.Visible(sp.Key) {
			continue
		}
		out.Spaces = append(out.Spaces, SpaceView{
			Key: sp.Key, Kind: sp.Kind, Name: sp.Name, OrgID: sp.OrgID, Role: sp.Role, ReadOnly: sp.ReadOnly,
			Need:    billing.SpaceNeed(sp.ReadOnly && !sp.Removed && sp.Role != "viewer", sp.OrgID, bill.Entitlements, bill.Enabled),
			Removed: sp.Removed, Pending: sp.Pending, Status: sp.Status, Error: sp.Error, SyncedAt: sp.SyncedAt,
		})
	}
	for i := range out.Spaces {
		a.count(&out.Spaces[i])
	}
	return out, nil
}

// SwitchSpace 切换当前空间并记住 (本机设置, 不同步)。
func (a *App) SwitchSpace(key string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	if !a.spaces.Visible(key) {
		return "", errcode.ErrSpaceUnknown
	}
	if _, err := a.settings.Update(func(s *settings.Settings) { s.Space = key }); err != nil {
		return "", err
	}
	wruntime.EventsEmit(a.ctx, eventSpaceChanged, key)
	return key, nil
}

// CloudState 返回云同步状态 (不联网)。
func (a *App) CloudState() cloudsync.State {
	if a.ready() != nil {
		return cloudsync.State{}
	}
	return a.cloud.State()
}

// CloudRefresh 联网刷新同步状态与空间列表。
func (a *App) CloudRefresh() cloudsync.State {
	if a.ready() != nil {
		return cloudsync.State{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.cloud.Refresh(ctx)
}

// CloudSyncNow 立即同步全部空间。
func (a *App) CloudSyncNow() (cloudsync.State, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a.cloud.Refresh(ctx)
	err := a.cloud.SyncAll(ctx)
	return a.cloud.State(), err
}

// CloudEnroll 首次开通云同步: 生成本机密钥与恢复码。恢复码只返回这一次, 界面必须让用户保存并确认。
func (a *App) CloudEnroll() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.cloud.Enroll(ctx)
}

// CloudUnlock 在新电脑上用恢复码解锁。
func (a *App) CloudUnlock(recoveryCode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.cloud.Unlock(ctx, strings.TrimSpace(recoveryCode))
}

// CloudResetRecoveryCode 生成新恢复码, 旧的作废。
func (a *App) CloudResetRecoveryCode() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.cloud.ResetRecoveryCode(ctx)
}

// CloudStartDeviceEnrollment 本机作为新设备请求已登录的设备批准, 进度经 cloud:device 事件推送。
func (a *App) CloudStartDeviceEnrollment() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.cloud.StartDeviceEnrollment(ctx)
}

// CloudCancelDeviceEnrollment 撤回进行中的新设备授权。
func (a *App) CloudCancelDeviceEnrollment() {
	a.cloud.CancelDeviceEnrollment()
}

// CloudPendingDevices 列出等待本账号批准的新设备。
func (a *App) CloudPendingDevices() ([]cloudsync.DeviceRequest, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.cloud.PendingDevices(ctx)
}

// CloudBeginDeviceApproval 开始批准一台新设备, 返回两边都应显示的 6 位校验码。
func (a *App) CloudBeginDeviceApproval(requestID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return a.cloud.BeginDeviceApproval(ctx, requestID)
}

// CloudFinishDeviceApproval 用户核对校验码后批准或拒绝。
func (a *App) CloudFinishDeviceApproval(requestID string, approve bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.cloud.FinishDeviceApproval(ctx, requestID, approve)
}

// personalSpace 返回本账号的个人同步空间。
func (a *App) personalSpace() (*space.Space, error) {
	for _, sp := range a.cloud.State().Spaces {
		if sp.Kind == "personal" && a.spaces.Visible(sp.Key) {
			if s := a.spaces.Get(sp.Key); s != nil {
				return s, nil
			}
		}
	}
	return nil, errcode.ErrSpaceUnknown
}

// CloudMoveLocalToPersonal 把本机空间的全部连接与脚本搬进个人同步空间 (id 不变), 并切到个人空间。
func (a *App) CloudMoveLocalToPersonal() (space.MoveResult, error) {
	if err := a.ready(); err != nil {
		return space.MoveResult{}, err
	}
	to, err := a.personalSpace()
	if err != nil {
		return space.MoveResult{}, err
	}
	res, err := space.MoveAll(a.spaces.Local(), to)
	if err != nil {
		return res, err
	}
	if _, err := a.SwitchSpace(to.Key); err != nil {
		a.log.Warn("switch space after move", "err", err)
	}
	return res, nil
}

// CopyConnectionTo 把连接复制到另一个空间 (换新 id, 跳板链一并复制), 返回新连接。
func (a *App) CopyConnectionTo(id, target string) (conn.Connection, error) {
	if err := a.ready(); err != nil {
		return conn.Connection{}, err
	}
	_, from, err := a.findConn(id)
	if err != nil {
		return conn.Connection{}, err
	}
	to := a.spaces.Get(target)
	if to == nil || !a.spaces.Visible(target) {
		return conn.Connection{}, errcode.ErrSpaceUnknown
	}
	return space.CopyConnection(from, to, id)
}

// CopyGroupTo 把当前空间里一个文件夹 (分组) 的全部连接复制到另一个空间, 文件夹名与彼此的跳板关系保持,
// 引用的凭据与组外跳板机一并带过去。group 为空表示未分组的连接。返回复制的条数。
func (a *App) CopyGroupTo(group, target string) (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	to := a.spaces.Get(target)
	if to == nil || !a.spaces.Visible(target) {
		return 0, errcode.ErrSpaceUnknown
	}
	out, err := space.CopyGroup(a.currentSpace(), to, group)
	return len(out), err
}

// CopyScriptTo 把脚本复制到另一个空间。
func (a *App) CopyScriptTo(id, target string) error {
	if err := a.ready(); err != nil {
		return err
	}
	_, from, err := a.spaces.FindScript(a.currentSpace().Key, id)
	if err != nil {
		return err
	}
	to := a.spaces.Get(target)
	if to == nil || !a.spaces.Visible(target) {
		return errcode.ErrSpaceUnknown
	}
	return space.CopyScript(from, to, id)
}

// CloudForgetSpace 清掉本机一个已被移出的团队副本。还有未上传修改时 force 为 false 返回 6406 由界面确认。
func (a *App) CloudForgetSpace(key string, force bool) error {
	sp := a.spaces.Get(key)
	if sp == nil {
		return errcode.ErrSpaceUnknown
	}
	if !force && sp.PendingCount() > 0 {
		return errcode.ErrSpaceHasPending
	}
	if a.currentSpace().Key == key {
		if _, err := a.SwitchSpace(space.Local); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.cloud.ForgetSpace(ctx, key)
}

// newCloud 组装云同步服务, 在 init 里调用。
func (a *App) newCloud(cfgDir, cacheDir string) {
	a.cloud = cloudsync.New(cloudsync.Options{
		Spaces: a.spaces, Tokens: a.account,
		KeyStore:     cloudsync.NewKeyStore(filepath.Join(cfgDir, "keys")),
		StateDir:     filepath.Join(cfgDir, "cloud"),
		BlobCacheDir: filepath.Join(cacheDir, "cloud-blobs"),
		DeviceName:   deviceName(),
		Log:          a.log,
		Subscribe:    true,
		Emit: func(event string, data any) {
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, event, data)
			}
		},
		OnApplied: func(_, collection, id string) {
			if collection == space.CollConnections {
				a.pool.Drop(id)
			}
		},
	})
}

// deviceName 是新设备授权时给已登录设备看的名字, 如「Windows · DESKTOP-1」。
func deviceName() string {
	host, _ := os.Hostname()
	osName := map[string]string{"windows": "Windows", "darwin": "macOS", "linux": "Linux"}[runtime.GOOS]
	if osName == "" {
		osName = runtime.GOOS
	}
	if host == "" {
		return osName
	}
	return osName + " · " + host
}

// SaveRecoveryCode 弹出保存对话框, 把恢复码存成文本文件 (用户自己保管, 新电脑解锁时可拖入或选择它)。
// 用户取消返回 false。
func (a *App) SaveRecoveryCode(code string) (bool, error) {
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		DefaultFilename: "czlterm-recovery-code.txt",
		Filters:         []wruntime.FileFilter{{DisplayName: "Text", Pattern: "*.txt"}},
	})
	if err != nil || path == "" {
		return false, err
	}
	body := "czlterm recovery code / 恢复码\n\n" + strings.TrimSpace(code) + "\n\n" +
		"Keep this file safe. It unlocks your end-to-end encrypted cloud sync on a new computer.\n" +
		"妥善保管。在新电脑上解锁端到端加密的云同步需要它; 丢了它且没有可用的已登录设备时, 云端数据无法恢复。\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return false, fmt.Errorf("6415 write recovery code file: %w", err)
	}
	return true, nil
}
