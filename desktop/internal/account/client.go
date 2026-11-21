// Package account 实现 CZL Connect 登录 (OAuth2 授权码 + PKCE, 公共客户端无 secret)。
//
// 打开系统浏览器授权, 经自定义协议 czlterm:// 回调: Windows 由第二个进程把回调 URL 交给
// 已运行的实例, macOS 经系统 URL 事件直达; 这里用授权码换 token。
// 登录是可选的, 只给云同步、团队与订阅用; 不登录时程序全部本地功能照常。
package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
)

// Config 是 CZL Connect 的接入参数。
type Config struct {
	ClientID     string
	RedirectURI  string
	AuthorizeURL string
	TokenURL     string
	UserInfoURL  string
	Scope        string
}

// DefaultConfig 是正式环境配置。
var DefaultConfig = Config{
	// ClientID 是 Connect 里 czlterm 应用的公共客户端 id (认证方式 none + PKCE), 为空时登录入口提示未开通。
	ClientID:     "client_51852963",
	RedirectURI:  "czlterm://oauth/callback",
	AuthorizeURL: "https://connect.czl.net/oauth2/authorize",
	TokenURL:     "https://connect.czl.net/api/oauth2/token",
	UserInfoURL:  "https://connect.czl.net/api/oauth2/userinfo",
	Scope:        "read",
}

// CallbackScheme 是回调协议前缀, 用于从命令行参数里认出回调 URL。
const CallbackScheme = "czlterm://"

// tokenResponse 是 token 接口的返回。
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// UserID 是用户在本应用下的标识 (与 OIDC sub 相同)。新建的 Connect 应用下发 UUID 字符串,
// 存量应用是数字; 两种都收, 统一成字符串。
type UserID string

// UnmarshalJSON 接受字符串或数字。
func (u *UserID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*u = UserID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*u = UserID(n.String())
	return nil
}

// User 是界面展示用的账号信息。
type User struct {
	ID       UserID `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Avatar   string `json:"avatar"`
}

// DisplayName 取展示名: 昵称 > 用户名 > 邮箱。
func (u User) DisplayName() string {
	for _, s := range []string{u.Nickname, u.Username, u.Email} {
		if s != "" {
			return s
		}
	}
	return ""
}

// client 封装对 CZL Connect 的 HTTP 调用。
type client struct {
	cfg  Config
	http *http.Client
}

func newClient(cfg Config) *client {
	return &client{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}}
}

// errRejected 表示服务端明确拒绝 (4xx): 授权码或 refresh token 无效。
// 与网络故障区分开: 前者要求重新登录, 后者走离线宽限。
var errRejected = errors.New("auth rejected by server")

func (c *client) authorizeURL(state, challenge string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.cfg.ClientID},
		"redirect_uri":          {c.cfg.RedirectURI},
		"scope":                 {c.cfg.Scope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return c.cfg.AuthorizeURL + "?" + q.Encode()
}

func (c *client) exchange(ctx context.Context, code, verifier string) (*tokenResponse, error) {
	return c.postToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {c.cfg.ClientID},
		"redirect_uri":  {c.cfg.RedirectURI},
		"code_verifier": {verifier},
	})
}

func (c *client) refresh(ctx context.Context, refreshToken string) (*tokenResponse, error) {
	return c.postToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.cfg.ClientID},
	})
}

func (c *client) postToken(ctx context.Context, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errcode.ErrAuthRequest.Wrap(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	var tr tokenResponse
	if err := c.do(req, &tr); err != nil {
		return nil, err
	}
	if tr.AccessToken == "" {
		return nil, errcode.ErrAuthResponse.Wrap(fmt.Errorf("token response without access_token: %s", tr.Error))
	}
	return &tr, nil
}

func (c *client) userInfo(ctx context.Context, accessToken string) (*User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.UserInfoURL, nil)
	if err != nil {
		return nil, errcode.ErrAuthRequest.Wrap(err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	var u User
	if err := c.do(req, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// do 发请求并解析 JSON。4xx 包成 errRejected, 其它失败归为请求失败。
func (c *client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return errcode.ErrAuthRequest.Wrap(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errcode.ErrAuthRequest.Wrap(err)
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return errcode.ErrAuthExpired.Wrap(fmt.Errorf("%w: http %d: %s", errRejected, resp.StatusCode, truncate(body)))
	}
	if resp.StatusCode != http.StatusOK {
		return errcode.ErrAuthRequest.Wrap(fmt.Errorf("http %d: %s", resp.StatusCode, truncate(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return errcode.ErrAuthResponse.Wrap(err)
	}
	return nil
}

func truncate(b []byte) string {
	if len(b) > 200 {
		b = b[:200]
	}
	return string(b)
}
