package cloudsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/woodchen-ink/go-web-utils/syncsdk"
	"github.com/woodchen-ink/go-web-utils/syncsdk/e2e"
	"github.com/woodchen-ink/go-web-utils/syncsdk/synctest"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/cred"
	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
	"github.com/woodchen-ink/czlterm/desktop/internal/script"
	"github.com/woodchen-ink/czlterm/desktop/internal/secret"
	"github.com/woodchen-ink/czlterm/desktop/internal/space"
)

func TestMain(m *testing.M) {
	// 凭据文件用系统钥匙串里的密钥加密; 测试与 CI 里没有钥匙串, 换成固定的测试密钥
	secret.UseKeyForTest(make([]byte, 32))
	os.Exit(m.Run())
}

// 端到端测试: SDK 的假服务端 (强制端到端加密) + 多台真实的 czlterm 客户端 (各自的数据目录、密钥库与 Service)。

func newServer(t *testing.T) *synctest.Server {
	t.Helper()
	srv := synctest.New(synctest.Options{E2E: syncsdk.E2ERequired})
	t.Cleanup(srv.Close)
	srv.RegisterCollection(space.CollConnections, synctest.Collection{})
	srv.RegisterCollection(space.CollScripts, synctest.Collection{})
	srv.RegisterCollection(space.CollCredentials, synctest.Collection{})
	return srv
}

type fixedToken string

func (t fixedToken) AccessToken(context.Context) (string, error) { return string(t), nil }
func (fixedToken) InvalidateToken(string)                        {}

// device 是一台装了 czlterm 的电脑。
type device struct {
	t     *testing.T
	user  string
	reg   *space.Registry
	svc   *Service
	mu    sync.Mutex
	evts  []any
	codes chan string
	done  chan DeviceProgress
}

func newDevice(t *testing.T, srv *synctest.Server, user, token string) *device {
	t.Helper()
	dir := t.TempDir()
	reg, err := space.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	d := &device{t: t, user: user, reg: reg, codes: make(chan string, 1), done: make(chan DeviceProgress, 1)}
	d.svc = New(Options{
		BaseURL: srv.BaseURL(), Spaces: reg, Tokens: fixedToken(token), KeyStore: syncsdk.NewMemoryKeyStore(),
		StateDir: filepath.Join(dir, "config"), BlobCacheDir: filepath.Join(dir, "blobs"), DeviceName: "test",
		Retry: syncsdk.RetryPolicy{MaxAttempts: 1},
		Emit:  d.onEvent,
	})
	if err := d.svc.Start(user); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.svc.Stop(false) })
	return d
}

func (d *device) onEvent(_ string, data any) {
	d.mu.Lock()
	d.evts = append(d.evts, data)
	d.mu.Unlock()
	if p, ok := data.(DeviceProgress); ok {
		if p.Stage == "code" {
			d.codes <- p.Code
		} else {
			d.done <- p
		}
	}
}

func (d *device) refresh() State {
	d.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return d.svc.Refresh(ctx)
}

func (d *device) enroll() string {
	d.t.Helper()
	code, err := d.svc.Enroll(context.Background())
	if err != nil {
		d.t.Fatalf("enroll: %v", err)
	}
	if st := d.refresh(); st.KeyStatus != "unlocked" || st.Fingerprint == "" {
		d.t.Fatalf("after enroll = %+v", st)
	}
	return code
}

// key 把测试里的 "org:<组织>" 换成本设备账号的本地空间标识 (org:<组织>@<用户>)。
func (d *device) key(k string) string {
	if org, ok := strings.CutPrefix(k, space.OrgPrefix); ok && !strings.Contains(org, "@") {
		return LocalKey(d.user, org)
	}
	return k
}

func (d *device) space(key string) *space.Space {
	d.t.Helper()
	key = d.key(key)
	sp := d.reg.Get(key)
	if sp == nil || !d.reg.Visible(key) {
		d.t.Fatalf("%s: space %s not visible; state %+v", d.user, key, d.svc.State())
	}
	return sp
}

func (d *device) sync(key string) {
	d.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := d.svc.SyncNow(ctx, d.key(key)); err != nil {
		d.t.Fatalf("%s sync %s: %v", d.user, key, err)
	}
}

func (d *device) save(key string, c conn.Connection) conn.Connection {
	d.t.Helper()
	saved, err := d.space(key).SaveConnection(c)
	if err != nil {
		d.t.Fatal(err)
	}
	return saved
}

func (d *device) get(key, id string) conn.Connection {
	d.t.Helper()
	c, err := d.space(key).Conns.Get(id)
	if err != nil {
		d.t.Fatalf("%s get %s: %v", d.user, id, err)
	}
	return c
}

const me = "me:u1"

