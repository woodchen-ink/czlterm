package agent

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// listen 创建仅当前用户可访问的命名管道。Windows 版 OpenSSH 的 SSH_AUTH_SOCK 接受管道路径。
func listen(name string) (string, net.Listener, error) {
	tok := windows.GetCurrentProcessToken()
	user, err := tok.GetTokenUser()
	if err != nil {
		return "", nil, fmt.Errorf("get token user: %w", err)
	}
	sddl := "D:P(A;;GA;;;" + user.User.Sid.String() + ")"
	path := `\\.\pipe\czlterm-agent-` + name
	ln, err := winio.ListenPipe(path, &winio.PipeConfig{SecurityDescriptor: sddl})
	if err != nil {
		return "", nil, err
	}
	return path, ln, nil
}

func cleanup(string) {}
