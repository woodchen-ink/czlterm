package agent

import (
	"bytes"
	"errors"
	"os"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// chainAgent 先用本端点的 keyring, 再透传到用户原有的系统 agent。
//
// 设了 SSH_AUTH_SOCK 后整条跳板链都只看得到本端点; 不透传的话, 凭据设为"系统默认"的那一跳
// (用 1Password / 系统 agent 里的密钥) 会认证失败。写操作 (Add / Remove / Lock) 只作用于本端点,
// 不去改用户的系统 agent。
type chainAgent struct {
	local sshagent.ExtendedAgent
}

var _ sshagent.ExtendedAgent = (*chainAgent)(nil)

// upstream 连接用户原有的 agent, 没有时返回 nil。每次调用新建连接: 调用频率很低, 不值得维护长连接。
func upstream() (sshagent.ExtendedAgent, func()) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		sock = defaultUpstream
	}
	if sock == "" {
		return nil, func() {}
	}
	c, err := dialUpstream(sock)
	if err != nil {
		return nil, func() {}
	}
	return sshagent.NewClient(c), func() { c.Close() }
}

func (a *chainAgent) List() ([]*sshagent.Key, error) {
	keys, err := a.local.List()
	if err != nil {
		return nil, err
	}
	if up, done := upstream(); up != nil {
		defer done()
		if more, err := up.List(); err == nil {
			keys = append(keys, more...)
		}
	}
	return keys, nil
}

func (a *chainAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return a.SignWithFlags(key, data, 0)
}

func (a *chainAgent) SignWithFlags(key ssh.PublicKey, data []byte, flags sshagent.SignatureFlags) (*ssh.Signature, error) {
	if a.hasLocal(key) {
		return a.local.SignWithFlags(key, data, flags)
	}
	up, done := upstream()
	defer done()
	if up == nil {
		return nil, errors.New("key not found")
	}
	return up.SignWithFlags(key, data, flags)
}

func (a *chainAgent) hasLocal(key ssh.PublicKey) bool {
	keys, err := a.local.List()
	if err != nil {
		return false
	}
	want := key.Marshal()
	for _, k := range keys {
		if bytes.Equal(k.Marshal(), want) {
			return true
		}
	}
	return false
}

func (a *chainAgent) Add(key sshagent.AddedKey) error { return a.local.Add(key) }
func (a *chainAgent) Remove(key ssh.PublicKey) error  { return a.local.Remove(key) }
func (a *chainAgent) RemoveAll() error                { return a.local.RemoveAll() }
func (a *chainAgent) Lock(passphrase []byte) error    { return a.local.Lock(passphrase) }
func (a *chainAgent) Unlock(passphrase []byte) error  { return a.local.Unlock(passphrase) }
func (a *chainAgent) Signers() ([]ssh.Signer, error)  { return a.local.Signers() }
func (a *chainAgent) Extension(string, []byte) ([]byte, error) {
	return nil, sshagent.ErrExtensionUnsupported
}
