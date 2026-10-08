package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/woodchen-ink/czlterm/desktop/internal/mcpbridge"
	"github.com/woodchen-ink/czlterm/desktop/internal/settings"
)

// 内置 MCP: 让同一台机器上的 AI 客户端列出连接、在服务器上读写文件与执行命令。
//
// 桌面端在 127.0.0.1 上开 Streamable HTTP 端点, 以随机令牌鉴权; 连接信息写进
// config/mcp.json (仅当前用户可读)。只支持 stdio 的客户端通过 `czlterm mcp` 桥接。
//
// 权限分三层, 全部满足才放行: 设置里开启 MCP; 连接本身开了「允许 AI 访问」;
// 写文件与执行命令另需设置里的两个开关。凭据始终不出桌面端进程。

const (
	mcpDefaultPort = 47840
	mcpPath        = "/mcp"
)

type mcpState struct {
	mu     sync.Mutex
	server *http.Server
	info   mcpbridge.Info
}

// startMCP 启动本地 MCP 端点。已在运行时不重复启动。
func (a *App) startMCP() error {
	a.mcp.mu.Lock()
	defer a.mcp.mu.Unlock()
	if a.mcp.server != nil {
		return nil
	}
	token, err := a.mcpToken()
	if err != nil {
		return err
	}

	// 固定端口便于客户端配置; 被占用时退回随机端口, 连接信息以 mcp.json 为准。
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", mcpDefaultPort))
	if err != nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return fmt.Errorf("7001 listen mcp: %w", err)
		}
	}

	server := a.newMCPServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux := http.NewServeMux()
	mux.Handle(mcpPath, requireToken(token, handler))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.log.Error("mcp server", "err", err)
		}
	}()

	info := mcpbridge.Info{URL: fmt.Sprintf("http://%s%s", ln.Addr().String(), mcpPath), Token: token}
	if path, err := mcpbridge.InfoPath(); err == nil {
		data, _ := json.MarshalIndent(info, "", "  ")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			a.log.Warn("write mcp.json", "err", err)
		}
	}
	a.mcp.server = srv
	a.mcp.info = info
	a.log.Info("mcp listening", "url", info.URL)
	return nil
}

func (a *App) stopMCP() {
	a.mcp.mu.Lock()
	defer a.mcp.mu.Unlock()
	if a.mcp.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Shutdown 不会结束仍在推送的 SSE 流, 超时后强制关闭。
	if err := a.mcp.server.Shutdown(ctx); err != nil {
		_ = a.mcp.server.Close()
	}
	a.mcp.server = nil
	a.mcp.info = mcpbridge.Info{}
	if path, err := mcpbridge.InfoPath(); err == nil {
		os.Remove(path)
	}
}

// mcpToken 读取或生成令牌。令牌跨重启不变, 配置过的客户端不必重配。
func (a *App) mcpToken() (string, error) {
	if tok := a.settings.Get().MCPToken; len(tok) >= 32 {
		return tok, nil
	}
	s, err := a.settings.Update(func(s *settings.Settings) { s.MCPToken = settings.NewToken() })
	return s.MCPToken, err
}

// MCPConfig 是设置页展示的客户端配置。
type MCPConfig struct {
	Running bool   `json:"running"`
	URL     string `json:"url"`
	// StdioJSON 是 Claude Desktop 等 stdio 客户端的配置片段; HTTPJSON 是支持 HTTP 的客户端 (Claude Code 等) 的配置片段, 含令牌。
	StdioJSON string `json:"stdioJson"`
	HTTPJSON  string `json:"httpJson"`
}

// GetMCPConfig 返回客户端配置片段。
func (a *App) GetMCPConfig() (MCPConfig, error) {
	a.mcp.mu.Lock()
	info := a.mcp.info
	running := a.mcp.server != nil
	a.mcp.mu.Unlock()

	exe, err := os.Executable()
	if err != nil {
		return MCPConfig{}, err
	}
	stdio, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{
		"czlterm": map[string]any{"command": exe, "args": []string{"mcp"}},
	}}, "", "  ")
	out := MCPConfig{Running: running, URL: info.URL, StdioJSON: string(stdio)}
	if running {
		h, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{
			"czlterm": map[string]any{"type": "http", "url": info.URL, "headers": map[string]string{"Authorization": "Bearer " + info.Token}},
		}}, "", "  ")
		out.HTTPJSON = string(h)
	}
	return out, nil
}

// RotateMCPToken 换新令牌并重启端点, 已配置的 HTTP 客户端需要更新配置。
func (a *App) RotateMCPToken() (MCPConfig, error) {
	if _, err := a.settings.Update(func(s *settings.Settings) { s.MCPToken = settings.NewToken() }); err != nil {
		return MCPConfig{}, err
	}
	if a.settings.Get().MCPEnabled {
		a.stopMCP()
		if err := a.startMCP(); err != nil {
			return MCPConfig{}, err
		}
	}
	return a.GetMCPConfig()
}

func requireToken(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 浏览器页面可以向 127.0.0.1 发请求; 带 Origin 的一律拒绝, 本地 AI 客户端不会带。
		if r.Header.Get("Origin") != "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
