//go:build windows

package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"

	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
)

const supported = true

const (
	runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	// approvedKey 记录任务管理器「启动应用」里的禁用状态, 首字节为奇数表示已禁用。
	approvedKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

// command 是写进 Run 键的命令行; 安装目录可能含空格, 路径需要加引号。
func command() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("8203 locate executable: %w", err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", fmt.Errorf("8203 locate executable: %w", err)
	}
	return `"` + exe + `" ` + Flag, nil
}

func enabled() (bool, error) {
	want, err := command()
	if err != nil {
		return false, err
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("8204 read autostart: %w", err)
	}
	defer k.Close()
	got, _, err := k.GetStringValue(paths.Product)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("8204 read autostart: %w", err)
	}
	// 指向别处 (旧安装路径) 的登记项不算开启, 用户重新打开开关即可修正。
	return got == want && !disabledInTaskManager(), nil
}

func disabledInTaskManager() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetBinaryValue(paths.Product)
	return err == nil && len(v) > 0 && v[0]&1 == 1
}

func set(on bool) error {
	// 无论开关方向都清掉任务管理器的禁用标记: 开启时让登记项真正生效, 关闭时不留残项。
	if k, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue(paths.Product)
		k.Close()
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("8205 write autostart: %w", err)
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(paths.Product); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("8205 write autostart: %w", err)
		}
		return nil
	}
	cmd, err := command()
	if err != nil {
		return err
	}
	if err := k.SetStringValue(paths.Product, cmd); err != nil {
		return fmt.Errorf("8205 write autostart: %w", err)
	}
	return nil
}
