package main

import (
	"runtime"

	"github.com/wailsapp/wails/v2/pkg/menu"
)

// appMenu 在 macOS 上提供应用菜单与编辑菜单: 没有编辑菜单时 WebView 里的 ⌘C / ⌘V 不生效。
// 其他平台返回 nil。
func appMenu() *menu.Menu {
	if runtime.GOOS != "darwin" {
		return nil
	}
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())
	return m
}
