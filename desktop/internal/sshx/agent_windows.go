package sshx

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// dialAgent 连接 Windows OpenSSH 的 agent 命名管道。
func dialAgent(sock string) (net.Conn, error) {
	timeout := 2 * time.Second
	return winio.DialPipe(sock, &timeout)
}

// defaultAgentSock 是 Windows OpenSSH agent 服务的固定管道。
const defaultAgentSock = `\\.\pipe\openssh-ssh-agent`