// 个人空间多设备: 开通、恢复码解锁、连接与脚本往返, 服务端只见密文; 本机空间不参与同步。
func TestPersonalMultiDevice(t *testing.T) {
	srv := newServer(t)
	tok := srv.AddUser("u1")
	a := newDevice(t, srv, "u1", tok)
	if st := a.refresh(); !st.Available || st.KeyStatus != "not_enrolled" {
		t.Fatalf("fresh state = %+v", st)
	}
	code := a.enroll()

	if _, err := a.reg.Local().SaveConnection(conn.Connection{Name: "local only", Protocol: "ssh", Host: "10.0.0.9"}); err != nil {
		t.Fatal(err)
	}
	c := a.save(me, conn.Connection{Name: "web", Protocol: "ssh", Host: "secret-host.example.com", Username: "root"})
	if _, err := a.space(me).SaveScript(script.Script{Name: "uptime", Content: "uptime\n"}); err != nil {
		t.Fatal(err)
	}
	a.sync(me)
	if n := a.space(me).PendingCount(); n != 0 {
		t.Fatalf("unpushed after sync = %d", n)
	}
	raw := srv.RawDoc(synctest.PersonalKey("u1"), space.CollConnections, c.ID)
	if raw == nil || strings.Contains(string(raw.Payload), "secret-host") {
		t.Fatalf("server sees plaintext: %v", raw)
	}

	// 第二台电脑: 用恢复码解锁
	b := newDevice(t, srv, "u1", tok)
	if st := b.refresh(); st.KeyStatus != "locked" {
		t.Fatalf("second device = %+v", st)
	}
	if err := b.svc.Unlock(context.Background(), "wrong-code"); !errors.Is(err, errcode.ErrSyncRecoveryCode) {
		t.Fatalf("wrong code = %v", err)
	}
	if err := b.svc.Unlock(context.Background(), code); err != nil {
		t.Fatal(err)
	}
	if st := b.refresh(); st.KeyStatus != "unlocked" {
		t.Fatalf("after unlock = %+v", st)
	}
	b.sync(me)
	if got := b.get(me, c.ID); got.Host != "secret-host.example.com" || got.UpdatedAt != c.UpdatedAt {
		t.Fatalf("pulled = %+v", got)
	}
	if list, _ := b.space(me).Scripts.List(); len(list) != 1 || list[0].Content != "uptime\n" {
		t.Fatalf("scripts = %+v", list)
	}
	if list, _ := b.reg.Local().Conns.List(); len(list) != 0 {
		t.Fatalf("local space got synced data: %+v", list)
	}

	// 删除也同步过去
	if err := b.space(me).DeleteConnection(c.ID); err != nil {
		t.Fatal(err)
	}
	b.sync(me)
	a.sync(me)
	if _, err := a.space(me).Conns.Get(c.ID); err == nil {
		t.Fatal("delete did not propagate")
	}
}

