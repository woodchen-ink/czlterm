// Package term 管理内置终端会话: 在伪终端里运行 launch.Spec 描述的命令 (系统 ssh)。
// macOS / Linux 走 creack/pty, Windows 走 ConPTY, 经 aymanbagabas/go-pty 统一。
//
// 会话归桌面端进程所有, 不归页面: 页面断开只是停止观看, 命令照常运行, 重新接上时回放最近的输出。
// 命令退出后会话保留到用户关闭 (还能看最后的输出); 桌面端退出时全部结束 (CloseAll)。
package term

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

const (
	// replayBytes 每个会话保留的最近输出, 重新接上时回放。
	replayBytes = 512 << 10
	// MaxSessions 同时存在的会话数上限 (含已退出未关闭的)。
	MaxSessions = 32
	// subBuffer 每个观看者的待发队列 (块数); 页面跟不上时断开它, 它重连后从回放补齐。
	subBuffer = 512
)

var (
	ErrNotFound = errors.New("4060 terminal session not found")
	ErrTooMany  = errors.New("4061 too many terminal sessions, close some first")
)

// Spec 是要在伪终端里运行的命令。
type Spec struct {
	// ConnID 与 ScriptID 记录会话来自哪个连接 / 脚本, 界面据此重新连接。
	ConnID   string
	ScriptID string
	Title    string
	Program  string
	Args     []string
	// Env 覆盖或追加到本进程环境 (K=V)。
	Env []string
	// CleanupFiles 在命令退出或会话关闭后删除。
	CleanupFiles []string
}

// Info 是会话的对外描述。
type Info struct {
	ID        string `json:"id"`
	ConnID    string `json:"connId"`
	ScriptID  string `json:"scriptId"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"createdAt"`
	Exited    bool   `json:"exited"`
	ExitCode  int    `json:"exitCode"`
}

// Session 是一个终端会话。
type Session struct {
	pty     pty.Pty
	cmd     *pty.Cmd
	cleanup []string
	// cleanOnce 保证临时文件 (含短期令牌的代理 manifest) 只删一次: 进程退出后立即删, kill 超时也删。
	cleanOnce sync.Once

	// ptyMu 保护伪终端句柄: Write / Resize 持读锁, 关闭持写锁。
	// ConPTY 关闭后句柄不置空, 再 Resize 或重复 Close 会访问已释放的 HPCON。
	ptyMu     sync.RWMutex
	ptyClosed bool

	mu sync.Mutex
	// waited 在 cmd.Wait 返回后置位: 进程已回收, pid 可能被复用, 不能再按 pid 结束进程树。
	waited   bool
	info     Info
	buf      []byte // 最近输出, 超出 replayBytes 丢掉前面的
	subs     map[*Sub]struct{}
	exited   bool
	exitCode int
	done     chan struct{} // 命令退出且输出读完后关闭
	closing  atomic.Bool
}

// Sub 是一个观看者 (一条 WebSocket 连接)。
type Sub struct {
	// C 是实时输出; 被关闭表示观看者跟不上被断开, 或会话已结束。
	C chan []byte
}

// Manager 管理全部终端会话。
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	// onExit 在命令退出后调用, 用于通知界面。
	onExit func(Info)
}

// NewManager 创建 Manager。onExit 可为 nil。
func NewManager(onExit func(Info)) *Manager {
	return &Manager{sessions: map[string]*Session{}, onExit: onExit}
}

