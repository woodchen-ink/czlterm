package launch

import (
	"crypto/des"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
)

// Clipboard 是系统剪贴板, 由调用方用 Wails runtime 实现。
type Clipboard interface {
	SetText(string) error
	Text() (string, error)
}

// clipboardTTL 是密码留在剪贴板里的时长。
const clipboardTTL = 45 * time.Second

// Remote 描述一次 RDP / VNC 启动。
type Remote struct {
	Host     string
	Port     int
	Username string
	Password string
	// ViewerPath 是 Windows 上 VNC 查看器的路径, 为空时自动查找 TigerVNC。
	ViewerPath string
}

// Result 告诉界面启动后用户还需要做什么。
type Result struct {
	// PasswordCopied 为 true 时密码已复制到剪贴板, 将在 ClipboardSeconds 秒后清除。
	PasswordCopied   bool `json:"passwordCopied"`
	ClipboardSeconds int  `json:"clipboardSeconds"`
}

// OpenRDP 用系统 RDP 客户端连接: Windows 为 mstsc, macOS 为 Windows App。
//
// Windows 上密码临时写进凭据管理器 (TERMSRV/<host>), mstsc 读取后还原: 用户原先为该主机保存的凭据
// 先备份, 到期写回; 原先没有的直接删除。
// macOS 的 Windows App 不接受外部传入的密码 (.rdp 里的密码字段要用 Windows DPAPI 加密),
// 只能复制到剪贴板让用户粘贴, 并在 45 秒后清除。
func OpenRDP(r Remote, clip Clipboard) (Result, error) {
	addr := r.Host
	if r.Port != 0 && r.Port != 3389 {
		addr = net.JoinHostPort(r.Host, strconv.Itoa(r.Port))
	}
	lines := []string{"full address:s:" + addr}
	if r.Username != "" {
		lines = append(lines, "username:s:"+r.Username)
	}
	if runtime.GOOS == "windows" && r.Password != "" {
		lines = append(lines, "prompt for credentials:i:0")
	}
	dir, err := paths.Cache("launch")
	if err != nil {
		return Result{}, err
	}
	file := filepath.Join(dir, "rdp-"+randHex()+".rdp")
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\r\n")+"\r\n"), 0o600); err != nil {
		return Result{}, fmt.Errorf("4010 write rdp file: %w", err)
	}
	removeLater(file, 2*time.Minute)

	var res Result
	switch runtime.GOOS {
	case "windows":
		if r.Password != "" {
			restore, err := storeRDPCredential(r.Host, r.Username, r.Password)
			if err != nil {
				return res, fmt.Errorf("4011 store rdp credential: %w", err)
			}
			time.AfterFunc(2*time.Minute, restore)
		}
		mstsc := filepath.Join(os.Getenv("SystemRoot"), "System32", "mstsc.exe")
		if err := startDetached(exec.Command(mstsc, file)); err != nil {
			return res, fmt.Errorf("4012 start mstsc: %w", err)
		}
	case "darwin":
		if err := openWithFirst(file, "Windows App", "Microsoft Remote Desktop"); err != nil {
			return res, err
		}
		if r.Password != "" {
			res = copyTemporarily(clip, r.Password)
		}
	default:
		p, err := exec.LookPath("xfreerdp")
		if err != nil {
			return res, fmt.Errorf("4013 no rdp client found, install FreeRDP")
		}
		args := []string{"/v:" + addr}
		if r.Username != "" {
			args = append(args, "/u:"+r.Username)
		}
		if err := startDetached(exec.Command(p, args...)); err != nil {
			return res, fmt.Errorf("4012 start rdp client: %w", err)
		}
		if r.Password != "" {
			res = copyTemporarily(clip, r.Password)
		}
	}
	return res, nil
}

