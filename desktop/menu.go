package main

import (
	"runtime"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// appMenu 在 macOS 上提供应用菜单与编辑菜单: 没有编辑菜单时 WebView 里的 ⌘C / ⌘V 不生效。
// 其他平台返回 nil。
func appMenu(app *App) *menu.Menu {
	if runtime.GOOS != "darwin" {
		return nil
	}
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())
	// ⌘W 与点 X 一致: 隐藏到后台, 点 Dock 图标唤回; ⌘Q 由应用菜单提供, 真正退出。
	m.Append(menu.SubMenu("文件", menu.NewMenuFromItems(
		menu.Text("关闭窗口", keys.CmdOrCtrl("w"), func(*menu.CallbackData) {
			if app.ctx != nil {
				wruntime.Hide(app.ctx)
			}
		}),
	)))
	return m
}
