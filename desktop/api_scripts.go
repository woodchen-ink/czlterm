package main

import (
	"errors"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/script"
)

// ScriptList 是脚本列表。Warnings 报告读不出来的脚本文件。
type ScriptList struct {
	Items    []script.Script `json:"items"`
	Groups   []string        `json:"groups"`
	Warnings []string        `json:"warnings"`
}

// ListScripts 返回全部脚本, 按分组、名称排序。
func (a *App) ListScripts() (ScriptList, error) {
	if err := a.ready(); err != nil {
		return ScriptList{}, err
	}
	list, errs := a.scripts.List()
	out := ScriptList{Items: list, Groups: []string{}, Warnings: []string{}}
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

// SaveScript 新建或更新脚本, 返回保存后的脚本。
func (a *App) SaveScript(s script.Script) (script.Script, error) {
	if err := a.ready(); err != nil {
		return s, err
	}
	// 与连接共用数据目录的写锁: git 同步 rebase 期间写入的文件会被 rebase --abort 回滚。
	var saved script.Script
	err := a.conns.Exclusive(func() (err error) {
		saved, err = a.scripts.Save(s)
		return err
	})
	if err != nil {
		return s, err
	}
	a.scheduleAutoSync()
	return saved, nil
}

// DeleteScript 删除脚本。
func (a *App) DeleteScript(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.conns.Exclusive(func() error { return a.scripts.Delete(id) }); err != nil {
		return err
	}
	a.scheduleAutoSync()
	return nil
}

// RunScript 在系统终端里连接 SSH 服务器并执行脚本, 执行完留在交互式 shell 里。
func (a *App) RunScript(connID, scriptID string) error {
	if err := a.ready(); err != nil {
		return err
	}
	c, err := a.conns.Get(connID)
	if err != nil {
		return err
	}
	if c.Protocol != conn.ProtoSSH {
		return errors.New("4050 scripts can only run on ssh connections")
	}
	// 默认 shell 是 cmd / PowerShell 的 Windows 主机跑不了 sh -c; 没采集过机器信息时放行。
	if a.facts.Get(c.ID).OSID == "windows" {
		return errors.New("4051 scripts are not supported on windows hosts")
	}
	s, err := a.scripts.Get(scriptID)
	if err != nil {
		return err
	}
	cmd, err := script.RemoteCommand(s.Content)
	if err != nil {
		return err
	}
	hops, err := a.resolveChain(c)
	if err != nil {
		return err
	}
	return a.openSSH(c, hops, sshSession{title: c.Name + " · " + s.Name, command: cmd})
}
