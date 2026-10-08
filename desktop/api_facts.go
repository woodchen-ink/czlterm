package main

import (
	"context"
	"errors"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/facts"
)

// GetFacts 返回最近一次采集的机器信息, 从未采集时为零值。
func (a *App) GetFacts(id string) (facts.Facts, error) {
	if err := a.ready(); err != nil {
		return facts.Facts{}, err
	}
	return a.facts.Get(id), nil
}

// RefreshFacts 立即重新采集机器信息。只支持 SSH 连接。
func (a *App) RefreshFacts(id string) (facts.Facts, error) {
	if err := a.ready(); err != nil {
		return facts.Facts{}, err
	}
	c, err := a.conns.Get(id)
	if err != nil {
		return facts.Facts{}, err
	}
	if c.Protocol != conn.ProtoSSH {
		return facts.Facts{}, errors.New("5204 system info is only available for ssh connections")
	}
	hops, err := a.resolveChain(c)
	if err != nil {
		return facts.Facts{}, err
	}
	return a.collectFacts(id, hops)
}

// collectFacts 经进程内 SSH 采集并保存, 通知界面刷新; 稳定信息有变化时触发自动同步。
func (a *App) collectFacts(id string, hops []hop) (facts.Facts, error) {
	chain, err := sshHops(hops)
	if err != nil {
		return facts.Facts{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, err := a.pool.Client(ctx, id, chain)
	if err != nil {
		return facts.Facts{}, err
	}
	f, err := facts.Collect(ctx, client)
	if err != nil {
		return f, err
	}
	changed, err := a.facts.Save(id, f)
	if err != nil {
		return f, err
	}
	if changed {
		a.scheduleAutoSync()
	}
	wruntime.EventsEmit(a.ctx, eventFacts, id)
	return f, nil
}
