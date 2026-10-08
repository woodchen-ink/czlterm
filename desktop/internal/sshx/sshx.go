// Package sshx 是进程内的 SSH 客户端, 供文件管理 (SFTP) 与 MCP (执行命令、读写文件) 使用。
//
// 交互式终端仍交给系统 ssh; 这里的连接按目标连接 ID 复用, 空闲 10 分钟后关闭。
//
// 主机密钥按 ~/.ssh/known_hosts 校验, 与系统 ssh 共用一份: 已知且不符时拒绝连接;
// 未知主机按 accept-new 语义记录后继续 (等同 StrictHostKeyChecking=accept-new),
// 文件管理与 MCP 没有终端可以弹出指纹确认。
package sshx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	dialTimeout = 15 * time.Second
	idleTimeout = 10 * time.Minute
)

// Hop 是链路上的一跳。Chain 中最后一个是目标机, 前面依次是跳板机。
type Hop struct {
	Host     string
	Port     int
	User     string
	Password string
	// Signers 来自 Vaultwarden 的私钥; 为空且 Password 为空时用系统 agent 与 ~/.ssh 默认私钥。
	Signers []ssh.Signer
}

func (h Hop) addr() string { return net.JoinHostPort(h.Host, strconv.Itoa(h.Port)) }

// Pool 复用 SSH 连接。
type Pool struct {
	mu      sync.Mutex
	entries map[string]*entry
	hostsMu sync.Mutex
}

type entry struct {
	key     string
	clients []*ssh.Client // 依次为每一跳的客户端, 关闭时倒序关闭
	sftp    *sftp.Client
	used    time.Time
	timer   *time.Timer
}

// NewPool 创建连接池。
func NewPool() *Pool {
	return &Pool{entries: map[string]*entry{}}
}

// chainKey 标识一条链路的配置; 配置变了 (改了主机、换了密码) 就不复用旧连接。
func chainKey(chain []Hop) string {
	var b strings.Builder
	for _, h := range chain {
		fmt.Fprintf(&b, "%s@%s|%d|", h.User, h.addr(), len(h.Password))
		for _, s := range h.Signers {
			b.WriteString(ssh.FingerprintSHA256(s.PublicKey()))
		}
		b.WriteString(";")
	}
	return b.String()
}

// Client 返回到 id 目标的 SSH 客户端, 必要时建立连接。
func (p *Pool) Client(ctx context.Context, id string, chain []Hop) (*ssh.Client, error) {
	e, err := p.get(ctx, id, chain)
	if err != nil {
		return nil, err
	}
	return e.clients[len(e.clients)-1], nil
}

// SFTP 返回到 id 目标的 SFTP 客户端。
func (p *Pool) SFTP(ctx context.Context, id string, chain []Hop) (*sftp.Client, error) {
	e, err := p.get(ctx, id, chain)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if e.sftp == nil {
		c, err := sftp.NewClient(e.clients[len(e.clients)-1])
		if err != nil {
			return nil, fmt.Errorf("5001 start sftp: %w", err)
		}
		e.sftp = c
	}
	return e.sftp, nil
}

// Drop 关闭 id 的连接, 下次使用时重连。操作失败疑似连接已断时调用。
func (p *Pool) Drop(id string) {
	p.mu.Lock()
	e := p.entries[id]
	delete(p.entries, id)
	p.mu.Unlock()
	if e != nil {
		e.close()
	}
}

// CloseAll 关闭全部连接。保险库锁定与退出时调用: 连接是用保险库里的凭据建立的。
func (p *Pool) CloseAll() {
	p.mu.Lock()
	entries := p.entries
	p.entries = map[string]*entry{}
	p.mu.Unlock()
	for _, e := range entries {
		e.close()
	}
}

func (p *Pool) get(ctx context.Context, id string, chain []Hop) (*entry, error) {
	if len(chain) == 0 {
		return nil, errors.New("5002 empty connection chain")
	}
	key := chainKey(chain)

	p.mu.Lock()
	e := p.entries[id]
	p.mu.Unlock()
	// 存活探测要走一次网络往返, 不能持锁做。
	if e != nil && e.key == key && e.alive() {
		p.mu.Lock()
		if p.entries[id] == e {
			e.used = time.Now()
			e.timer.Reset(idleTimeout)
			p.mu.Unlock()
			return e, nil
		}
		p.mu.Unlock()
	} else if e != nil {
		p.mu.Lock()
		if p.entries[id] == e {
			delete(p.entries, id)
		}
		p.mu.Unlock()
		e.close()
	}

	clients, err := p.dial(ctx, chain)
	if err != nil {
		return nil, err
	}
	e = &entry{key: key, clients: clients, used: time.Now()}
	e.timer = time.AfterFunc(idleTimeout, func() { p.dropIfIdle(id, e) })

	p.mu.Lock()
	if old := p.entries[id]; old != nil {
		// 并发请求同时建了连接: 保留先到的一条。
		p.mu.Unlock()
		e.close()
		return old, nil
	}
	p.entries[id] = e
	p.mu.Unlock()
	return e, nil
}

