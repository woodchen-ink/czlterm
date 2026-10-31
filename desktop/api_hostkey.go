package main

import (
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/woodchen-ink/czlterm/desktop/internal/sshx"
)

// onHostKeyChanged 在进程内握手发现主机密钥与 known_hosts 不符时通知界面。
// 终端走系统 ssh 看不到这一步, 但每次打开终端都会经进程内连接采集机器信息, 同样会触发。
func (a *App) onHostKeyChanged(c sshx.HostKeyChange) {
	a.log.Warn("host key changed", "host", c.Host, "type", c.KeyType, "fingerprint", c.Fingerprint)
	wruntime.EventsEmit(a.ctx, eventHostKey, c)
}

// TrustHostKey 用户核实指纹后, 用新密钥替换该主机在 known_hosts 里的记录。
func (a *App) TrustHostKey(host, fingerprint string) error {
	if err := a.pool.TrustHostKey(host, fingerprint); err != nil {
		return err
	}
	a.log.Info("host key trusted", "host", host, "fingerprint", fingerprint)
	return nil
}
