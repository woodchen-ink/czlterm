package main

import (
	"runtime"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// SetUILanguage 更新本应用定义的原生菜单文案；偏好由前端在本机保存，不进入同步数据。
// macOS 系统提供的应用 / 编辑 / 窗口菜单仍遵循系统语言。
func (a *App) SetUILanguage(language string) {
	a.uiEnglish.Store(language == "en")
	if a.ctx == nil {
		return
	}
	if runtime.GOOS == "darwin" {
		wruntime.MenuSetApplicationMenu(a.ctx, appMenu(a))
	}
	a.updateTrayLanguage()
}

// uiText 只用于程序定义的原生菜单，不处理用户提供的数据。
func (a *App) uiText(zh, en string) string {
	if a.uiEnglish.Load() {
		return en
	}
	return zh
}
