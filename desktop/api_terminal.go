package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/term"
)

// eventTerminalExit 在内置终端的命令退出后推送, 数据为 term.Info。
const eventTerminalExit = "terminal:exit"

// terminalState 是内置终端的会话与本机 WebSocket 端点。
//
// 终端与 VNC 画面数据量大且频繁, 不走 Wails 事件 (要经 JSON 编码), 走 127.0.0.1 随机端口的 WebSocket,
// 用本次运行生成的令牌鉴权。端点与 MCP 开关无关, 第一次打开内置终端或内置 VNC 时启动;
// 终端在 /terminal, VNC 在 /vnc (见 api_vnc.go)。
type terminalState struct {
	once     sync.Once
	mgr      *term.Manager
	mu       sync.Mutex
	server   *http.Server
	endpoint TerminalEndpoint
}

// TerminalEndpoint 是前端连接本机 WebSocket 用的根地址 (ws://127.0.0.1:端口) 与令牌。
type TerminalEndpoint struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func (a *App) terms() *term.Manager {
	a.terminal.once.Do(func() {
		a.terminal.mgr = term.NewManager(func(info term.Info) {
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, eventTerminalExit, info)
			}
		})
	})
	return a.terminal.mgr
}

// startTerminalServer 启动终端 WebSocket 端点, 已在运行时直接返回。
func (a *App) startTerminalServer() (TerminalEndpoint, error) {
	a.terminal.mu.Lock()
	defer a.terminal.mu.Unlock()
	if a.terminal.server != nil {
		return a.terminal.endpoint, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return TerminalEndpoint{}, fmt.Errorf("4064 listen terminal: %w", err)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		ln.Close()
		return TerminalEndpoint{}, fmt.Errorf("4064 generate terminal token: %w", err)
	}
	token := hex.EncodeToString(b)
	mux := http.NewServeMux()
	mux.HandleFunc("/terminal", func(w http.ResponseWriter, r *http.Request) { a.serveTerminal(w, r, token) })
	mux.HandleFunc("/vnc", func(w http.ResponseWriter, r *http.Request) { a.serveVNC(w, r, token) })
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.log.Error("terminal server", "err", err)
		}
	}()
	a.terminal.server = srv
	a.terminal.endpoint = TerminalEndpoint{URL: "ws://" + ln.Addr().String(), Token: token}
	return a.terminal.endpoint, nil
}

// stopTerminals 结束全部终端与 VNC 会话 (含独立窗口) 并关闭端点, 退出程序时调用。
func (a *App) stopTerminals() {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	a.terms().CloseAll(ctx)
	a.stopVNC()
	a.terminal.mu.Lock()
	defer a.terminal.mu.Unlock()
	if a.terminal.server != nil {
		_ = a.terminal.server.Close()
		a.terminal.server = nil
	}
}

// GetTerminalEndpoint 返回本机 WebSocket 根地址与令牌, 未启动时先启动。
func (a *App) GetTerminalEndpoint() (TerminalEndpoint, error) {
	return a.startTerminalServer()
}

// ListTerminals 列出内置终端会话, 界面重新挂载时据此恢复标签。
func (a *App) ListTerminals() []term.Info {
	return a.terms().List()
}

// CloseTerminal 结束并移除会话。
func (a *App) CloseTerminal(id string) error {
	return a.terms().Close(id)
}

// RenameTerminal 修改会话标题。
func (a *App) RenameTerminal(id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("4065 terminal title is empty")
	}
	return a.terms().Rename(id, title)
}

// trustedOrigin 只接受应用自己的页面: 打包后的 Wails 页面, 以及开发模式下的本机地址。
// 主要防线是令牌, Origin 检查挡住浏览器里其他网页猜端口的尝试。
func trustedOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "wails":
		return true
	case "http", "https":
		h := u.Hostname()
		return h == "wails.localhost" || h == "localhost" || h == "127.0.0.1"
	}
	return false
}

