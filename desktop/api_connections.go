package main

import (
	"context"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

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
	// CredentialName 是所引用保存的凭据的名称 (auth=stored); 凭据已删除或读不出时为空。
	CredentialName string `json:"credentialName"`
	// OSID / OSName 来自最近一次采集的机器信息, 列表据此显示系统图标; 从未采集时为空。
	OSID   string `json:"osId"`
	OSName string `json:"osName"`
	// CountryCode (ISO 3166-1 alpha-2, 大写) / Country 是主机所在国家, 未查到或私网地址时为空。
	CountryCode string `json:"countryCode"`
	Country     string `json:"country"`
}

// ConnectionList 是当前空间的连接列表。Warnings 报告读不出来的连接文件, 不让一个坏文件挡住整个列表。
type ConnectionList struct {
	// Space 是列表所属的空间标识; ReadOnly 为 true 时界面禁用新建、编辑与删除。
	Space    string           `json:"space"`
	ReadOnly bool             `json:"readOnly"`
	Items    []ConnectionView `json:"items"`
	Groups   []string         `json:"groups"`
	Warnings []string         `json:"warnings"`
}

// ListConnections 返回当前空间的全部连接, 按分组、名称排序。
func (a *App) ListConnections() (ConnectionList, error) {
	if err := a.ready(); err != nil {
		return ConnectionList{}, err
	}
	sp := a.currentSpace()
	list, errs := sp.Conns.List()
	names := make(map[string]string, len(list))
	for _, c := range list {
		names[c.ID] = c.Name
	}
	out := ConnectionList{Space: sp.Key, ReadOnly: sp.ReadOnly(), Items: make([]ConnectionView, 0, len(list)), Groups: []string{}, Warnings: []string{}}
	seen := map[string]bool{}
	geoOn := a.settings.Get().GeoLookup
	hosts := make([]string, 0, len(list))
	var credNames map[string]string // 有连接引用保存的凭据时才读一次摘要
	for _, c := range list {
		v := ConnectionView{Connection: c, JumpName: names[c.Jump]}
		v.VaultItemName = a.itemName(c.VaultItem)
		v.KeyItemName = a.itemName(c.KeyItem)
		if c.Auth == "stored" && c.Credential != "" {
			if credNames == nil {
				credNames = map[string]string{}
				sums, _ := sp.Creds.List()
				for _, s := range sums {
					credNames[s.ID] = s.Name
				}
			}
			v.CredentialName = credNames[c.Credential]
		}
		f := a.facts.Get(c.ID)
		v.OSID, v.OSName = f.OSID, f.OSName
		if v.OSID == "" && f.OSLike != "" {
			v.OSID = strings.Fields(f.OSLike)[0]
		}
		if geoOn {
			g := a.geo.Get(c.Host)
			v.CountryCode, v.Country = g.CountryCode, g.Country
			hosts = append(hosts, c.Host)
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
	if geoOn {
		// 有坏文件时列表不完整, 只看一个空间时也不全, 都不据此清理缓存。
		if len(errs) == 0 && len(a.spaces.All()) == 1 {
			a.geo.Prune(hosts)
		}
		go a.refreshGeo(hosts)
	}
	return out, nil
}

// refreshGeo 在后台查询缺失或过期的主机国家, 有变化时通知界面重新拉列表。
func (a *App) refreshGeo(hosts []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// 每查一个主机前确认开关仍开着, 关闭后立即停下。
	enabled := func() bool { return a.settings.Get().GeoLookup }
	if a.geo.Refresh(ctx, hosts, enabled) {
		wruntime.EventsEmit(a.ctx, eventGeo)
	}
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

// SaveConnection 新建或更新连接, 返回保存后的连接。已有连接存回它所在的空间, 新建的进当前空间。
func (a *App) SaveConnection(c conn.Connection) (conn.Connection, error) {
	if err := a.ready(); err != nil {
		return c, err
	}
	sp := a.currentSpace()
	if c.ID != "" {
		if _, owner, err := a.findConn(c.ID); err == nil {
			sp = owner
		}
	}
	saved, err := sp.SaveConnection(c)
	if err != nil {
		return saved, err
	}
	// 连接配置变了, 已建立的进程内连接可能指向旧主机或旧凭据。
	a.pool.Drop(saved.ID)
	return saved, nil
}

// DuplicateConnection 复制一条连接, 名称加后缀。
func (a *App) DuplicateConnection(id string) (conn.Connection, error) {
	if err := a.ready(); err != nil {
		return conn.Connection{}, err
	}
	c, _, err := a.findConn(id)
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
	_, sp, err := a.findConn(id)
	if err != nil {
		return err
	}
	if err := sp.DeleteConnection(id); err != nil {
		return err
	}
	a.pool.Drop(id)
	a.facts.Delete(id)
	return nil
}
