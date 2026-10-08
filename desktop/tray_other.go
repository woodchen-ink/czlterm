//go:build !windows && !darwin

package main

import wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

// 其它平台无托盘, 点 X 仍直接关闭 (见 main.go 的 hideOnClose 判断)。
func (a *App) startTray() {}

func (a *App) stopTray() {}

func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	wruntime.WindowShow(a.ctx)
	wruntime.WindowUnminimise(a.ctx)
}
