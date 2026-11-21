package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
)

// fakeConnect 模拟 CZL Connect 的 token 与 userinfo 接口。
type fakeConnect struct {
	srv          *httptest.Server
	refreshCalls atomic.Int32
	rejectToken  atomic.Bool
	lastForm     url.Values
}

func newFake(t *testing.T) *fakeConnect {
	f := &fakeConnect{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.lastForm = r.PostForm
		if f.rejectToken.Load() {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		if r.PostForm.Get("grant_type") == "refresh_token" {
			f.refreshCalls.Add(1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-" + r.PostForm.Get("grant_type"), "refresh_token": "rt", "expires_in": 3600,
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "username": "wood", "nickname": "Wood"})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeConnect) manager(t *testing.T) *Manager {
	cfg := DefaultConfig
	cfg.TokenURL = f.srv.URL + "/token"
	cfg.UserInfoURL = f.srv.URL + "/userinfo"
	cfg.ClientID = "client_test"
	return NewManager(cfg, filepath.Join(t.TempDir(), "auth.dat"))
}

// login 走一遍完整登录, 返回授权地址里的 state。
func login(t *testing.T, m *Manager) {
	t.Helper()
	au, _ := url.Parse(begin(t, m))
	state := au.Query().Get("state")
	if au.Query().Get("code_challenge_method") != "S256" || au.Query().Get("code_challenge") == "" {
		t.Fatalf("authorize url missing pkce: %s", au)
	}
	u, err := m.Complete(context.Background(), "czlterm://oauth/callback?code=c1&state="+state)
	if err != nil {
		t.Fatal(err)
	}
	if u.DisplayName() != "Wood" {
		t.Fatalf("user = %+v", u)
	}
}

func TestLoginAndStatus(t *testing.T) {
	f := newFake(t)
	m := f.manager(t)
	if st, _ := m.Status(context.Background()); st.LoggedIn {
		t.Fatal("fresh install must be logged out")
	}
	login(t, m)
	if f.lastForm.Get("code_verifier") == "" {
		t.Error("exchange must send code_verifier")
	}
	st, err := m.Status(context.Background())
	if err != nil || !st.LoggedIn || st.Offline || st.User.Username != "wood" {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if f.refreshCalls.Load() != 0 {
		t.Error("valid access token must not hit the network")
	}
}

func TestCallbackGuards(t *testing.T) {
	f := newFake(t)
	m := f.manager(t)
	ctx := context.Background()

	if _, err := m.Complete(ctx, "czlterm://oauth/callback?code=x&state=y"); !errors.Is(err, errcode.ErrAuthNotPending) {
		t.Errorf("no pending: %v", err)
	}
	begin(t, m)
	if _, err := m.Complete(ctx, "czlterm://oauth/callback?code=x&state=forged"); !errors.Is(err, errcode.ErrAuthState) {
		t.Errorf("forged state: %v", err)
	}
	// 一次回调就作废 pending, 重放同一个回调不能再换 token
	au, _ := url.Parse(begin(t, m))
	cb := "czlterm://oauth/callback?error=access_denied&state=" + au.Query().Get("state")
	if _, err := m.Complete(ctx, cb); !errors.Is(err, errcode.ErrAuthDenied) {
		t.Errorf("denied: %v", err)
	}
	if _, err := m.Complete(ctx, cb); !errors.Is(err, errcode.ErrAuthNotPending) {
		t.Errorf("replay: %v", err)
	}
}

func TestExpiredPending(t *testing.T) {
	f := newFake(t)
	m := f.manager(t)
	base := time.Now()
	m.now = func() time.Time { return base }
	au, _ := url.Parse(begin(t, m))
	m.now = func() time.Time { return base.Add(pendingTTL + time.Second) }
	_, err := m.Complete(context.Background(), "czlterm://oauth/callback?code=c&state="+au.Query().Get("state"))
	if !errors.Is(err, errcode.ErrAuthNotPending) {
		t.Errorf("want not pending, got %v", err)
	}
}

func TestRefreshPaths(t *testing.T) {
	f := newFake(t)
	m := f.manager(t)
	login(t, m)
	base := time.Now()

	// token 过期 → 续期成功
	m.now = func() time.Time { return base.Add(2 * time.Hour) }
	st, _ := m.Status(context.Background())
	if !st.LoggedIn || st.Offline || f.refreshCalls.Load() != 1 {
		t.Fatalf("refresh ok: %+v calls=%d", st, f.refreshCalls.Load())
	}

	// 服务端联系不上 → 宽限期内放行并标记离线
	f.srv.Close()
	m.now = func() time.Time { return base.Add(5 * time.Hour) }
	st, _ = m.Status(context.Background())
	if !st.LoggedIn || !st.Offline {
		t.Fatalf("offline grace: %+v", st)
	}
	// 超过宽限期 → 要求重新登录
	m.now = func() time.Time { return base.Add(OfflineGrace + 3*time.Hour) }
	if st, _ = m.Status(context.Background()); st.LoggedIn {
		t.Fatalf("grace exceeded must log out: %+v", st)
	}
}

func TestRefreshRejectedClears(t *testing.T) {
	f := newFake(t)
	m := f.manager(t)
	login(t, m)
	f.rejectToken.Store(true)
	m.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if st, _ := m.Status(context.Background()); st.LoggedIn {
		t.Fatal("rejected refresh must log out")
	}
	// 记录已清掉: 服务恢复后也不会凭旧记录放行
	f.rejectToken.Store(false)
	if st, _ := m.Status(context.Background()); st.LoggedIn {
		t.Fatal("cleared session must stay logged out")
	}
}

// 后台取令牌时续期被拒: 与 Status 一样清掉登录记录, 并通知界面退回登录页。
func TestAccessTokenRejectedClearsAndNotifies(t *testing.T) {
	f := newFake(t)
	m := f.manager(t)
	login(t, m)
	expired := make(chan struct{}, 1)
	m.OnExpired(func() { expired <- struct{}{} })
	f.rejectToken.Store(true)
	m.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := m.AccessToken(context.Background()); !errors.Is(err, errcode.ErrAuthExpired) {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-expired:
	case <-time.After(2 * time.Second):
		t.Fatal("expiry not notified")
	}
	f.rejectToken.Store(false)
	if st, _ := m.Status(context.Background()); st.LoggedIn {
		t.Fatal("record must be cleared")
	}
}

func TestLogout(t *testing.T) {
	f := newFake(t)
	m := f.manager(t)
	login(t, m)
	if err := m.Logout(); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Status(context.Background()); st.LoggedIn {
		t.Fatal("logged out")
	}
}

// 会话测试不碰系统凭据库, 平台加密由 TestVaultRoundTrip 单独覆盖。
func TestMain(m *testing.M) {
	sealVault = func(b []byte) ([]byte, error) { return b, nil }
	openVault = sealVault
	os.Exit(m.Run())
}

func TestIsCallback(t *testing.T) {
	if !IsCallback("czlterm://oauth/callback?code=1") || IsCallback("--flag") || IsCallback("czl") {
		t.Error("IsCallback")
	}
	// 加入团队的链接不能被当成登录回调
	if IsCallback("czlterm://team/join?code=ABCD1234") {
		t.Error("team join link treated as oauth callback")
	}
}

func begin(t *testing.T, m *Manager) string {
	t.Helper()
	u, err := m.Begin()
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestBeginNotConfigured(t *testing.T) {
	m := NewManager(DefaultConfig, filepath.Join(t.TempDir(), "account.dat"))
	if DefaultConfig.ClientID == "" {
		if _, err := m.Begin(); !errors.Is(err, errcode.ErrAuthNotConfigured) {
			t.Fatalf("Begin without client id = %v", err)
		}
	}
}

// 新建的 Connect 应用下发 UUID 字符串, 存量应用是数字, 两种都要认。
func TestUserIDStringOrNumber(t *testing.T) {
	for raw, want := range map[string]UserID{`{"id":7}`: "7", `{"id":"0190f3a2-5b7c"}`: "0190f3a2-5b7c"} {
		var u User
		if err := json.Unmarshal([]byte(raw), &u); err != nil || u.ID != want {
			t.Fatalf("%s => %q, %v", raw, u.ID, err)
		}
	}
}

// 任意网页都能拉起回调: state 不对的伪造回调不能把进行中的登录作废。
func TestForgedCallbackKeepsPendingLogin(t *testing.T) {
	f := newFake(t)
	m := f.manager(t)
	au, _ := url.Parse(begin(t, m))
	state := au.Query().Get("state")
	if _, err := m.Complete(context.Background(), "czlterm://oauth/callback?code=x&state=forged"); !errors.Is(err, errcode.ErrAuthState) {
		t.Fatalf("forged = %v", err)
	}
	if _, err := m.Complete(context.Background(), "czlterm://oauth/callback?code=c1&state="+state); err != nil {
		t.Fatalf("genuine callback after forged one: %v", err)
	}
}
