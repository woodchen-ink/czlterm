package space

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/cred"
	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
	"github.com/woodchen-ink/czlterm/desktop/internal/secret"
)

func TestMain(m *testing.M) {
	// 凭据文件用系统钥匙串里的密钥加密; 测试与 CI 里没有钥匙串, 换成固定的测试密钥
	secret.UseKeyForTest(make([]byte, 32))
	os.Exit(m.Run())
}

func newRegistry(t *testing.T, visible ...string) *Registry {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range visible {
		if _, err := r.Ensure(k); err != nil {
			t.Fatal(err)
		}
	}
	r.SetVisible(visible)
	return r
}

const me = "me:u1"

// 推送途中本地又改了同一条: 推送确认用的是旧序号, 新修改仍待上传, 不被当成已上传清掉。
func TestAckKeepsNewerLocalEdit(t *testing.T) {
	r := newRegistry(t, me)
	sp := r.Get(me)
	c, err := sp.SaveConnection(conn.Connection{Protocol: "ssh", Host: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	out := sp.Outgoing(0)
	if len(out) != 1 || out[0].Missing {
		t.Fatalf("outgoing = %+v", out)
	}
	c.Host = "h2"
	if _, err := sp.SaveConnection(c); err != nil {
		t.Fatal(err)
	}
	if err := sp.Ack(CollConnections, c.ID, out[0].Dirty, out[0].Rev, 7); err != nil {
		t.Fatal(err)
	}
	st := sp.State(CollConnections, c.ID)
	if st == nil || st.Dirty == 0 || st.Rev != 7 {
		t.Fatalf("state after ack = %+v, want still dirty with base rev 7", st)
	}
	// 拉到别处的版本时, 本地有未上传修改不覆盖
	if err := sp.Apply(CollConnections, c.ID, 8, false, []byte(`{"protocol":"ssh","host":"remote"}`)); err != ErrLocalChanged {
		t.Fatalf("apply over dirty = %v", err)
	}
	if got, _ := sp.Conns.Get(c.ID); got.Host != "h2" {
		t.Fatalf("local edit lost: %+v", got)
	}
}

// 不同空间有同 id 的连接时, 按当前空间优先解析。
func TestFindPrefersCurrentSpace(t *testing.T) {
	r := newRegistry(t, me)
	c := conn.Connection{ID: conn.NewID(), Name: "local", Protocol: "ssh", Host: "local-host"}
	if err := r.Local().Conns.Put(c); err != nil {
		t.Fatal(err)
	}
	c.Name, c.Host = "cloud", "cloud-host"
	if err := r.Get(me).Conns.Put(c); err != nil {
		t.Fatal(err)
	}
	if got, sp, _ := r.FindConnection(me, c.ID); sp.Key != me || got.Host != "cloud-host" {
		t.Fatalf("prefer me: got %s from %s", got.Host, sp.Key)
	}
	if got, sp, _ := r.FindConnection(Local, c.ID); sp.Key != Local || got.Host != "local-host" {
		t.Fatalf("prefer local: got %s from %s", got.Host, sp.Key)
	}
}

// 搬进同步空间时目标已有同 id: 内容相同只删来源, 不同则另存为新 id, 都不覆盖目标版本。
func TestMoveAllCollisions(t *testing.T) {
	r := newRegistry(t, me)
	local, cloud := r.Local(), r.Get(me)
	same := conn.Connection{ID: conn.NewID(), Name: "same", Protocol: "ssh", Host: "same"}
	diff := conn.Connection{ID: conn.NewID(), Name: "diff", Protocol: "ssh", Host: "old"}
	for _, c := range []conn.Connection{same, diff} {
		if err := local.Conns.Put(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := cloud.Conns.Put(same); err != nil {
		t.Fatal(err)
	}
	newer := diff
	newer.Host = "newer"
	if err := cloud.Conns.Put(newer); err != nil {
		t.Fatal(err)
	}
	res, err := MoveAll(local, cloud)
	if err != nil {
		t.Fatal(err)
	}
	if res.Merged != 1 || res.Renamed != 1 {
		t.Fatalf("result = %+v", res)
	}
	if got, _ := cloud.Conns.Get(diff.ID); got.Host != "newer" {
		t.Fatalf("target version overwritten: %+v", got)
	}
	list, _ := cloud.Conns.List()
	if len(list) != 3 {
		t.Fatalf("cloud list = %+v", list)
	}
	if left, _ := local.Conns.List(); len(left) != 0 {
		t.Fatalf("local not emptied: %+v", left)
	}
}

// 搬移带上保存的凭据, 连接里的引用跟着改写。
func TestMoveAllCredentials(t *testing.T) {
	r := newRegistry(t, me)
	local, cloud := r.Local(), r.Get(me)
	sum, err := local.SaveCredential(cred.Credential{Name: "root", Username: "root", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := local.SaveConnection(conn.Connection{Protocol: "ssh", Host: "h", Auth: conn.AuthStored, Credential: sum.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MoveAll(local, cloud); err != nil {
		t.Fatal(err)
	}
	got, err := cloud.Conns.Get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	cr, err := cloud.Creds.Get(got.Credential)
	if err != nil || cr.Password != "pw" {
		t.Fatalf("credential after move = %+v, %v", cr, err)
	}
	// 凭据还被连接引用时不能删
	if err := cloud.DeleteCredential(cr.ID); err == nil {
		t.Fatal("deleted a credential still in use")
	}
}

// 团队连接: 本人保存的不用确认; 别人推来或改了主机、凭据的, 连接前要确认。
func TestTeamConnectionConfirm(t *testing.T) {
	org := OrgPrefix + "o1@u1"
	r := newRegistry(t, org)
	sp := r.Get(org)
	mine, err := sp.SaveConnection(conn.Connection{Protocol: "ssh", Host: "a", Auth: conn.AuthVault, VaultItem: "item1"})
	if err != nil {
		t.Fatal(err)
	}
	if sp.NeedsConfirm(mine) {
		t.Fatal("own connection needs confirm")
	}
	// 先上传 (确认 rev 2), 再拉到同事把主机改到别处的版本
	st := sp.State(CollConnections, mine.ID)
	if err := sp.Ack(CollConnections, mine.ID, st.Dirty, st.Rev, 2); err != nil {
		t.Fatal(err)
	}
	if err := sp.Apply(CollConnections, mine.ID, 3, false, []byte(`{"protocol":"ssh","host":"evil","auth":"vault","vaultItem":"item1"}`)); err != nil {
		t.Fatal(err)
	}
	changed, _ := sp.Conns.Get(mine.ID)
	if !sp.NeedsConfirm(changed) {
		t.Fatal("teammate's change not flagged")
	}
	sp.Confirm(changed)
	if sp.NeedsConfirm(changed) {
		t.Fatal("still flagged after confirm")
	}
	// 不带凭据的连接没什么可泄露, 不打扰
	if sp.NeedsConfirm(conn.Connection{ID: conn.NewID(), Protocol: "ssh", Host: "x", Auth: conn.AuthSystem}) {
		t.Fatal("system-auth connection flagged")
	}
	// 个人空间不需要确认
	r2 := newRegistry(t, me)
	if r2.Get(me).NeedsConfirm(changed) {
		t.Fatal("personal space flagged")
	}
}

// 凭据文件里没有明文。
func TestCredentialEncryptedAtRest(t *testing.T) {
	r := newRegistry(t)
	sum, err := r.Local().SaveCredential(cred.Credential{Username: "root", Password: "very-secret-pw"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(r.Local().Dir, "credentials", sum.ID+".sec"))
	if err != nil {
		t.Fatal(err)
	}
	if contains(raw, "very-secret-pw") || contains(raw, "root") {
		t.Fatal("credential stored in plaintext")
	}
}

func contains(b []byte, s string) bool {
	return len(s) > 0 && string(b) != "" && indexOf(string(b), s) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// 整个文件夹复制到团队: 文件夹名与组内跳板关系保留, 共用的跳板机与凭据只复制一份, 组外跳板机一并带过去。
func TestCopyGroup(t *testing.T) {
	org := OrgPrefix + "o1@u1"
	r := newRegistry(t, me, org)
	src, dst := r.Get(me), r.Get(org)
	sum, err := src.SaveCredential(cred.Credential{Name: "root", Username: "root", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	outside, err := src.SaveConnection(conn.Connection{Name: "edge", Group: "infra", Protocol: "ssh", Host: "edge"})
	if err != nil {
		t.Fatal(err)
	}
	bastion, err := src.SaveConnection(conn.Connection{Name: "bastion", Group: "prod", Protocol: "ssh", Host: "b", Jump: outside.ID, Auth: conn.AuthStored, Credential: sum.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"web1", "web2"} {
		if _, err := src.SaveConnection(conn.Connection{Name: h, Group: "prod", Protocol: "ssh", Host: h, Jump: bastion.ID, Auth: conn.AuthStored, Credential: sum.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := src.SaveConnection(conn.Connection{Name: "other", Group: "dev", Protocol: "ssh", Host: "o"}); err != nil {
		t.Fatal(err)
	}

	out, _, err := CopyGroup(src, dst, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("copied %d, want 3", len(out))
	}
	list, _ := dst.Conns.List()
	byName := map[string]conn.Connection{}
	for _, c := range list {
		if _, dup := byName[c.Name]; dup {
			t.Fatalf("%s copied twice", c.Name)
		}
		byName[c.Name] = c
	}
	if len(list) != 4 || byName["other"].ID != "" {
		t.Fatalf("target has %d connections: %v", len(list), byName)
	}
	b := byName["bastion"]
	if b.Group != "prod" || b.Jump != byName["edge"].ID || byName["edge"].Group != "infra" {
		t.Fatalf("bastion = %+v, edge = %+v", b, byName["edge"])
	}
	for _, h := range []string{"web1", "web2"} {
		if w := byName[h]; w.Jump != b.ID || w.Credential != b.Credential || w.Group != "prod" {
			t.Fatalf("%s = %+v, bastion = %+v", h, w, b)
		}
	}
	creds, _ := dst.Creds.List()
	if len(creds) != 1 || creds[0].ID == sum.ID {
		t.Fatalf("target credentials = %+v", creds)
	}
	if _, _, err := CopyGroup(src, dst, "missing"); err != errcode.ErrGroupEmpty {
		t.Fatalf("empty group err = %v", err)
	}
}

// Vaultwarden 引用转为保存的凭据: 共用同一条目的只建一条凭据, 跳板链一并转换, 读不到的保留原引用。
func TestConvertVault(t *testing.T) {
	r := newRegistry(t, me)
	sp := r.Get(me)
	jump, err := sp.SaveConnection(conn.Connection{Name: "jump", Protocol: "ssh", Host: "j", Auth: conn.AuthVault, KeyItem: "key1"})
	if err != nil {
		t.Fatal(err)
	}
	var web []conn.Connection
	for _, h := range []string{"w1", "w2"} {
		c, err := sp.SaveConnection(conn.Connection{Name: h, Protocol: "ssh", Host: h, Jump: jump.ID, Auth: conn.AuthVault, VaultItem: "pw1"})
		if err != nil {
			t.Fatal(err)
		}
		web = append(web, c)
	}
	gone, err := sp.SaveConnection(conn.Connection{Name: "gone", Protocol: "ssh", Host: "g", Auth: conn.AuthVault, VaultItem: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := sp.SaveConnection(conn.Connection{Name: "plain", Protocol: "ssh", Host: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if !NeedsVault(sp, []string{web[0].ID}) || NeedsVault(sp, []string{plain.ID}) {
		t.Fatal("NeedsVault wrong")
	}
	read := func(vi, ki string) (cred.Credential, error) {
		switch {
		case vi == "pw1":
			return cred.Credential{Name: "web", Username: "root", Password: "pw"}, nil
		case ki == "key1":
			return cred.Credential{Name: "jump key", Username: "ops", PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nx\n-----END OPENSSH PRIVATE KEY-----\n"}, nil
		}
		return cred.Credential{}, errcode.ErrSpaceUnknown
	}
	res, err := ConvertVault(sp, []string{web[0].ID, web[1].ID, gone.ID}, read)
	if err != nil {
		t.Fatal(err)
	}
	if res.Converted != 3 || res.Credentials != 2 || res.Skipped != 1 {
		t.Fatalf("result = %+v", res)
	}
	w1, _ := sp.Conns.Get(web[0].ID)
	w2, _ := sp.Conns.Get(web[1].ID)
	j, _ := sp.Conns.Get(jump.ID)
	g, _ := sp.Conns.Get(gone.ID)
	if w1.Auth != conn.AuthStored || w1.VaultItem != "" || w1.Credential == "" || w1.Credential != w2.Credential {
		t.Fatalf("w1 = %+v, w2 = %+v", w1, w2)
	}
	if j.Auth != conn.AuthStored || j.KeyItem != "" || j.Credential == w1.Credential {
		t.Fatalf("jump = %+v", j)
	}
	if g.Auth != conn.AuthVault || g.VaultItem != "missing" {
		t.Fatalf("unreadable connection changed: %+v", g)
	}
	cr, err := sp.Creds.Get(w1.Credential)
	if err != nil || cr.Password != "pw" || cr.Username != "root" {
		t.Fatalf("credential = %+v, %v", cr, err)
	}
}
