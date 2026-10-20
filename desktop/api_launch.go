package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

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
		return launch.Result{}, a.openSSH(c, hops, sshSession{title: c.Name})
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

// sshSession 描述一次终端里的 ssh 会话。
type sshSession struct {
	// title 是终端窗口 / 标签标题。
	title string
	// command 是远程命令, 为空时打开交互式登录 shell。
	command string
}

// openSSH 在系统终端里运行系统 ssh, 成功后在后台采集机器信息。
//
// 私钥放进各跳独立的进程内 agent 端点, 通过临时 SSH 配置中的 IdentityAgent 指定。
// 密码经 askpass 回调提供, 不出现在 ssh 的命令行参数或配置文件中。
func (a *App) openSSH(c conn.Connection, hops []hop, sess sshSession) error {
	sshPath, err := launch.SSHBinary()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("4041 locate czlterm executable: %w", err)
	}
	launchHops, err := a.terminalHops(hops, exe)
	if err != nil {
		return err
	}
	args, files, err := launch.SSHCommand(sshPath, exe, launchHops)
	if err != nil {
		return err
	}
	launched := false
	defer func() {
		if !launched {
			for _, path := range files {
				_ = os.Remove(path)
			}
		}
	}()
	if sess.command != "" {
		args = append([]string{"-t"}, args...)
		args = append(args, sess.command)
	}

	spec := launch.Spec{Title: sess.title, Program: sshPath, Args: args,
		Env: launchHops[len(launchHops)-1].Env, CleanupFiles: files}
	if err := launch.OpenTerminal(a.settings.Get().Terminal, spec); err != nil {
		return err
	}
	launched = true
	hasKey, hasPassword := false, false
	for _, h := range launchHops {
		hasKey = hasKey || h.Agent != ""
		hasPassword = hasPassword || h.Password
	}
	a.log.Info("ssh launched", "conn", c.ID, "hops", len(hops), "agent", hasKey, "askpass", hasPassword,
		"command", sess.command != "")

	// 终端打开后在后台采集机器信息, 失败只记日志 (如密码要靠终端手输的主机)。
	go func() {
		if _, err := a.collectFacts(c.ID, hops); err != nil {
			a.log.Info("collect facts", "conn", c.ID, "err", err)
		}
	}()
	return nil
}

// bracketless 去掉 IPv6 地址两侧的方括号, 由调用方按需重新加。
func bracketless(host string) string {
	return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
}
