package launch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Editor 是一个可选的文本编辑器。
type Editor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

type editorDef struct {
	id, name string
	// mac 是 macOS 上的应用名; win 是 Windows 上相对 %LOCALAPPDATA%\Programs 与 %ProgramFiles% 的候选路径; cli 是 PATH 中的命令名。
	mac string
	win []string
	cli string
}

var editorDefs = []editorDef{
	{"vscode", "Visual Studio Code", "Visual Studio Code", []string{`Microsoft VS Code\Code.exe`}, "code"},
	{"cursor", "Cursor", "Cursor", []string{`cursor\Cursor.exe`}, "cursor"},
	{"zed", "Zed", "Zed", []string{`Zed\Zed.exe`}, "zed"},
	{"sublime", "Sublime Text", "Sublime Text", []string{`Sublime Text\sublime_text.exe`}, "subl"},
	{"system", "System default", "", nil, ""},
}

// Editors 列出可选编辑器及是否已安装。
func Editors() []Editor {
	var out []Editor
	for _, d := range editorDefs {
		out = append(out, Editor{ID: d.id, Name: d.name, Installed: d.id == "system" || resolveEditor(d) != nil})
	}
	return out
}

// OpenInEditor 用指定编辑器打开本地文件。custom 非空时直接运行该程序。
func OpenInEditor(editorID, custom, file string) error {
	if custom != "" {
		return startDetached(exec.Command(custom, file))
	}
	for _, d := range editorDefs {
		if d.id != editorID {
			continue
		}
		if d.id == "system" {
			return openDefault(file)
		}
		argv := resolveEditor(d)
		if argv == nil {
			return fmt.Errorf("4030 editor %s is not installed", d.name)
		}
		return startDetached(exec.Command(argv[0], append(argv[1:], file)...))
	}
	return fmt.Errorf("4031 unknown editor %q", editorID)
}

// resolveEditor 返回启动编辑器的 argv 前缀, 未安装时为 nil。
func resolveEditor(d editorDef) []string {
	switch runtime.GOOS {
	case "darwin":
		if d.mac != "" && exec.Command("open", "-Ra", d.mac).Run() == nil {
			return []string{"open", "-a", d.mac}
		}
	case "windows":
		for _, base := range []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"), os.Getenv("ProgramFiles")} {
			for _, rel := range d.win {
				p := filepath.Join(base, rel)
				if _, err := os.Stat(p); err == nil {
					return []string{p}
				}
			}
		}
	}
	if d.cli != "" {
		if p, err := exec.LookPath(d.cli); err == nil {
			return []string{p}
		}
	}
	return nil
}

// openDefault 用系统默认程序打开文件或目录。
func openDefault(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return startDetached(exec.Command("open", path))
	case "windows":
		return startDetached(exec.Command("rundll32", "url.dll,FileProtocolHandler", path))
	default:
		return startDetached(exec.Command("xdg-open", path))
	}
}

// Reveal 在文件管理器中显示本地文件。
func Reveal(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return startDetached(exec.Command("open", "-R", path))
	case "windows":
		return startDetached(exec.Command("explorer", "/select,", path))
	default:
		return openDefault(filepath.Dir(path))
	}
}