func (p *Pool) dropIfIdle(id string, e *entry) {
	p.mu.Lock()
	if p.entries[id] != e {
		p.mu.Unlock()
		return
	}
	delete(p.entries, id)
	p.mu.Unlock()
	e.close()
}

// dial 依次建立各跳连接: 第一跳直连, 之后经上一跳的 direct-tcpip 通道。
func (p *Pool) dial(ctx context.Context, chain []Hop) ([]*ssh.Client, error) {
	var clients []*ssh.Client
	fail := func(err error) ([]*ssh.Client, error) {
		for i := len(clients) - 1; i >= 0; i-- {
			clients[i].Close()
		}
		return nil, err
	}
	for i, h := range chain {
		cfg, err := p.config(h)
		if err != nil {
			return fail(err)
		}
		var conn net.Conn
		if i == 0 {
			d := net.Dialer{Timeout: dialTimeout}
			conn, err = d.DialContext(ctx, "tcp", h.addr())
		} else {
			conn, err = clients[i-1].DialContext(ctx, "tcp", h.addr())
		}
		if err != nil {
			return fail(fmt.Errorf("5003 connect %s: %w", h.addr(), err))
		}
		// 握手阶段 ssh 库不看 ctx, 用连接截止时间兜底。
		_ = conn.SetDeadline(time.Now().Add(dialTimeout))
		c, chans, reqs, err := ssh.NewClientConn(conn, h.addr(), cfg)
		if err != nil {
			conn.Close()
			return fail(fmt.Errorf("5004 ssh handshake with %s: %w", h.addr(), err))
		}
		_ = conn.SetDeadline(time.Time{})
		clients = append(clients, ssh.NewClient(c, chans, reqs))
	}
	return clients, nil
}

func (p *Pool) config(h Hop) (*ssh.ClientConfig, error) {
	u := h.User
	if u == "" {
		if cur, err := user.Current(); err == nil {
			u = cur.Username
			// Windows 下是 DOMAIN\user。
			if i := strings.LastIndexByte(u, '\\'); i >= 0 {
				u = u[i+1:]
			}
		}
	}
	var methods []ssh.AuthMethod
	if len(h.Signers) > 0 {
		methods = append(methods, ssh.PublicKeys(h.Signers...))
	}
	if h.Password != "" {
		pw := h.Password
		methods = append(methods, ssh.Password(pw),
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				ans := make([]string, len(qs))
				for i, q := range qs {
					if strings.Contains(strings.ToLower(q), "password") {
						ans[i] = pw
					}
				}
				return ans, nil
			}))
	}
	if len(methods) == 0 {
		methods = systemAuth()
	}
	return &ssh.ClientConfig{
		User:              u,
		Auth:              methods,
		HostKeyCallback:   p.hostKeyCallback,
		HostKeyAlgorithms: knownAlgorithms(h.Host, h.Port),
		Timeout:           dialTimeout,
	}, nil
}

// systemAuth 用系统 ssh-agent 与 ~/.ssh 下未加密的默认私钥, 对应连接配置里的"系统默认"。
func systemAuth() []ssh.AuthMethod {
	var signers []ssh.Signer
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		sock = defaultAgentSock
	}
	if sock != "" {
		if c, err := dialAgent(sock); err == nil {
			if s, err := sshagent.NewClient(c).Signers(); err == nil {
				signers = append(signers, s...)
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			data, err := os.ReadFile(filepath.Join(home, ".ssh", name))
			if err != nil {
				continue
			}
			if s, err := ssh.ParsePrivateKey(data); err == nil {
				signers = append(signers, s)
			}
		}
	}
	if len(signers) == 0 {
		return nil
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signers...)}
}

// hostKeyCallback 按 known_hosts 校验, 未知主机记录后放行。
func (p *Pool) hostKeyCallback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	path, err := knownHostsPath()
	if err != nil {
		return err
	}
	p.hostsMu.Lock()
	defer p.hostsMu.Unlock()

	if _, err := os.Stat(path); err == nil {
		cb, err := knownhosts.New(path)
		if err != nil {
			return fmt.Errorf("5005 read known_hosts: %w", err)
		}
		err = cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) || len(ke.Want) > 0 {
			return fmt.Errorf("5006 host key mismatch for %s, check ~/.ssh/known_hosts: %w", hostname, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("5007 write known_hosts: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key) + "\n")
	return err
}

func knownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("5008 locate home dir: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

// alive 发一次 keepalive, 2 秒内没有回应视为已断 (休眠唤醒、换网络后 TCP 常常半开)。
func (e *entry) alive() bool {
	done := make(chan error, 1)
	go func() {
		_, _, err := e.clients[len(e.clients)-1].SendRequest("keepalive@openssh.com", true, nil)
		done <- err
	}()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(2 * time.Second):
		return false
	}
}

func (e *entry) close() {
	if e.timer != nil {
		e.timer.Stop()
	}
	if e.sftp != nil {
		e.sftp.Close()
	}
	for i := len(e.clients) - 1; i >= 0; i-- {
		e.clients[i].Close()
	}
}
