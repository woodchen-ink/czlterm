package main

import (
	"errors"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/script"
)

// ScriptList 是当前空间的脚本列表。Warnings 报告读不出来的脚本文件。
type ScriptList struct {
	Space    string          `json:"space"`
	ReadOnly bool            `json:"readOnly"`
	Items    []script.Script `json:"items"`
	Groups   []string        `json:"groups"`
	Warnings []string        `json:"warnings"`
}

// ListScripts 返回全部脚本, 按分组、名称排序。
func (a *App) ListScripts() (ScriptList, error) {
	if err := a.ready(); err != nil {
		return ScriptList{}, err
	}
	sp := a.currentSpace()
	list, errs := sp.Scripts.List()
	out := ScriptList{Space: sp.Key, ReadOnly: sp.ReadOnly(), Items: list, Groups: []string{}, Warnings: []string{}}
	seen := map[string]bool{}
	for _, s := range list {
		if s.Group != "" && !seen[s.Group] {
			seen[s.Group] = true
			out.Groups = append(out.Groups, s.Group)
		}
	}
	for _, e := range errs {
		out.Warnings = append(out.Warnings, e.Error())
	}
	return out, nil
}

// SaveScript 新建或更新脚本, 返回保存后的脚本。已有脚本存回它所在的空间, 新建的进当前空间。
func (a *App) SaveScript(s script.Script) (script.Script, error) {
	if err := a.ready(); err != nil {
		return s, err
	}
	sp := a.currentSpace()
	if s.ID != "" {
		if _, owner, err := a.spaces.FindScript(a.currentSpace().Key, s.ID); err == nil {
			sp = owner
		}
	}
	return sp.SaveScript(s)
}

// DeleteScript 删除脚本。
func (a *App) DeleteScript(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	_, sp, err := a.spaces.FindScript(a.currentSpace().Key, id)
	if err != nil {
		return err
	}
	return sp.DeleteScript(id)
}

// RunScript 在内置终端或系统终端里连接 SSH 服务器并执行脚本, 执行完留在交互式 shell 里。
// 走内置终端时返回会话 ID, 系统终端返回空串。
func (a *App) RunScript(connID, scriptID string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	c, sp, err := a.findConn(connID)
	if err != nil {
		return "", err
	}
	if c.Protocol != conn.ProtoSSH {
		return "", errors.New("4050 scripts can only run on ssh connections")
	}
	// 默认 shell 是 cmd / PowerShell 的 Windows 主机跑不了 sh -c; 没采集过机器信息时放行。
	if a.facts.Get(c.ID).OSID == "windows" {
		return "", errors.New("4051 scripts are not supported on windows hosts")
	}
	s, _, err := a.spaces.FindScript(a.currentSpace().Key, scriptID)
	if err != nil {
		return "", err
	}
	cmd, err := script.RemoteCommand(s.Content)
	if err != nil {
		return "", err
	}
	hops, err := a.resolveChain(sp, c)
	if err != nil {
		return "", err
	}
	return a.openSSH(c, hops, sshSession{title: c.Name + " · " + s.Name, command: cmd, scriptID: s.ID})
}
