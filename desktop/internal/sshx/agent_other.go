//go:build !windows

package sshx

import "net"

func dialAgent(sock string) (net.Conn, error) {
	return net.Dial("unix", sock)
}

// defaultAgentSock 为空: macOS / Linux 的系统 agent 总是经 SSH_AUTH_SOCK 告知。
const defaultAgentSock = ""