// Create 启动命令。cols / rows 为初始尺寸, ≤0 时用 80×24。
// 失败时由调用方清理 spec.CleanupFiles; 成功后归会话负责。
func (m *Manager) Create(spec Spec, cols, rows int) (*Session, error) {
	m.mu.Lock()
	n := len(m.sessions)
	m.mu.Unlock()
	if n >= MaxSessions {
		return nil, ErrTooMany
	}
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	// go-pty 在 Windows 上设了 Dir 后按 Dir 拼接相对程序名, 不查 PATH, 先解析成绝对路径。
	program, err := exec.LookPath(spec.Program)
	if err != nil {
		return nil, fmt.Errorf("4063 find %s: %w", spec.Program, err)
	}
	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("4062 open pseudo-terminal: %w", err)
	}
	if err := p.Resize(cols, rows); err != nil {
		p.Close()
		return nil, fmt.Errorf("4062 resize pseudo-terminal: %w", err)
	}
	cmd := p.Command(program, spec.Args...)
	cmd.Env = buildEnv(os.Environ(), spec.Env)
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	if err := cmd.Start(); err != nil {
		p.Close()
		return nil, fmt.Errorf("4063 start %s: %w", spec.Program, err)
	}
	s := &Session{
		pty: p, cmd: cmd, cleanup: spec.CleanupFiles,
		info: Info{ID: newID(), ConnID: spec.ConnID, ScriptID: spec.ScriptID, Title: spec.Title, CreatedAt: time.Now().UnixMilli()},
		subs: map[*Sub]struct{}{}, done: make(chan struct{}),
	}
	m.mu.Lock()
	m.sessions[s.info.ID] = s
	m.mu.Unlock()
	go func() {
		s.loop()
		if m.onExit != nil {
			m.onExit(s.Info())
		}
	}()
	return s, nil
}

// buildEnv 合并环境变量: extra 覆盖 base 中的同名项, 再补上终端类型。
// Windows 环境变量名不区分大小写。
func buildEnv(base, extra []string) []string {
	key := func(kv string) string {
		k, _, _ := strings.Cut(kv, "=")
		if runtime.GOOS == "windows" {
			return strings.ToUpper(k)
		}
		return k
	}
	add := append([]string{"TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=czlterm"}, extra...)
	override := map[string]bool{}
	for _, kv := range add {
		override[key(kv)] = true
	}
	out := make([]string, 0, len(base)+len(add))
	for _, kv := range base {
		if !override[key(kv)] {
			out = append(out, kv)
		}
	}
	out = append(out, add...)
	if runtime.GOOS != "windows" {
		out = fixLocale(out)
	}
	return out
}

// fixLocale 补齐 UTF-8 locale: macOS 从访达启动时没有 LANG, 而 LC_CTYPE 可能是不规范的 "UTF-8",
// ssh 经 SendEnv 带到 Linux 后每次登录都刷 setlocale 警告。
func fixLocale(env []string) []string {
	hasLang := false
	for i, kv := range env {
		switch {
		case kv == "LC_CTYPE=UTF-8":
			env[i] = "LC_CTYPE=en_US.UTF-8"
		case strings.HasPrefix(kv, "LANG="), strings.HasPrefix(kv, "LC_ALL="):
			hasLang = true
		}
	}
	if !hasLang {
		env = append(env, "LANG=en_US.UTF-8")
	}
	return env
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// loop 读输出直到读不出来, 然后等进程退出、清理临时文件、通知观看者。
func (s *Session) loop() {
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 32<<10)
		for {
			n, err := s.pty.Read(buf)
			if n > 0 {
				s.publish(append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				return
			}
		}
	}()
	err := s.cmd.Wait()
	s.mu.Lock()
	s.waited = true
	s.mu.Unlock()
	s.removeFiles()
	code := 0
	if st := s.cmd.ProcessState; st != nil {
		code = st.ExitCode()
	} else if err != nil {
		code = -1
	}
	// 进程退出后输出可能还没读完: 等读协程收尾, ConPTY 不会自己给 EOF, 读不完就关掉伪终端。
	select {
	case <-readDone:
	case <-time.After(300 * time.Millisecond):
	}
	s.closePty()
	<-readDone

	// 先关 done 再关订阅: 观看者看到订阅关闭时据 done 区分「命令退出」和「跟不上被断开」。
	s.mu.Lock()
	s.exited, s.exitCode = true, code
	close(s.done)
	for sub := range s.subs {
		close(sub.C)
		delete(s.subs, sub)
	}
	s.mu.Unlock()
}

