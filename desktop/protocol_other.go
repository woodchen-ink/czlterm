//go:build !windows

package main

// registerProtocol 在 Windows 以外由打包配置登记协议 (macOS: wails.json 的 info.protocols 生成 Info.plist)。
func registerProtocol(string) error { return nil }
