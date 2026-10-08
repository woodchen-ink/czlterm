package main

import (
	"fmt"
	"os/user"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/woodchen-ink/czlterm/desktop/internal/agent"
	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/sshx"
)

// hop 是解析完凭据的一跳。
type hop struct {
	conn       conn.Connection
	user       string
	password   string
	privateKey string
	passphrase string
}

// resolveChain 返回从最外层跳板机到目标机的各跳, 并从保险库取出每一跳的凭据。
// 任何一跳用保险库凭据而保险库未解锁时返回 vault.ErrLocked, 界面据此弹出解锁框后重试。
func (a *App) resolveChain(target conn.Connection) ([]hop, error) {
	var chain []conn.Connection
	cur := target
	for {
		chain = append([]conn.Connection{cur}, chain...)
		if cur.Jump == "" {
			break
		}
		if len(chain) > conn.MaxJumpDepth {
			return nil, fmt.Errorf("2019 jump chain longer than %d", conn.MaxJumpDepth)
		}
		next, err := a.conns.Get(cur.Jump)
		if err != nil {
			return nil, fmt.Errorf("2021 jump host of %q not found", cur.Name)
		}
		if next.ID == target.ID {
			return nil, fmt.Errorf("2020 jump chain has a loop")
		}
		cur = next
	}

	hops := make([]hop, 0, len(chain))
	for _, c := range chain {
		h := hop{conn: c, user: c.Username}
		if c.Auth == conn.AuthVault {
			if c.VaultItem != "" {
				s, err := a.vault.Secret(c.VaultItem)
				if err != nil {
					return nil, err
				}
				h.password = s.Password
				if h.user == "" {
					h.user = s.Username
				}
			}
			if c.KeyItem != "" {
				s, err := a.vault.Secret(c.KeyItem)
				if err != nil {
					return nil, err
				}
				h.privateKey, h.passphrase = s.PrivateKey, s.Passphrase
				if h.user == "" {
					h.user = s.Username
				}
			}
		}
		hops = append(hops, h)
	}
	return hops, nil
}

// agentKeys 收集链上来自保险库的私钥。
func agentKeys(hops []hop) []agent.Key {
	var keys []agent.Key
	for _, h := range hops {
		if h.privateKey != "" {
			keys = append(keys, agent.Key{PrivateKey: h.privateKey, Passphrase: h.passphrase, Comment: "czlterm:" + h.conn.Name})
		}
	}
	return keys
}

// sshHops 把解析结果转成进程内 SSH 客户端用的链路。
func sshHops(hops []hop) ([]sshx.Hop, error) {
	out := make([]sshx.Hop, 0, len(hops))
	for _, h := range hops {
		sh := sshx.Hop{Host: h.conn.Host, Port: h.conn.Port, User: h.user, Password: h.password}
		if h.privateKey != "" {
			raw, err := agent.ParseKey(h.privateKey, h.passphrase)
			if err != nil {
				return nil, err
			}
			signer, err := ssh.NewSignerFromKey(raw)
			if err != nil {
				return nil, fmt.Errorf("4101 unsupported private key: %w", err)
			}
			sh.Signers = []ssh.Signer{signer}
		}
		out = append(out, sh)
	}
	return out, nil
}

// localUsername 是 ssh 在未指定用户名时使用的本机用户名, 用于匹配 askpass 提示语。
func localUsername() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	name := u.Username
	if i := strings.LastIndexByte(name, '\\'); i >= 0 {
		name = name[i+1:]
	}
	return name
}