func (s *Session) publish(chunk []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, chunk...)
	if over := len(s.buf) - replayBytes; over > 0 {
		// 从下一个换行处截, 避免回放从半个 UTF-8 字符或控制序列开始。
		if i := bytes.IndexByte(s.buf[over:], '\n'); i >= 0 && i < 4096 {
			over += i + 1
		}
		s.buf = append([]byte(nil), s.buf[over:]...)
	}
	for sub := range s.subs {
		select {
		case sub.C <- chunk:
		default:
			// 跟不上: 断开这个观看者, 它重连后从回放补齐, 不拖住命令。
			close(sub.C)
			delete(s.subs, sub)
		}
	}
}

// Attach 接上会话: 返回最近输出的快照与实时输出订阅, 两者在同一把锁内取, 中间不丢字节。
// 会话已退出时 sub 为 nil, 只回快照。
func (s *Session) Attach() (replay []byte, sub *Sub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	replay = append([]byte(nil), s.buf...)
	if s.exited {
		return replay, nil
	}
	sub = &Sub{C: make(chan []byte, subBuffer)}
	s.subs[sub] = struct{}{}
	return replay, sub
}

// Detach 断开观看者。
func (s *Session) Detach(sub *Sub) {
	if sub == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[sub]; ok {
		delete(s.subs, sub)
		close(sub.C)
	}
}

// Write 写入键盘输入。
func (s *Session) Write(b []byte) error {
	s.ptyMu.RLock()
	defer s.ptyMu.RUnlock()
	if s.ptyClosed {
		return nil
	}
	_, err := s.pty.Write(b)
	return err
}

// Resize 调整窗口尺寸, 多个观看者时以最后一次为准。
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 || cols > 1000 || rows > 1000 {
		return nil
	}
	s.ptyMu.RLock()
	defer s.ptyMu.RUnlock()
	if s.ptyClosed {
		return nil
	}
	return s.pty.Resize(cols, rows)
}

// closePty 关闭伪终端, 只关一次。
func (s *Session) closePty() {
	s.ptyMu.Lock()
	defer s.ptyMu.Unlock()
	if !s.ptyClosed {
		s.ptyClosed = true
		_ = s.pty.Close()
	}
}

// removeFiles 删除临时文件, 只删一次。
func (s *Session) removeFiles() {
	s.cleanOnce.Do(func() {
		for _, path := range s.cleanup {
			_ = os.Remove(path)
		}
	})
}

// Done 在命令退出后关闭。
func (s *Session) Done() <-chan struct{} { return s.done }

// Exited 报告命令是否已退出。
func (s *Session) Exited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

// Info 返回会话描述。
func (s *Session) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := s.info
	info.Exited, info.ExitCode = s.exited, s.exitCode
	return info
}

// kill 结束命令及其子进程, 最多等 3 秒收尾。
func (s *Session) kill() {
	if !s.closing.CompareAndSwap(false, true) {
		<-s.done
		return
	}
	s.mu.Lock()
	waited := s.waited
	s.mu.Unlock()
	if !waited && s.cmd.Process != nil {
		killTree(s.cmd.Process.Pid)
		_ = s.cmd.Process.Kill()
	}
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
		s.closePty()
	}
	// 收尾超时时程序可能紧接着退出, 不等 loop 删临时文件。
	s.removeFiles()
}

// Get 按 id 取会话。
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

// List 返回全部会话, 按创建先后。
func (m *Manager) List() []Info {
	m.mu.Lock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.Unlock()
	out := make([]Info, 0, len(list))
	for _, s := range list {
		out = append(out, s.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

// Running 返回仍在运行的会话数。
func (m *Manager) Running() int {
	n := 0
	for _, info := range m.List() {
		if !info.Exited {
			n++
		}
	}
	return n
}

// Rename 修改会话标题。
func (m *Manager) Rename(id, title string) error {
	s, err := m.Get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.info.Title = title
	s.mu.Unlock()
	return nil
}

// Close 结束并移除会话。
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	s.kill()
	return nil
}

// CloseAll 结束全部会话, 并发收尾, 最多等到 ctx 结束。
func (m *Manager) CloseAll(ctx context.Context) {
	m.mu.Lock()
	all := m.sessions
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range all {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			s.kill()
		}(s)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
