// Package script 定义常用脚本模型、本地 JSON 存储与远程执行命令的拼装。
//
// 每个脚本一个文件 (`data/scripts/<id>.json`), 与连接一样随数据目录同步。
// 脚本在系统终端里的 ssh 会话中执行, 执行完进入交互式登录 shell, 相当于连上服务器后手动输入了这些命令。
package script

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxContentBytes 是脚本内容上限。编码后的脚本要放进 ssh 的命令行参数, Windows 命令行总长不超过 32767 字符。
const MaxContentBytes = 16 * 1024

// Script 是一条常用脚本。
type Script struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
	// Content 是脚本正文; 首行可用 #! 指定解释器 (如 #!/bin/bash), 缺省用 sh。
	Content string `json:"content"`
	Notes   string `json:"notes"`
	// UpdatedAt 为 RFC3339 UTC 时间。
	UpdatedAt string `json:"updatedAt"`
}

// Normalize 清理用户输入并校验, 返回校验错误。
func (s *Script) Normalize() error {
	s.Name = strings.TrimSpace(s.Name)
	s.Group = strings.Trim(strings.TrimSpace(s.Group), "/")
	// Windows 上编辑的脚本带 \r, 远程 sh 会把它当成命令的一部分。
	s.Content = strings.ReplaceAll(s.Content, "\r\n", "\n")
	if s.Name == "" {
		return fmt.Errorf("2101 script name is required")
	}
	if strings.TrimSpace(s.Content) == "" {
		return fmt.Errorf("2102 script content is required")
	}
	if len(s.Content) > MaxContentBytes {
		return fmt.Errorf("2103 script content larger than %d bytes", MaxContentBytes)
	}
	if !utf8.ValidString(s.Content) {
		return fmt.Errorf("2105 script content is not valid utf-8")
	}
	if _, err := Interpreter(s.Content); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return nil
}

// Interpreter 返回执行脚本用的解释器命令: 首行 #! 指定的程序 (含参数), 没有时为 sh。
//
// 解释器会原样拼进远程命令, 只允许路径与参数常见的字符, 不允许引号、$ 等 shell 元字符。
func Interpreter(content string) (string, error) {
	first, _, _ := strings.Cut(content, "\n")
	rest, ok := strings.CutPrefix(first, "#!")
	if !ok {
		return "sh", nil
	}
	interp := strings.Join(strings.Fields(rest), " ")
	if interp == "" {
		return "", fmt.Errorf("2104 invalid interpreter line")
	}
	for _, r := range interp {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._+- ", r)) {
			return "", fmt.Errorf("2104 invalid interpreter line")
		}
	}
	return interp, nil
}

// RemoteCommand 生成 ssh 的远程命令: 把脚本写进远程临时文件、用解释器执行、删除临时文件,
// 最后 exec 进入用户的交互式登录 shell。
//
// 脚本以 base64 传输并整体包在 `sh -c '...'` 里, 远程登录 shell 是 bash / zsh / fish 都能执行;
// 生成的命令不含双引号: Windows PowerShell 5 把参数传给原生程序时不转义内嵌的双引号。
// 脚本的标准输入是终端, 交互式提示 (如 apt 的确认) 照常可用; Ctrl+C 只中断脚本, 不断开会话。
func RemoteCommand(content string) (string, error) {
	interp, err := Interpreter(content)
	if err != nil {
		return "", err
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(content))
	inner := strings.Join([]string{
		"trap : INT",
		"b=$(mktemp) && t=$(mktemp) || exit 1",
		"printf %s " + b64 + " > $b",
		// macOS 12 及更早的 base64 只认 -D, BusyBox 只认 -d。
		"{ base64 -d < $b || base64 -D < $b; } > $t 2>/dev/null && " + interp + " $t",
		"c=$?",
		"rm -f $b $t",
		"echo",
		"echo czlterm: script exited with code $c",
		"exec ${SHELL:-sh} -l",
	}, "; ")
	return "sh -c '" + inner + "'", nil
}
