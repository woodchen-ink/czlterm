//go:build !windows && !darwin

package autostart

// 其它平台没有托盘, 隐藏启动后无处唤回窗口, 不提供自启。
const supported = false

func enabled() (bool, error) { return false, nil }

func set(bool) error { return nil }
