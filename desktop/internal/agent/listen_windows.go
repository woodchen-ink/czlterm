package agent

import (
	"fmt"
	"net"
	"time"

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

// defaultUpstream 是 Windows OpenSSH agent 服务的固定管道。
const defaultUpstream = `\\.\pipe\openssh-ssh-agent`

func dialUpstream(sock string) (net.Conn, error) {
	timeout := 2 * time.Second
	return winio.DialPipe(sock, &timeout)
}
