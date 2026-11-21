//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"

	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
)

// registerProtocol 把 czlterm:// 登记到当前用户 (HKCU), 指向本程序自身。
//
// 每次启动核对一遍: 开发版、手动挪过目录、旧版残留的
// 登记指向了不存在的程序, 都会让浏览器唤不起客户端, 登录永远等不到回调。
// 命令必须直接是本 exe: 经 cmd.exe 之类包装时浏览器会拒绝或弹出危险提示。
func registerProtocol(scheme string) error {
	// 开发调试用的独立实例不抢正式版的登记, 否则装好的版本收不到登录回调。
	if os.Getenv(paths.InstanceEnv) != "" {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	want := fmt.Sprintf(`"%s" "%%1"`, exe)

	base := `Software\Classes\` + scheme
	if cur, err := readDefault(base + `\shell\open\command`); err == nil && strings.EqualFold(cur, want) {
		return nil
	}

	values := []struct{ path, name, value string }{
		{base, "", "czlterm link"},
		{base, "URL Protocol", ""},
		{base + `\DefaultIcon`, "", fmt.Sprintf(`"%s",0`, exe)},
		{base + `\shell\open\command`, "", want},
	}
	for _, v := range values {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, v.path, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("create %s: %w", v.path, err)
		}
		err = k.SetStringValue(v.name, v.value)
		k.Close()
		if err != nil {
			return fmt.Errorf("set %s: %w", v.path, err)
		}
	}
	return nil
}

func readDefault(path string) (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue("")
	return v, err
}
