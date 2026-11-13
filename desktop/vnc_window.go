package main

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
	"github.com/woodchen-ink/czlterm/desktop/internal/vnc"
)

// vncWindowArg 让 czlterm 以独立 VNC 窗口模式运行, 由主程序的 PopOutVNC 调起。
const vncWindowArg = "vnc-window"

// VNCWindowParams 是主程序经 stdin 交给独立窗口的参数。
type VNCWindowParams struct {
	// Endpoint 与 Token 是主程序的本机 WebSocket 根地址与令牌, 画面经主程序转发。
	Endpoint string   `json:"endpoint"`
	Token    string   `json:"token"`
	Session  vnc.Info `json:"session"`
	// Language 是主窗口当前的界面语言: 独立窗口的 WebView 数据目录不同, 读不到主窗口保存的偏好。
	Language string `json:"language"`
}

// VNCWindow 绑定给独立 VNC 窗口的页面。
//
// 主窗口也绑定一个空的, 只为生成前端绑定; 页面按有没有 App 绑定区分主窗口与独立窗口。
type VNCWindow struct {
	params VNCWindowParams
}

// Params 返回窗口参数, 主窗口里为空。
func (w *VNCWindow) Params() VNCWindowParams {
	return w.params
}

// runVNCWindow 运行独立 VNC 窗口: 不加单实例锁、不开托盘与 MCP, 只显示一个会话。
// stdin 关闭 (主程序退出) 时退出。
func runVNCWindow() int {
	log := newLogger()
	var p VNCWindowParams
	if err := json.NewDecoder(os.Stdin).Decode(&p); err != nil {
		log.Error("vnc window params", "err", err)
		return 1
	}
	// 与主窗口分开: 两个进程同时用一个 WebView2 数据目录要求创建参数完全一致, 不值得冒险。
	webviewDir, err := paths.Cache("webview-vnc")
	if err != nil {
		log.Error("vnc window webview dir", "err", err)
		return 1
	}
	w := &VNCWindow{params: p}
	err = wails.Run(&options.App{
		Title:            p.Session.Title,
		Width:            1280,
		Height:           800,
		MinWidth:         480,
		MinHeight:        320,
		BackgroundColour: &options.RGBA{R: 15, G: 17, B: 21, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup: func(ctx context.Context) {
			go func() {
				_, _ = io.Copy(io.Discard, os.Stdin)
				wruntime.Quit(ctx)
			}()
		},
		Bind: []any{w},
		Windows: &windows.Options{
			WebviewUserDataPath: webviewDir,
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	})
	if err != nil {
		log.Error("vnc window", "err", err)
		return 1
	}
	return 0
}
