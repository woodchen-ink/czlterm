// Package autostart 管理登录后自动启动。
//
// 开关状态以系统登记为准 (Windows 注册表 Run 键、macOS LaunchAgent), 不写进设置文件:
// 用户在任务管理器或系统设置里关掉后, 界面读到的也是关闭。
// 自启时命令行带 Flag, 程序据此隐藏窗口, 只留托盘 / 菜单栏图标。
package autostart

import (
	"errors"
	"os"
	"slices"

	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
)

// Flag 标记本次启动来自登录自启。
const Flag = "--autostart"

// Launched 报告当前进程是否由登录自启拉起。
func Launched() bool { return slices.Contains(os.Args[1:], Flag) }

// FromArgs 报告另一实例的启动参数是否来自登录自启, 用于单实例唤醒时不弹窗。
func FromArgs(args []string) bool { return slices.Contains(args, Flag) }

// Supported 报告当前系统是否支持自启; 无托盘的平台自启后无处唤回窗口, 不提供。
func Supported() bool { return supported }

// Enabled 报告自启是否已登记且指向当前程序。
func Enabled() (bool, error) {
	if !supported {
		return false, nil
	}
	return enabled()
}

// Set 登记或取消自启。
func Set(on bool) error {
	if !supported {
		return errors.New("8201 autostart is not supported on this system")
	}
	// 登记项不带环境变量, 调试实例自启后会落到正式版的数据目录。
	if on && os.Getenv(paths.InstanceEnv) != "" {
		return errors.New("8202 autostart is unavailable for dev instances")
	}
	return set(on)
}