// 新设备由已解锁的设备批准: 两边看到同一个 6 位码。
func TestDeviceApproval(t *testing.T) {
	srv := newServer(t)
	tok := srv.AddUser("u1")
	a := newDevice(t, srv, "u1", tok)
	a.refresh()
	a.enroll()

	b := newDevice(t, srv, "u1", tok)
	b.refresh()
	ctx := context.Background()
	if err := b.svc.StartDeviceEnrollment(ctx); err != nil {
		t.Fatal(err)
	}
	reqs, err := a.svc.PendingDevices(ctx)
	if err != nil || len(reqs) != 1 {
		t.Fatalf("pending = %+v, %v", reqs, err)
	}
	codeA, err := a.svc.BeginDeviceApproval(ctx, reqs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case codeB := <-b.codes:
		if codeA != codeB || len(codeA) != 6 {
			t.Fatalf("codes %q vs %q", codeA, codeB)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("new device never showed a code")
	}
	if err := a.svc.FinishDeviceApproval(ctx, reqs[0].ID, true); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-b.done:
		if p.Stage != "approved" {
			t.Fatalf("progress = %+v", p)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("new device never got approved")
	}
	if st := b.refresh(); st.KeyStatus != "unlocked" {
		t.Fatalf("after approval = %+v", st)
	}
}

// 团队空间: 成员读写、查看者只读、移除成员后轮换密钥, 被移出者拿不到新内容。
func TestTeamSpace(t *testing.T) {
	srv := newServer(t)
	owner := newDevice(t, srv, "u1", srv.AddUser("u1"))
	member := newDevice(t, srv, "u2", srv.AddUser("u2"))
	viewer := newDevice(t, srv, "u3", srv.AddUser("u3"))
	leaver := newDevice(t, srv, "u4", srv.AddUser("u4"))
	srv.CreateOrg("o1", "运维组", "u1")
	srv.AddMember("o1", "u2", syncsdk.RoleMember)
	srv.AddMember("o1", "u3", syncsdk.RoleViewer)
	srv.AddMember("o1", "u4", syncsdk.RoleMember)
	for _, d := range []*device{owner, member, viewer, leaver} {
		d.refresh()
		d.enroll()
	}
	const org = "org:o1"
	if sp := owner.svc.Space(owner.key(org)); sp == nil || sp.Name != "运维组" || sp.ReadOnly {
		t.Fatalf("owner org space = %+v", sp)
	}

	c := owner.save(org, conn.Connection{Name: "db", Protocol: "ssh", Host: "db.internal"})
	owner.sync(org)
	member.sync(org)
	if got := member.get(org, c.ID); got.Host != "db.internal" {
		t.Fatalf("member read %+v", got)
	}

	viewer.sync(org)
	if _, err := viewer.space(org).SaveConnection(conn.Connection{Protocol: "ssh", Host: "x"}); !errors.Is(err, errcode.ErrSyncReadOnly) {
		t.Fatalf("viewer write = %v", err)
	}

	leaver.sync(org)
	srv.RemoveMember("o1", "u4")
	owner.sync(org) // 轮换出新版本密钥, 只封装给剩下的成员
	c2 := owner.save(org, conn.Connection{Name: "after", Protocol: "ssh", Host: "after.internal"})
	owner.sync(org)
	env, err := e2e.ParseEnvelope(srv.RawDoc("o1", space.CollConnections, c2.ID).Payload)
	if err != nil || env.KV != 2 {
		t.Fatalf("key version = %+v, %v", env, err)
	}
	member.sync(org)
	if got := member.get(org, c2.ID); got.Host != "after.internal" {
		t.Fatal("remaining member cannot read after rotation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := leaver.svc.SyncNow(ctx, leaver.key(org)); err == nil {
		t.Fatal("removed member still syncs")
	}
	leaver.refresh()
	if sp := leaver.svc.Space(leaver.key(org)); sp == nil || !sp.Removed || !sp.ReadOnly {
		t.Fatalf("removed space = %+v", sp)
	}
	if _, err := leaver.space(org).Conns.Get(c2.ID); err == nil {
		t.Fatal("removed member got new content")
	}
	if err := leaver.svc.ForgetSpace(context.Background(), leaver.key(org)); err != nil {
		t.Fatal(err)
	}
	if leaver.reg.Get(leaver.key(org)) != nil || leaver.svc.Space(leaver.key(org)) != nil {
		t.Fatal("space still on disk after forget")
	}
}

// 两台都改了同一条连接: 后同步的那台以服务端为准, 自己的另存为冲突副本并推上去。
func TestConflictCopy(t *testing.T) {
	srv := newServer(t)
	tok := srv.AddUser("u1")
	a := newDevice(t, srv, "u1", tok)
	a.refresh()
	code := a.enroll()
	b := newDevice(t, srv, "u1", tok)
	b.refresh()
	if err := b.svc.Unlock(context.Background(), code); err != nil {
		t.Fatal(err)
	}
	b.refresh()

	c := a.save(me, conn.Connection{Name: "web", Protocol: "ssh", Host: "h1"})
	a.sync(me)
	b.sync(me)

	ca := a.get(me, c.ID)
	ca.Host = "from-a"
	a.save(me, ca)
	cb := b.get(me, c.ID)
	cb.Host = "from-b"
	b.save(me, cb)
	a.sync(me)
	b.sync(me)

	if got := b.get(me, c.ID); got.Host != "from-a" {
		t.Fatalf("server version should win, got %q", got.Host)
	}
	var conflict *ConflictInfo
	b.mu.Lock()
	for _, e := range b.evts {
		if ci, ok := e.(ConflictInfo); ok {
			conflict = &ci
		}
	}
	b.mu.Unlock()
	if conflict == nil || conflict.CopyID == "" {
		t.Fatal("no conflict reported")
	}
	cp := b.get(me, conflict.CopyID)
	if cp.Host != "from-b" || cp.Name != "web"+conflictSuffix {
		t.Fatalf("copy = %+v", cp)
	}
	if n := b.space(me).PendingCount(); n != 0 {
		t.Fatalf("copy should be pushed in the same sync, pending %d", n)
	}
	a.sync(me)
	if got := a.get(me, conflict.CopyID); got.Host != "from-b" {
		t.Fatal("copy did not reach the other device")
	}
}

// 团队里别人推来的连接照样过本机校验: 以 - 开头的主机名 (会被 ssh 当成选项) 不落地, 其余照常。
func TestRemotePayloadValidated(t *testing.T) {
	srv := newServer(t)
	a := newDevice(t, srv, "u1", srv.AddUser("u1"))
	a.refresh()
	a.enroll()
	sp := a.space(me)
	id := conn.NewID()
	err := sp.Apply(space.CollConnections, id, 5, false, []byte(`{"protocol":"ssh","host":"-oProxyCommand=calc"}`))
	if !errors.Is(err, errcode.ErrSyncPayload) {
		t.Fatalf("malicious host applied: %v", err)
	}
	if _, err := sp.Conns.Get(id); err == nil {
		t.Fatal("malicious connection written")
	}
	if err := sp.Apply(space.CollConnections, "../x", 6, false, []byte(`{"protocol":"ssh","host":"h"}`)); !errors.Is(err, errcode.ErrSyncPayload) {
		t.Fatalf("path id applied: %v", err)
	}
	if err := sp.Apply(space.CollConnections, id, 7, false, []byte(`{"protocol":"ssh","host":"ok.example.com","updatedAt":"2026-01-01T00:00:00Z"}`)); err != nil {
		t.Fatal(err)
	}
	if got := a.get(me, id); got.Host != "ok.example.com" || got.UpdatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("applied = %+v", got)
	}
}

// 同步服务连不上: 状态标为不可用, 本地照常写入, 修改留在队列里。
func TestServiceUnavailable(t *testing.T) {
	srv := newServer(t)
	tok := srv.AddUser("u1")
	a := newDevice(t, srv, "u1", tok)
	a.refresh()
	a.enroll()
	srv.SetOffline(true)
	if st := a.refresh(); st.Available {
		t.Fatalf("offline state = %+v", st)
	}
	a.save(me, conn.Connection{Protocol: "ssh", Host: "h"})
	if n := a.space(me).PendingCount(); n != 1 {
		t.Fatalf("pending = %d", n)
	}
	srv.SetOffline(false)
	a.refresh()
	a.sync(me)
	if n := a.space(me).PendingCount(); n != 0 {
		t.Fatalf("pending after reconnect = %d", n)
	}
}

// 本机数据整体搬进个人空间: id 不变、跳板关系保留、搬完上传。
func TestMoveLocalIntoPersonal(t *testing.T) {
	srv := newServer(t)
	tok := srv.AddUser("u1")
	a := newDevice(t, srv, "u1", tok)
	a.refresh()
	a.enroll()
	local := a.reg.Local()
	jump, _ := local.SaveConnection(conn.Connection{Name: "bastion", Protocol: "ssh", Host: "b"})
	inner, err := local.SaveConnection(conn.Connection{Name: "inner", Protocol: "ssh", Host: "i", Jump: jump.ID})
	if err != nil {
		t.Fatal(err)
	}
	res, err := space.MoveAll(local, a.space(me))
	if err != nil || res.Connections != 2 {
		t.Fatalf("move = %+v, %v", res, err)
	}
	if list, _ := local.Conns.List(); len(list) != 0 {
		t.Fatalf("local not emptied: %+v", list)
	}
	if got := a.get(me, inner.ID); got.Jump != jump.ID {
		t.Fatalf("jump lost: %+v", got)
	}
	a.sync(me)
	if srv.RawDoc(synctest.PersonalKey("u1"), space.CollConnections, inner.ID) == nil {
		t.Fatal("moved data not uploaded")
	}
}

// 保存的凭据随个人空间同步: 服务端只见密文, 另一台电脑解锁后能用。
func TestCredentialSync(t *testing.T) {
	srv := newServer(t)
	tok := srv.AddUser("u1")
	a := newDevice(t, srv, "u1", tok)
	a.refresh()
	code := a.enroll()
	sum, err := a.space(me).SaveCredential(cred.Credential{Name: "root@web", Username: "root", Password: "S3cret-pw"})
	if err != nil {
		t.Fatal(err)
	}
	c := a.save(me, conn.Connection{Protocol: "ssh", Host: "web", Auth: conn.AuthStored, Credential: sum.ID})
	a.sync(me)
	raw := srv.RawDoc(synctest.PersonalKey("u1"), space.CollCredentials, sum.ID)
	if raw == nil || strings.Contains(string(raw.Payload), "S3cret-pw") || strings.Contains(string(raw.Payload), "root") {
		t.Fatalf("server sees credential plaintext: %v", raw)
	}

	b := newDevice(t, srv, "u1", tok)
	b.refresh()
	if err := b.svc.Unlock(context.Background(), code); err != nil {
		t.Fatal(err)
	}
	b.refresh()
	b.sync(me)
	got := b.get(me, c.ID)
	cr, err := b.space(me).Creds.Get(got.Credential)
	if err != nil || cr.Password != "S3cret-pw" || cr.Username != "root" {
		t.Fatalf("pulled credential = %+v, %v", cr, err)
	}
}
