//go:build darwin

package autostart

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const supported = true

// label 是 LaunchAgent 标识, 与 bundle id 区分开, 便于在「登录项」里辨认。
const label = "net.czl.term.autostart"

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("8203 locate home dir: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

// executable 返回 .app 内的主程序路径。从 DMG 或隔离副本运行时路径会失效, 不允许登记。
func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("8203 locate executable: %w", err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", fmt.Errorf("8203 locate executable: %w", err)
	}
	if strings.Contains(exe, "/AppTranslocation/") || strings.HasPrefix(exe, "/Volumes/") {
		return "", errors.New("8206 move czlterm to Applications before enabling autostart")
	}
	return exe, nil
}

// plist 生成 LaunchAgent 内容; 只在登录时运行一次, 退出后不由 launchd 拉起。
func plist(exe string) []byte {
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + label + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + esc(exe) + `</string>
		<string>` + Flag + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>LimitLoadToSessionType</key>
	<string>Aqua</string>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`)
}

func enabled() (bool, error) {
	path, err := plistPath()
	if err != nil {
		return false, err
	}
	got, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("8204 read autostart: %w", err)
	}
	exe, err := executable()
	if err != nil {
		// 当前副本不能登记时, 已有登记项也不是指向它的。
		return false, nil
	}
	// 指向别处 (旧安装路径) 的登记项不算开启, 用户重新打开开关即可修正。
	return bytes.Equal(got, plist(exe)), nil
}

// set 只写文件, 不调用 launchctl load: 立即加载会马上再拉起一个实例, 下次登录时 launchd 自会读取。
func set(on bool) error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("8205 write autostart: %w", err)
		}
		return nil
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("8205 write autostart: %w", err)
	}
	if err := os.WriteFile(path, plist(exe), 0o644); err != nil {
		return fmt.Errorf("8205 write autostart: %w", err)
	}
	return nil
}
