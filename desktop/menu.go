package main

import (
	"runtime"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
)

// appMenu 在 macOS 上提供应用菜单与编辑菜单: 没有编辑菜单时 WebView 里的 ⌘C / ⌘V 不生效。
// 其他平台返回 nil。
//
// 点 X 只最小化 (见 App.beforeClose), 所以退出必须走自己的菜单项: 默认应用菜单的「退出」
// 与点 X 在 Wails 里是同一个信号, 区分不了。
func appMenu(app *App) *menu.Menu {
	if runtime.GOOS != "darwin" {
		return nil
	}
	m := menu.NewMenu()
	m.Append(menu.SubMenu("czlterm", menu.NewMenuFromItems(
		menu.Text("退出 czlterm", keys.CmdOrCtrl("q"), func(*menu.CallbackData) { app.quit() }),
	)))
	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())
	m.Append(menu.SubMenu("文件", menu.NewMenuFromItems(
		menu.Text("关闭窗口", keys.CmdOrCtrl("w"), func(*menu.CallbackData) { app.minimise() }),
	)))
	return m
}
