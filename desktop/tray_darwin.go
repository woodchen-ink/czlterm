//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>

void czlTrayStart(const void *png1x, int length1x, const void *png2x, int length2x);
void czlTrayStop(void);
void czlTrayLanguage(int english);
void czlActivateApp(void);
*/
import "C"

import (
	_ "embed"
	"sync/atomic"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// macOS 菜单栏图标(NSStatusItem), 实现在 tray_darwin.m。
//
// 不用 energye/systray: 它要自己跑 NSApplication 主循环, 与 Wails 冲突。
// 这里只往 Wails 已经在跑的主线程上挂一个状态栏项, 另外补上 Dock 图标点击时恢复窗口 ——
// Wails 的 AppDelegate 没有实现 applicationShouldHandleReopen, 窗口隐藏后点 Dock 不会出来。

// 菜单栏用 template 单色图标 (纯黑 + alpha, 系统按菜单栏主题着色), 18pt 的 1x / 2x 两份。
// 由 docs/design/brand/build-icons.js 生成。

//go:embed build/tray/template.png
var trayTemplate1x []byte

//go:embed build/tray/template@2x.png
var trayTemplate2x []byte

var trayApp atomic.Pointer[App]

func (a *App) startTray() {
	trayApp.Store(a)
	// C 侧拷贝后释放。
	C.czlTrayStart(C.CBytes(trayTemplate1x), C.int(len(trayTemplate1x)),
		C.CBytes(trayTemplate2x), C.int(len(trayTemplate2x)))
	a.updateTrayLanguage()
}

// updateTrayLanguage 在 AppKit 主线程刷新托盘菜单。
func (a *App) updateTrayLanguage() {
	var english C.int
	if a.uiEnglish.Load() {
		english = 1
	}
	C.czlTrayLanguage(english)
}

func (a *App) stopTray() {
	C.czlTrayStop()
}

// showWindow 恢复被隐藏或最小化的窗口并把程序切到前台。
func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	wruntime.WindowShow(a.ctx)
	wruntime.WindowUnminimise(a.ctx)
	C.czlActivateApp()
}

// 以下由 Objective-C 在主线程回调。Wails 的窗口操作本身会派发回主线程,
// 放到协程里执行, 不在主线程的回调栈里调用它们。

//export czlTrayOpen
func czlTrayOpen() {
	if a := trayApp.Load(); a != nil {
		go a.showWindow()
	}
}

//export czlTrayQuit
func czlTrayQuit() {
	if a := trayApp.Load(); a != nil && a.ctx != nil {
		go wruntime.Quit(a.ctx)
	}
}
