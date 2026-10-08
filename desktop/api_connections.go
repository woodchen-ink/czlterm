package main

import (
	"strings"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
)

// ConnectionView 是列表与编辑页用的连接视图: 连接本身加上界面需要的派生字段。
type ConnectionView struct {
	conn.Connection
	// JumpName 是跳板机的名称, 跳板机已不存在时为空。
	JumpName string `json:"jumpName"`
	// VaultItemName / KeyItemName 是所引用条目的名称; 保险库锁定或条目已删除时为空。
	VaultItemName string `json:"vaultItemName"`
	KeyItemName   string `json:"keyItemName"`
	// OSID / OSName 来自最近一次采集的机器信息, 列表据此显示系统图标; 从未采集时为空。
	OSID   string `json:"osId"`
	OSName string `json:"osName"`
}

// ConnectionList 是连接列表。Warnings 报告读不出来的连接文件, 不让一个坏文件挡住整个列表。
type ConnectionList struct {
	Items    []ConnectionView `json:"items"`
	Groups   []string         `json:"groups"`
	Warnings []string         `json:"warnings"`
}

// ListConnections 返回全部连接, 按分组、名称排序。
func (a *App) ListConnections() (ConnectionList, error) {
	if err := a.ready(); err != nil {
		return ConnectionList{}, err
	}
	list, errs := a.conns.List()
	names := make(map[string]string, len(list))
	for _, c := range list {
		names[c.ID] = c.Name
	}
	out := ConnectionList{Items: make([]ConnectionView, 0, len(list)), Groups: []string{}, Warnings: []string{}}
	seen := map[string]bool{}
	for _, c := range list {
		v := ConnectionView{Connection: c, JumpName: names[c.Jump]}
		v.VaultItemName = a.itemName(c.VaultItem)
		v.KeyItemName = a.itemName(c.KeyItem)
		f := a.facts.Get(c.ID)
		v.OSID, v.OSName = f.OSID, f.OSName
		if v.OSID == "" && f.OSLike != "" {
			v.OSID = strings.Fields(f.OSLike)[0]
		}
		out.Items = append(out.Items, v)
		if c.Group != "" && !seen[c.Group] {
			seen[c.Group] = true
			out.Groups = append(out.Groups, c.Group)
		}
	}
	for _, e := range errs {
		out.Warnings = append(out.Warnings, e.Error())
	}
	return out, nil
}

func (a *App) itemName(id string) string {
	if id == "" {
		return ""
	}
	if s, ok := a.vault.Summary(id); ok {
		return s.Name
	}
	return ""
}

// SaveConnection 新建或更新连接, 返回保存后的连接。
func (a *App) SaveConnection(c conn.Connection) (conn.Connection, error) {
	if err := a.ready(); err != nil {
		return c, err
	}
	saved, err := a.conns.Save(c)
	if err != nil {
		return saved, err
	}
	// 连接配置变了, 已建立的进程内连接可能指向旧主机或旧凭据。
	a.pool.Drop(saved.ID)
	a.scheduleAutoSync()
	return saved, nil
}

// DuplicateConnection 复制一条连接, 名称加后缀。
func (a *App) DuplicateConnection(id string) (conn.Connection, error) {
	if err := a.ready(); err != nil {
		return conn.Connection{}, err
	}
	c, err := a.conns.Get(id)
	if err != nil {
		return c, err
	}
	c.ID = ""
	c.Name += " (copy)"
	return a.SaveConnection(c)
}

// DeleteConnection 删除连接。
func (a *App) DeleteConnection(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.conns.Delete(id); err != nil {
		return err
	}
	a.pool.Drop(id)
	a.facts.Delete(id)
	a.scheduleAutoSync()
	return nil
}
