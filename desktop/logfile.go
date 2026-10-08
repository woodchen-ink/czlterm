package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
)

// maxLogBytes 超过后启动时把日志轮转为 .old, 只保留一份旧日志。
const maxLogBytes = 5 << 20

// newLogger 同时写 stderr 与 logs/czlterm.log。日志里不记录密码、私钥与命令输出。
func newLogger() *slog.Logger {
	var out io.Writer = os.Stderr
	if dir, err := paths.Logs(); err == nil {
		path := filepath.Join(dir, "czlterm.log")
		if info, err := os.Stat(path); err == nil && info.Size() > maxLogBytes {
			_ = os.Rename(path, path+".old")
		}
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			// 文件在前: MultiWriter 遇到第一个错误就停, GUI 程序没有 stderr。
			out = io.MultiWriter(f, os.Stderr)
		}
	}
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
