package main

// version 由构建脚本经 -ldflags "-X main.version=..." 注入。
var version = "v0.0.0-dev"

// Version 返回当前版本, 供界面显示。
func (a *App) Version() string { return version }
