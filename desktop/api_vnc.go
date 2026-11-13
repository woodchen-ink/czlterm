package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/woodchen-ink/czlterm/desktop/internal/launch"
	"github.com/woodchen-ink/czlterm/desktop/internal/vnc"
)

// vncState 是内置 VNC 的会话与弹出的独立窗口。
//
// 独立窗口是另起的 czlterm 子进程 (Wails v2 只有一个窗口), 参数经 stdin 传入且 stdin 一直开着:
// 主程序退出时管道关闭, 子进程随之退出。子进程退出 (用户关窗口) 时结束对应会话。
type vncState struct {
	once    sync.Once
	mgr     *vnc.Manager
	mu      sync.Mutex
	windows map[string]io.WriteCloser
}

func (a *App) vncs() *vnc.Manager {
	a.vnc.once.Do(func() {
		a.vnc.mgr = vnc.NewManager()
		a.vnc.windows = map[string]io.WriteCloser{}
	})
	return a.vnc.mgr
}

// openVNC 记下一个内置 VNC 会话, 页面随后经 /vnc WebSocket 连接。
func (a *App) openVNC(t vnc.Target) (launch.Result, error) {
	if _, err := a.startTerminalServer(); err != nil {
		return launch.Result{}, err
	}
	s := a.vncs().Create(t)
	a.log.Info("vnc session", "conn", t.ConnID)
	return launch.Result{VNCID: s.Info().ID}, nil
}

// GetVNCSession 返回会话标题与凭据, 页面里的 noVNC 用它完成认证。
func (a *App) GetVNCSession(id string) (vnc.Info, error) {
	s, err := a.vncs().Get(id)
	if err != nil {
		return vnc.Info{}, err
	}
	return s.Info(), nil
}

// SetVNCCredentials 记下用户在画面上手动输入并认证成功的凭据, 弹出独立窗口时一并带过去。
func (a *App) SetVNCCredentials(id, username, password string) error {
	s, err := a.vncs().Get(id)
	if err != nil {
		return err
	}
	s.SetCredentials(username, password)
	return nil
}

// CloseVNC 断开并移除会话。
func (a *App) CloseVNC(id string) {
	a.vncs().Close(id)
}

// PopOutVNC 把会话移到独立窗口。成功后主窗口不再显示这个会话, 关掉独立窗口即结束会话。
func (a *App) PopOutVNC(id string) error {
	s, err := a.vncs().Get(id)
	if err != nil {
		return err
	}
	ep, err := a.startTerminalServer()
	if err != nil {
		return err
	}
	a.vnc.mu.Lock()
	_, open := a.vnc.windows[id]
	a.vnc.mu.Unlock()
	if open {
		return fmt.Errorf("4027 vnc window already open")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("4041 locate czlterm executable: %w", err)
	}
	lang := "zh-CN"
	if a.uiEnglish.Load() {
		lang = "en"
	}
	cmd := exec.Command(exe, vncWindowArg)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("4026 start vnc window: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("4026 start vnc window: %w", err)
	}
	params := VNCWindowParams{Endpoint: ep.URL, Token: ep.Token, Session: s.Info(), Language: lang}
	if err := json.NewEncoder(stdin).Encode(params); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("4026 start vnc window: %w", err)
	}
	a.vnc.mu.Lock()
	a.vnc.windows[id] = stdin
	a.vnc.mu.Unlock()
	a.log.Info("vnc window opened", "session", id)

	go func() {
		err := cmd.Wait()
		a.log.Info("vnc window closed", "session", id, "err", err)
		a.vnc.mu.Lock()
		delete(a.vnc.windows, id)
		a.vnc.mu.Unlock()
		a.vncs().Close(id)
	}()
	return nil
}

// stopVNC 关闭全部独立窗口与会话, 退出程序时调用。
func (a *App) stopVNC() {
	a.vncs()
	a.vnc.mu.Lock()
	for _, w := range a.vnc.windows {
		_ = w.Close()
	}
	a.vnc.mu.Unlock()
	a.vncs().CloseAll()
}

// serveVNC 把页面的 WebSocket 接到会话的 VNC 服务器。
func (a *App) serveVNC(w http.ResponseWriter, r *http.Request, token string) {
	q := r.URL.Query()
	if !validToken(q.Get("token"), token) {
		http.Error(w, "4066 invalid terminal token", http.StatusUnauthorized)
		return
	}
	s, err := a.vncs().Get(q.Get("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	up := websocket.Upgrader{
		ReadBufferSize:  64 << 10,
		WriteBufferSize: 64 << 10,
		CheckOrigin:     func(r *http.Request) bool { return trustedOrigin(r.Header.Get("Origin")) },
	}
	ws, err := up.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade 已写好错误响应
	}
	defer ws.Close()
	// RFB 客户端消息都很小, 剪贴板粘贴是最大的一种。
	ws.SetReadLimit(4 << 20)
	s.Serve(ws)
}
