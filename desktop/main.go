package main

import (
	"embed"
	"log"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/woodchen-ink/czlterm/desktop/internal/askpass"
	"github.com/woodchen-ink/czlterm/desktop/internal/gitsync"
	"github.com/woodchen-ink/czlterm/desktop/internal/mcpbridge"
	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
	"github.com/woodchen-ink/czlterm/desktop/internal/platform"
)

// all: 前缀是必须的: Next.js 导出产物里有 _next 目录, 默认 embed 规则会跳过下划线开头的目录。
//
//go:embed all:frontend/out
var assets embed.FS

func main() {
	// 被 git 当作 GIT_ASKPASS 调起, 回答同步仓库的用户名与令牌。见 internal/gitsync/auth.go。
	if gitsync.AskpassActive() {
		os.Exit(gitsync.RunAskpass(os.Args[1:]))
	}
	// 被 ssh 当作 SSH_ASKPASS 调起: ssh 以提示语为参数调用, 只能靠环境变量识别。见 internal/askpass。
	if askpass.Active() {
		os.Exit(askpass.Run(os.Args[1:]))
	}
	// `czlterm mcp` 作为 MCP stdio 桥运行, 不启动窗口。
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		os.Exit(mcpbridge.Run(version))
	}

	platform.ImportLoginPath()
	app := NewApp()

	// WebView2 默认把数据目录放在 %APPDATA% (Roaming), 改到安装根目录的 cache 下。
	webviewDir, err := paths.Cache("webview")
	if err != nil {
		log.Fatalf("1 resolve app dir: %v", err)
	}

	err = wails.Run(&options.App{
		Title:     "czlterm",
		Width:     1100,
		Height:    720,
		MinWidth:  760,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// 两个实例会各开一套 agent 与 MCP 端点并争写 mcp.json; 再次启动时唤出已有窗口。
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               paths.InstanceID(),
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.showWindow() },
		},
		Menu:       appMenu(app),
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		// 点 X 只隐藏到后台继续运行, 靠托盘图标 (macOS 为菜单栏图标) 或 Dock 唤回; 退出走托盘菜单 / ⌘Q。
		HideWindowOnClose: runtime.GOOS == "windows" || runtime.GOOS == "darwin",
		Bind:              []any{app},
		Windows: &windows.Options{
			WebviewUserDataPath: webviewDir,
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	})
	if err != nil {
		log.Fatalf("1 run application: %v", err)
	}
}
