// Package launch 调起外部程序: 系统终端里的 ssh、系统 RDP 客户端、VNC 查看器、文本编辑器。
//
// 本包只负责"怎么调起"; 凭据从哪来、跳板链怎么解析由调用方决定后以 Spec 传入。
package launch

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
)

// Spec 描述一次终端里的命令启动。
type Spec struct {
	// Title 是终端窗口 / 标签标题。
	Title string
	// Program 与 Args 是要执行的命令, Program 为绝对路径或 PATH 中的名字。
	Program string
	Args    []string
	// Env 为额外环境变量 (K=V), 写进启动脚本而不是终端程序的环境:
	// Windows Terminal 等单实例终端的新标签由已运行的进程创建, 不继承调用方的环境。
	Env []string
}

// writeScript 写出自删除的启动脚本, 返回路径。脚本第一步就删除自身, 里面的一次性令牌不在磁盘上久留。
func writeScript(s Spec) (string, error) {
	dir, err := paths.PrivateTemp()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, scriptName(s.Title)+".ps1")
		// Windows PowerShell 5 按 ANSI 读无 BOM 的脚本, 中文标题会乱码。
		body := "\ufeff" + powershellScript(s)
		return path, os.WriteFile(path, []byte(body), 0o600)
	}
	path := filepath.Join(dir, scriptName(s.Title)+".sh")
	return path, os.WriteFile(path, []byte(shScript(s)), 0o700)
}

func shScript(s Spec) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\nrm -f \"$0\"\n")
	// macOS Terminal 默认把 LC_CTYPE 设成不规范的 "UTF-8", ssh 经 SendEnv 带到 Linux 后
	// 每次登录都刷 setlocale 警告。只替换这一个值, 用户自己设置的 locale 不动。
	b.WriteString("[ \"$LC_CTYPE\" = \"UTF-8\" ] && export LC_CTYPE=en_US.UTF-8\n")
	for _, kv := range s.Env {
		k, v, _ := strings.Cut(kv, "=")
		fmt.Fprintf(&b, "export %s=%s\n", k, shQuote(v))
	}
	fmt.Fprintf(&b, "printf '\\033]0;%%s\\007' %s\n", shQuote(s.Title))
	b.WriteString(shQuote(s.Program))
	for _, a := range s.Args {
		b.WriteString(" " + shQuote(a))
	}
	// 连接失败时停住, 否则终端窗口一闪而过, 用户看不到报错。
	b.WriteString("\ncode=$?\nif [ $code -ne 0 ]; then printf '\\n[czlterm] exited with code %s, press Enter to close.' \"$code\"; read _; fi\n")
	return b.String()
}

func powershellScript(s Spec) string {
	var b strings.Builder
	b.WriteString("Remove-Item -LiteralPath $PSCommandPath -Force -ErrorAction SilentlyContinue\n")
	fmt.Fprintf(&b, "$Host.UI.RawUI.WindowTitle = %s\n", psQuote(s.Title))
	for _, kv := range s.Env {
		k, v, _ := strings.Cut(kv, "=")
		fmt.Fprintf(&b, "$env:%s = %s\n", k, psQuote(v))
	}
	b.WriteString("& " + psQuote(s.Program))
	for _, a := range s.Args {
		b.WriteString(" " + psQuote(a))
	}
	b.WriteString("\nif ($LASTEXITCODE -ne 0) { Write-Host ''; Read-Host \"[czlterm] exited with code $LASTEXITCODE, press Enter to close\" | Out-Null }\n")
	return b.String()
}

// shQuote 用单引号包裹; 内部的单引号先闭合引号、反斜杠转义、再重新打开。
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// psQuote 用 PowerShell 单引号字符串, 内部的单引号重复一次即为转义。单引号字符串里 $ 与反引号都不展开。
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// scriptName 生成启动脚本的文件名。终端 (如 macOS Terminal) 会把脚本名显示在标题栏,
// 所以带上连接名; 末尾随机串避免同名连接同时打开时互相覆盖。
func scriptName(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
		if b.Len() >= 40 {
			break
		}
	}
	name := strings.Trim(b.String(), "-.")
	if name == "" {
		name = "session"
	}
	return "czlterm-" + name + "-" + randHex()[:6]
}

func randHex() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
