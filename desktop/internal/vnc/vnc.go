// Package vnc 是内置 VNC 查看器的后端: 记住打开过的 VNC 会话, 把页面 (noVNC) 的 WebSocket 转发到 VNC 服务器的 TCP 端口。
//
// RFB 协议与认证 (含 macOS 的用户名 + 密码认证) 全在页面里完成, 这里只搬字节。
// 会话只是目标与凭据的记录, 每次页面连上 WebSocket 才拨一条新的 TCP 连接, 断线重连即重新拨号。
package vnc

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// dialTimeout 是连 VNC 服务器的超时。
const dialTimeout = 10 * time.Second

// CloseDialFailed 是拨号失败时 WebSocket 的关闭码 (4000-4999 为应用自定义), 原因文本以错误码开头。
const CloseDialFailed = 4000

// Target 是一次 VNC 连接的目标与凭据。
type Target struct {
	ConnID   string
	Title    string
	Host     string
	Port     int
	Username string
	Password string
}

// Info 是交给页面的会话信息, 含凭据: 认证在页面里的 noVNC 完成。
type Info struct {
	ID       string `json:"id"`
	ConnID   string `json:"connId"`
	Title    string `json:"title"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Session 是一个 VNC 会话。
type Session struct {
	id string

	mu     sync.Mutex
	target Target
	conns  map[net.Conn]struct{}
	closed bool
}

// Info 返回会话信息。
func (s *Session) Info() Info {
	s.mu.Lock()
	t := s.target
	s.mu.Unlock()
	return Info{ID: s.id, ConnID: t.ConnID, Title: t.Title, Username: t.Username, Password: t.Password}
}

// SetCredentials 记下用户在画面上手动输入并认证成功的凭据, 重新连接与弹出窗口时沿用。只留在内存里。
func (s *Session) SetCredentials(username, password string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.target.Username, s.target.Password = username, password
}

// Serve 为一个已升级的 WebSocket 拨号并双向转发, 直到任一侧断开。拨号失败时以 CloseDialFailed 关闭 WebSocket。
func (s *Session) Serve(ws *websocket.Conn) {
	s.mu.Lock()
	addr := net.JoinHostPort(s.target.Host, strconv.Itoa(s.target.Port))
	s.mu.Unlock()
	c, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		closeWith(ws, CloseDialFailed, fmt.Sprintf("4024 dial vnc server: %v", err))
		return
	}
	if !s.track(c) {
		c.Close()
		closeWith(ws, websocket.CloseNormalClosure, "")
		return
	}
	defer s.untrack(c)
	Relay(ws, c)
}

func (s *Session) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Session) untrack(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, c)
}

// active 返回正在转发的连接数。
func (s *Session) active() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// close 断开全部转发, 之后不再接受新连接。
func (s *Session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for c := range s.conns {
		c.Close()
	}
}

// Manager 管理全部 VNC 会话。
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager 创建 Manager。
func NewManager() *Manager {
	return &Manager{sessions: map[string]*Session{}}
}

// Create 记下一个会话, 此时还不连服务器。
func (m *Manager) Create(t Target) *Session {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	s := &Session{id: "vnc-" + hex.EncodeToString(b), target: t, conns: map[net.Conn]struct{}{}}
	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()
	return s
}

// Get 按 ID 取会话。
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("4025 vnc session not found")
	}
	return s, nil
}

// Close 断开并移除会话, 会话不存在时什么也不做。
func (m *Manager) Close(id string) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if ok {
		s.close()
	}
}

// CloseAll 断开并移除全部会话。
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := m.sessions
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	for _, s := range all {
		s.close()
	}
}

// ForgetPasswords 清掉全部会话里的密码, 保险库锁定时调用。已连着的画面不受影响, 重新连接时在画面上重新输入。
// 已弹出的独立窗口进程里另有一份, 窗口关掉即随进程释放。
func (m *Manager) ForgetPasswords() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		s.mu.Lock()
		s.target.Password = ""
		s.mu.Unlock()
	}
}

// Running 返回正连着服务器的会话数, 退出确认时用。
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.sessions {
		if s.active() > 0 {
			n++
		}
	}
	return n
}
