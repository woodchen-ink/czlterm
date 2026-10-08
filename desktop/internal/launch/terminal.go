package launch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Terminal 是一个可选的终端程序。
type Terminal struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

type terminalDef struct {
	id, name string
	// probe 返回可执行文件或 .app 路径, 为空表示未安装。
	probe func() string
	// open 在终端里运行脚本。
	open func(probe, script, title string) *exec.Cmd
}

// Terminals 列出当前系统支持的终端及是否已安装。
func Terminals() []Terminal {
	var out []Terminal
	for _, d := range terminalDefs() {
		out = append(out, Terminal{ID: d.id, Name: d.name, Installed: d.probe() != ""})
	}
	return out
}

// DefaultTerminal 返回当前系统的默认终端 ID。
func DefaultTerminal() string {
	if runtime.GOOS == "windows" {
		if findWT() != "" {
			return "wt"
		}
		return "powershell"
	}
	return "terminal"
}

// OpenTerminal 在指定终端中运行 spec。
func OpenTerminal(terminalID string, spec Spec) error {
	var def *terminalDef
	for _, d := range terminalDefs() {
		if d.id == terminalID {
			def = &d
			break
		}
	}
	if def == nil {
		return fmt.Errorf("4001 unknown terminal %q", terminalID)
	}
	probe := def.probe()
	if probe == "" {
		return fmt.Errorf("4002 terminal %s is not installed", def.name)
	}
	script, err := writeScript(spec)
	if err != nil {
		return fmt.Errorf("4003 write launch script: %w", err)
	}
	cmd := def.open(probe, script, spec.Title)
	if err := startDetached(cmd); err != nil {
		os.Remove(script)
		return fmt.Errorf("4004 start terminal: %w", err)
	}
	return nil
}

// startDetached 启动并在后台回收子进程, 不等待它退出。
func startDetached(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func terminalDefs() []terminalDef {
	switch runtime.GOOS {
	case "darwin":
		return darwinTerminals()
	case "windows":
		return windowsTerminals()
	default:
		return linuxTerminals()
	}
}

func darwinTerminals() []terminalDef {
	app := func(names ...string) func() string {
		return func() string {
			for _, n := range names {
				for _, dir := range []string{"/Applications", "/System/Applications/Utilities", filepath.Join(os.Getenv("HOME"), "Applications")} {
					p := filepath.Join(dir, n+".app")
					if _, err := os.Stat(p); err == nil {
						return p
					}
				}
			}
			return ""
		}
	}
	return []terminalDef{
		{"terminal", "Terminal", app("Terminal"), func(_, script, _ string) *exec.Cmd {
			return exec.Command("open", "-a", "Terminal", script)
		}},
		{"iterm2", "iTerm2", app("iTerm"), func(_, script, _ string) *exec.Cmd {
			// AppleScript 字符串里只需转义反斜杠与双引号; 脚本路径在私有临时目录, 不含空格。
			esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(script)
			return exec.Command("osascript",
				"-e", `tell application "iTerm"`,
				"-e", `activate`,
				"-e", `create window with default profile command "`+esc+`"`,
				"-e", `end tell`)
		}},
		{"ghostty", "Ghostty", app("Ghostty"), func(p, script, _ string) *exec.Cmd {
			return exec.Command("open", "-na", p, "--args", "-e", script)
		}},
		{"wezterm", "WezTerm", app("WezTerm"), func(p, script, _ string) *exec.Cmd {
			return exec.Command(filepath.Join(p, "Contents", "MacOS", "wezterm"), "start", "--", script)
		}},
		{"kitty", "kitty", app("kitty"), func(p, script, _ string) *exec.Cmd {
			return exec.Command("open", "-na", p, "--args", script)
		}},
	}
}

func windowsTerminals() []terminalDef {
	ps := func(exe string) []string {
		return []string{exe, "-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File"}
	}
	look := func(name string) func() string {
		return func() string { p, _ := exec.LookPath(name); return p }
	}
	return []terminalDef{
		{"wt", "Windows Terminal", findWT, func(p, script, title string) *exec.Cmd {
			// wt 把参数里的 ; 当作子命令分隔符。
			title = strings.ReplaceAll(title, ";", ",")
			args := append([]string{"-w", "0", "new-tab", "--title", title}, ps("powershell.exe")...)
			return exec.Command(p, append(args, script)...)
		}},
		{"powershell", "PowerShell", look("powershell.exe"), func(p, script, _ string) *exec.Cmd {
			cmd := exec.Command(p, append(ps("")[1:], script)...)
			newConsole(cmd)
			return cmd
		}},
		{"pwsh", "PowerShell 7", look("pwsh.exe"), func(p, script, _ string) *exec.Cmd {
			cmd := exec.Command(p, append(ps("")[1:], script)...)
			newConsole(cmd)
			return cmd
		}},
	}
}

// findWT 找 Windows Terminal。商店版的 wt.exe 是 WindowsApps 下的执行别名, 通常已在 PATH 里。
func findWT() string {
	if p, err := exec.LookPath("wt.exe"); err == nil {
		return p
	}
	p := filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps", "wt.exe")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func linuxTerminals() []terminalDef {
	look := func(name string) func() string {
		return func() string { p, _ := exec.LookPath(name); return p }
	}
	return []terminalDef{
		{"x-terminal-emulator", "System terminal", look("x-terminal-emulator"), func(p, script, _ string) *exec.Cmd {
			return exec.Command(p, "-e", script)
		}},
		{"gnome-terminal", "GNOME Terminal", look("gnome-terminal"), func(p, script, _ string) *exec.Cmd {
			return exec.Command(p, "--", script)
		}},
	}
}

// SSHBinary 返回系统 ssh 的路径。Windows 优先用系统自带的 OpenSSH, 避免 PATH 里 Git 自带的旧版
// ssh 抢先 (它不认 Windows 命名管道形式的 SSH_AUTH_SOCK)。
func SSHBinary() (string, error) {
	if runtime.GOOS == "windows" {
		p := filepath.Join(os.Getenv("SystemRoot"), "System32", "OpenSSH", "ssh.exe")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	p, err := exec.LookPath("ssh")
	if err != nil {
		return "", fmt.Errorf("4005 ssh client not found, install OpenSSH")
	}
	return p, nil
}

// removeLater 在 d 之后删除临时文件 (.rdp / VNC 密码文件), 给外部程序留出读取时间。
func removeLater(path string, d time.Duration) {
	time.AfterFunc(d, func() { os.Remove(path) })
}
