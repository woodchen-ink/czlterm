// Package askpass 让系统 ssh 在需要密码时回调本程序, 密码不出现在命令行、脚本文件与剪贴板里。
//
// 启动 ssh 时设置 SSH_ASKPASS=<本程序>、SSH_ASKPASS_REQUIRE=force, 以及本包的两个变量
// (本地端点地址与一次性令牌)。ssh 需要输入时以提示语为参数运行本程序, 本程序识别出
// "密码" 类提示后凭令牌向桌面端取密码写到 stdout; 其它提示 (主机指纹确认、两步验证码)
// 转到终端里让用户自己回答, 行为与不设 askpass 时一致。
//
// 令牌只对一次启动有效: 限定次数与有效期, 端点只监听 127.0.0.1 且拒绝带 Origin 的请求。
package askpass

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 传给 askpass 子进程的环境变量。
const (
	EnvURL   = "CZLTERM_ASKPASS_URL"
	EnvToken = "CZLTERM_ASKPASS_TOKEN"
)

const (
	tokenTTL     = 10 * time.Minute
	tokenMaxUses = 6
)

// Credential 是一跳 (目标机或跳板机) 的密码。Target 为 "user@host", 与 ssh 提示语中的写法一致。
type Credential struct {
	Target   string
	Password string
}

type grant struct {
	creds   []Credential
	hops    int
	expires time.Time
	uses    int
}

// Broker 持有一次性令牌并提供本地 HTTP 端点。
type Broker struct {
	mu     sync.Mutex
	grants map[string]*grant
	url    string
	srv    *http.Server
}

// Start 在 127.0.0.1 随机端口上启动端点。
func Start() (*Broker, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("4201 listen askpass: %w", err)
	}
	b := &Broker{grants: map[string]*grant{}, url: "http://" + ln.Addr().String() + "/askpass"}
	mux := http.NewServeMux()
	mux.HandleFunc("/askpass", b.handle)
	b.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = b.srv.Serve(ln) }()
	return b, nil
}

// RevokeAll 作废全部令牌。保险库锁定时调用: 锁定后已启动的 ssh 也不能再取到密码。
func (b *Broker) RevokeAll() {
	b.mu.Lock()
	b.grants = map[string]*grant{}
	b.mu.Unlock()
}

// Close 停止端点。
func (b *Broker) Close() { _ = b.srv.Close() }

// Grant 登记一组密码, 返回需要注入 ssh 进程的环境变量。hops 是整条链的跳数 (含目标机)。
func (b *Broker) Grant(creds []Credential, hops int) []string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	token := hex.EncodeToString(buf)

	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for k, g := range b.grants {
		if now.After(g.expires) {
			delete(b.grants, k)
		}
	}
	b.grants[token] = &grant{creds: creds, hops: hops, expires: now.Add(tokenTTL)}
	return []string{EnvURL + "=" + b.url, EnvToken + "=" + token}
}

type request struct {
	Prompt string `json:"prompt"`
}

type response struct {
	Password string `json:"password"`
}

func (b *Broker) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("Origin") != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	var req request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	var g *grant
	for k, v := range b.grants {
		if subtle.ConstantTimeCompare([]byte(k), []byte(token)) == 1 {
			g = v
			break
		}
	}
	if g == nil || time.Now().After(g.expires) || g.uses >= tokenMaxUses {
		b.mu.Unlock()
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	g.uses++
	pw, ok := match(g.creds, g.hops, req.Prompt)
	b.mu.Unlock()

	if !ok {
		http.Error(w, "no match", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(response{Password: pw})
}

// match 按提示语里的目标找到对应的一跳。两种写法:
//   - 密码认证: "user@host's password: "
//   - keyboard-interactive (OpenSSH 8.4+): "(user@host) Password: "
//
// 提示语里没有目标时只在整条链只有一跳的情况下使用唯一的密码; 多跳时宁可交给终端,
// 也不能把目标机的密码发给跳板机。
func match(creds []Credential, hops int, prompt string) (string, bool) {
	if target, ok := promptTarget(prompt); ok {
		for _, c := range creds {
			if strings.EqualFold(c.Target, target) {
				return c.Password, true
			}
		}
		return "", false
	}
	if hops == 1 && len(creds) == 1 {
		return creds[0].Password, true
	}
	return "", false
}

// promptTarget 从提示语中取出 "user@host"。
func promptTarget(prompt string) (string, bool) {
	p := strings.TrimSpace(prompt)
	if strings.HasPrefix(p, "(") {
		if i := strings.Index(p, ")"); i > 1 {
			return strings.TrimSpace(p[1:i]), true
		}
	}
	if i := strings.Index(strings.ToLower(p), "'s password"); i >= 0 {
		target := strings.TrimSpace(p[:i])
		if j := strings.LastIndexAny(target, " \t"); j >= 0 {
			target = target[j+1:]
		}
		return target, true
	}
	return "", false
}

// IsPasswordPrompt 判断提示语是否在要登录密码。主机指纹确认、私钥口令、验证码都不算。
func IsPasswordPrompt(prompt string) bool {
	p := strings.ToLower(prompt)
	return strings.Contains(p, "password") && !strings.Contains(p, "passphrase") && !strings.Contains(p, "(yes/no")
}

// ErrNoCredential 表示桌面端没有对应的密码, 需要用户在终端里手动输入。
var ErrNoCredential = errors.New("no credential")