func validToken(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// terminalMsg 是页面发来的控制消息。键盘输入走二进制帧。
type terminalMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// serveTerminal 把一个会话接到 WebSocket: 先回放最近输出, 再转发实时输出; 页面输入写回伪终端。
// 命令退出时发 {"type":"exit","code":N} 后关闭连接。
func (a *App) serveTerminal(w http.ResponseWriter, r *http.Request, token string) {
	q := r.URL.Query()
	if !validToken(q.Get("token"), token) {
		http.Error(w, "4066 invalid terminal token", http.StatusUnauthorized)
		return
	}
	s, err := a.terms().Get(q.Get("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	up := websocket.Upgrader{
		ReadBufferSize:  32 << 10,
		WriteBufferSize: 32 << 10,
		CheckOrigin:     func(r *http.Request) bool { return trustedOrigin(r.Header.Get("Origin")) },
	}
	ws, err := up.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade 已写好错误响应
	}
	defer ws.Close()
	ws.SetReadLimit(1 << 20)

	replay, sub := s.Attach()
	defer s.Detach(sub)

	// gorilla 的连接同一时刻只允许一个写者, 全部输出由这个协程写。
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		// 任何一次写失败都关掉连接, 读循环随之退出, 页面收到 onclose 后重连。
		defer ws.Close()
		write := func(mt int, b []byte) bool {
			_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			return ws.WriteMessage(mt, b) == nil
		}
		if len(replay) > 0 && !write(websocket.BinaryMessage, replay) {
			return
		}
		if sub != nil {
			for chunk := range sub.C {
				if !write(websocket.BinaryMessage, chunk) {
					return
				}
			}
		}
		select {
		case <-s.Done():
			msg, _ := json.Marshal(map[string]any{"type": "exit", "code": s.Info().ExitCode})
			write(websocket.TextMessage, msg)
		default:
			// 订阅因跟不上被断开: 关掉连接让页面重连, 从回放补齐。
		}
		_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		_ = ws.Close()
	}()

	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			break
		}
		if mt == websocket.BinaryMessage {
			_ = s.Write(data)
			continue
		}
		var msg terminalMsg
		if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" {
			_ = s.Resize(msg.Cols, msg.Rows)
		}
	}
	s.Detach(sub)
	<-writeDone
}

// beforeClose 在退出前确认是否结束仍在运行的内置终端与 VNC 会话, 返回 true 取消退出。
// 关窗口只隐藏到托盘, 不经过这里; 自动更新已经启动安装程序, 不再询问。
func (a *App) beforeClose(ctx context.Context) bool {
	if a.forceQuit.Load() {
		return false
	}
	n := a.terms().Running() + a.vncs().Running()
	if n == 0 {
		return false
	}
	a.showWindow()
	opts := wruntime.MessageDialogOptions{
		Type:          wruntime.QuestionDialog,
		Title:         a.uiText("退出 CZLTerm", "Quit CZLTerm"),
		Message:       fmt.Sprintf(a.uiText("还有 %d 个连接在运行, 退出会断开它们。确定退出吗?", "%d connections are still running. Quitting will disconnect them. Quit anyway?"), n),
		Buttons:       []string{a.uiText("退出", "Quit"), a.uiText("取消", "Cancel")},
		DefaultButton: a.uiText("取消", "Cancel"),
		CancelButton:  a.uiText("取消", "Cancel"),
	}
	// Windows 忽略自定义按钮, 显示「是 / 否」, 只有 DefaultButton 为 "No" 时默认按钮才是「否」。
	if runtime.GOOS == "windows" {
		opts.DefaultButton = "No"
	}
	answer, err := wruntime.MessageDialog(ctx, opts)
	if err != nil {
		a.log.Warn("quit confirm", "err", err)
		return false
	}
	// Windows 的 MessageBox 返回按钮类型 (Yes / No), 不是按钮文字。
	return answer != a.uiText("退出", "Quit") && answer != "Yes" && answer != "Ok"
}
