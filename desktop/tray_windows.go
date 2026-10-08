//go:build windows

package main

import (
	_ "embed"
	"runtime"

	"github.com/energye/systray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 系统托盘。关闭窗口只是隐藏(见 main.go 的 HideWindowOnClose), 后台要常驻
// (MCP、保险库、同步); 托盘是用户找回窗口与真正退出的入口。

//go:embed build/windows/icon.ico
var trayIcon []byte

func (a *App) startTray() {
	go func() {
		// 托盘窗口的创建与消息循环必须在同一个系统线程上。
		runtime.LockOSThread()
		systray.Run(a.onTrayReady, nil)
	}()
}

func (a *App) onTrayReady() {
	systray.SetIcon(trayIcon)
	systray.SetTooltip("czlterm")
	systray.SetOnClick(func(systray.IMenu) { a.showWindow() })
	systray.SetOnRClick(func(menu systray.IMenu) { _ = menu.ShowMenu() })

	systray.AddMenuItem("打开 czlterm", "").Click(a.showWindow)
	systray.AddSeparator()
	systray.AddMenuItem("退出", "").Click(func() {
		if a.ctx != nil {
			wruntime.Quit(a.ctx)
		}
	})
}

func (a *App) stopTray() {
	systray.Quit()
}

// showWindow 恢复被隐藏或最小化的窗口并置前。
func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	wruntime.WindowShow(a.ctx)
	wruntime.WindowUnminimise(a.ctx)
}
