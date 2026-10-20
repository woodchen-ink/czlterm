package main

import (
	"fmt"
	"os"
	"os/user"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/woodchen-ink/czlterm/desktop/internal/agent"
	"github.com/woodchen-ink/czlterm/desktop/internal/askpass"
	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/launch"
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
			if c.VaultItem != "" && h.password == "" {
				return nil, fmt.Errorf("4107 vault item has no password")
			}
			if c.KeyItem != "" && h.privateKey == "" {
				return nil, fmt.Errorf("4104 vault item has no private key")
			}
			if h.password == "" && h.privateKey == "" {
				return nil, fmt.Errorf("4108 vault credentials are required")
			}
		}
		hops = append(hops, h)
	}
	return hops, nil
}

// sshHops 把解析结果转成进程内 SSH 客户端用的链路。
func sshHops(hops []hop) ([]sshx.Hop, error) {
	out := make([]sshx.Hop, 0, len(hops))
	for _, h := range hops {
		sh := sshx.Hop{Host: h.conn.Host, Port: h.conn.Port, User: h.user, Password: h.password,
			Vault: h.conn.Auth == conn.AuthVault}
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

// terminalHops 给每一跳独立的 agent 和密码令牌, 同主机同用户不同端口也不共享凭据。
func (a *App) terminalHops(hops []hop, exe string) ([]launch.SSHHop, error) {
	out := make([]launch.SSHHop, 0, len(hops))
	for _, h := range hops {
		lh := launch.SSHHop{Host: bracketless(h.conn.Host), Port: h.conn.Port, User: h.user,
			Vault: h.conn.Auth == conn.AuthVault, Password: h.password != "", Env: launch.SSHPasswordEnv()}
		if h.privateKey != "" {
			sock, err := a.agents.Serve(h.conn.ID, []agent.Key{{PrivateKey: h.privateKey,
				Passphrase: h.passphrase, Comment: "czlterm:" + h.conn.Name}})
			if err != nil {
				return nil, err
			}
			lh.Agent = sock
		}
		if h.password != "" {
			u := h.user
			if u == "" {
				u = localUsername()
			}
			lh.Env = append(lh.Env, "SSH_ASKPASS="+exe, "SSH_ASKPASS_REQUIRE=force")
			if os.Getenv("DISPLAY") == "" {
				lh.Env = append(lh.Env, "DISPLAY=:0")
			}
			creds := []askpass.Credential{{Target: u + "@" + bracketless(h.conn.Host), Password: h.password}}
			lh.Env = append(lh.Env, a.askpass.Grant(creds, 1)...)
		}
		out = append(out, lh)
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