// OpenVNC 用系统 VNC 客户端连接: macOS 为屏幕共享 (vnc:// 链接), Windows 为 TigerVNC。
//
// macOS 上密码不放进 vnc:// 链接: 链接是 open 的命令行参数, 同机其它用户用 ps 就能看到。
// 用户名可以带上, 密码走剪贴板。
func OpenVNC(r Remote, clip Clipboard) (Result, error) {
	var res Result
	if runtime.GOOS == "darwin" {
		u := url.URL{Scheme: "vnc", Host: net.JoinHostPort(r.Host, strconv.Itoa(r.Port))}
		if r.Username != "" {
			u.User = url.User(r.Username)
		}
		if err := startDetached(exec.Command("open", u.String())); err != nil {
			return res, fmt.Errorf("4020 open screen sharing: %w", err)
		}
		if r.Password != "" {
			res = copyTemporarily(clip, r.Password)
		}
		return res, nil
	}

	viewer := r.ViewerPath
	if viewer == "" {
		viewer = findVNCViewer()
	}
	if viewer == "" {
		return res, fmt.Errorf("4021 no vnc viewer found, install TigerVNC or set its path in settings")
	}
	args := []string{fmt.Sprintf("%s::%d", r.Host, r.Port)}
	// 只有 TigerVNC 认 -passwd 的混淆密码文件; 其它查看器退回剪贴板。
	if r.Password != "" && isTigerVNC(viewer) {
		dir, err := paths.Cache("launch")
		if err != nil {
			return res, err
		}
		file := filepath.Join(dir, "vnc-"+randHex()+".passwd")
		if err := os.WriteFile(file, obfuscateVNCPassword(r.Password), 0o600); err != nil {
			return res, fmt.Errorf("4022 write vnc password file: %w", err)
		}
		removeLater(file, 2*time.Minute)
		args = append(args, "-passwd", file)
	} else if r.Password != "" {
		res = copyTemporarily(clip, r.Password)
	}
	if err := startDetached(exec.Command(viewer, args...)); err != nil {
		return res, fmt.Errorf("4023 start vnc viewer: %w", err)
	}
	return res, nil
}

func findVNCViewer() string {
	if runtime.GOOS == "windows" {
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			p := filepath.Join(base, "TigerVNC", "vncviewer.exe")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	for _, name := range []string{"vncviewer", "xtigervncviewer"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func isTigerVNC(path string) bool {
	p := strings.ToLower(path)
	return strings.Contains(p, "tigervnc") || strings.Contains(p, "xtigervncviewer")
}

// vncKey 是 VNC 密码文件的固定 DES 密钥 (各实现通用), 已按 crypto/des 的位序翻转。
var vncKey = []byte{0xe8, 0x4a, 0xd6, 0x60, 0xc4, 0x72, 0x1a, 0xe0}

// obfuscateVNCPassword 按 vncpasswd 格式编码: 截断或补零到 8 字节后 DES 加密。这只是混淆, 不是加密。
func obfuscateVNCPassword(pw string) []byte {
	buf := make([]byte, 8)
	copy(buf, pw)
	block, _ := des.NewCipher(vncKey)
	out := make([]byte, 8)
	block.Encrypt(out, buf)
	return out
}

// openWithFirst 用第一个已安装的 macOS 应用打开文件, 都没有时交给系统默认程序。
func openWithFirst(file string, apps ...string) error {
	for _, app := range apps {
		if exec.Command("open", "-Ra", app).Run() == nil {
			return startDetached(exec.Command("open", "-a", app, file))
		}
	}
	if err := startDetached(exec.Command("open", file)); err != nil {
		return fmt.Errorf("4014 no rdp client found, install Windows App from the App Store")
	}
	return nil
}

// copyTemporarily 把密码放进剪贴板, 到期时若剪贴板内容未被替换就清空。
func copyTemporarily(clip Clipboard, secret string) Result {
	if clip == nil || clip.SetText(secret) != nil {
		return Result{}
	}
	time.AfterFunc(clipboardTTL, func() {
		if cur, err := clip.Text(); err == nil && cur == secret {
			_ = clip.SetText("")
		}
	})
	return Result{PasswordCopied: true, ClipboardSeconds: int(clipboardTTL / time.Second)}
}
