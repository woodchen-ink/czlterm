package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/woodchen-ink/czlterm/desktop/internal/askpass"
	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/launch"
)

// Connect 用外部程序打开连接: SSH 开系统终端, RDP 开系统远程桌面客户端, VNC 开系统 VNC 查看器。
func (a *App) Connect(id string) (launch.Result, error) {
	if err := a.ready(); err != nil {
		return launch.Result{}, err
	}
	c, err := a.conns.Get(id)
	if err != nil {
		return launch.Result{}, err
	}
	hops, err := a.resolveChain(c)
	if err != nil {
		return launch.Result{}, err
	}
	target := hops[len(hops)-1]
	cfg := a.settings.Get()

	switch c.Protocol {
	case conn.ProtoSSH:
		if err := a.openSSH(c, hops, cfg.Terminal); err != nil {
			return launch.Result{}, err
		}
		// 终端打开后在后台采集机器信息, 失败只记日志 (如密码要靠终端手输的主机)。
		go func() {
			if _, err := a.collectFacts(c.ID, hops); err != nil {
				a.log.Info("collect facts", "conn", c.ID, "err", err)
			}
		}()
		return launch.Result{}, nil
	case conn.ProtoRDP:
		if len(hops) > 1 {
			return launch.Result{}, errors.New("4040 jump hosts are only supported for ssh")
		}
		return launch.OpenRDP(launch.Remote{Host: c.Host, Port: c.Port, Username: target.user, Password: target.password}, clipboard{a.ctx})
	case conn.ProtoVNC:
		return launch.OpenVNC(launch.Remote{Host: c.Host, Port: c.Port, Username: target.user, Password: target.password,
			ViewerPath: cfg.VNCViewerPath}, clipboard{a.ctx})
	}
	return launch.Result{}, fmt.Errorf("2001 unsupported protocol %q", c.Protocol)
}

// openSSH 在系统终端里运行系统 ssh。
//
// 私钥放进本连接专用的进程内 agent 端点 (SSH_AUTH_SOCK); 密码经 askpass 回调提供。
// 两者都只经启动脚本里的环境变量传递, 不出现在 ssh 的命令行参数中。
func (a *App) openSSH(c conn.Connection, hops []hop, terminal string) error {
	sshPath, err := launch.SSHBinary()
	if err != nil {
		return err
	}
	target := hops[len(hops)-1]

	var args []string
	if len(hops) > 1 {
		var jumps []string
		for _, h := range hops[:len(hops)-1] {
			jumps = append(jumps, sshDest(h.user, net.JoinHostPort(bracketless(h.conn.Host), strconv.Itoa(h.conn.Port))))
		}
		args = append(args, "-J", strings.Join(jumps, ","))
	}
	if c.Port != 22 {
		args = append(args, "-p", strconv.Itoa(c.Port))
	}
	// -- 之后的目标不会被当成选项, 双重保险 (Normalize 已拒绝以 - 开头的主机名)。
	args = append(args, "--", sshDest(target.user, c.Host))

	var env []string
	if keys := agentKeys(hops); len(keys) > 0 {
		sock, err := a.agents.Serve(c.ID, keys)
		if err != nil {
			return err
		}
		env = append(env, "SSH_AUTH_SOCK="+sock)
	}

	var creds []askpass.Credential
	local := localUsername()
	for _, h := range hops {
		if h.password == "" {
			continue
		}
		u := h.user
		if u == "" {
			u = local
		}
		creds = append(creds, askpass.Credential{Target: u + "@" + bracketless(h.conn.Host), Password: h.password})
	}
	if len(creds) > 0 {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("4041 locate czlterm executable: %w", err)
		}
		env = append(env, "SSH_ASKPASS="+exe, "SSH_ASKPASS_REQUIRE=force")
		if os.Getenv("DISPLAY") == "" {
			// OpenSSH 8.4 之前只在设置了 DISPLAY 时才用 askpass。
			env = append(env, "DISPLAY=:0")
		}
		env = append(env, a.askpass.Grant(creds, len(hops))...)
	}

	spec := launch.Spec{Title: c.Name, Program: sshPath, Args: args, Env: env}
	if err := launch.OpenTerminal(terminal, spec); err != nil {
		return err
	}
	a.log.Info("ssh launched", "conn", c.ID, "hops", len(hops), "agent", len(agentKeys(hops)) > 0, "askpass", len(creds) > 0)
	return nil
}

func sshDest(user, host string) string {
	if user == "" {
		return host
	}
	return user + "@" + host
}

// bracketless 去掉 IPv6 地址两侧的方括号, 由调用方按需重新加。
func bracketless(host string) string {
	return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
}
