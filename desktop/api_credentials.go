package main

// 保存的凭据 (不用 Vaultwarden 时的用户名、密码与 SSH 私钥) 绑定。凭据按空间存放、随空间同步,
// 秘密只在新建与修改时从界面传进来, 列表与编辑只回摘要, 不把密码和私钥送回界面。

import (
	"strings"

	"github.com/woodchen-ink/czlterm/desktop/internal/agent"
	"github.com/woodchen-ink/czlterm/desktop/internal/cred"
)

// CredentialList 是当前空间的凭据摘要。
type CredentialList struct {
	Space    string         `json:"space"`
	ReadOnly bool           `json:"readOnly"`
	Shared   bool           `json:"shared"`
	Items    []cred.Summary `json:"items"`
	Warnings []string       `json:"warnings"`
}

// ListCredentials 返回当前空间的凭据摘要。Shared 为 true 时界面提示团队成员都能用这些凭据。
func (a *App) ListCredentials() (CredentialList, error) {
	if err := a.ready(); err != nil {
		return CredentialList{}, err
	}
	sp := a.currentSpace()
	list, errs := sp.Creds.List()
	out := CredentialList{Space: sp.Key, ReadOnly: sp.ReadOnly(), Shared: sp.Shared(), Items: list, Warnings: []string{}}
	for _, e := range errs {
		out.Warnings = append(out.Warnings, e.Error())
	}
	return out, nil
}

// CredentialInput 是新建或修改凭据的表单。修改时秘密字段留空表示保持原值, 对应的 Clear 为 true 表示清除。
type CredentialInput struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Username   string `json:"username"`
	Notes      string `json:"notes"`
	Password   string `json:"password"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`
	// ClearPassword / ClearKey 修改时清掉已存的密码 / 私钥 (连同口令)
	ClearPassword bool `json:"clearPassword"`
	ClearKey      bool `json:"clearKey"`
}

// SaveCredential 新建或修改当前空间里的一条凭据。私钥在保存前解析一遍, 格式不对或口令不对当场报错,
// 不等到连接时才发现。
func (a *App) SaveCredential(in CredentialInput) (cred.Summary, error) {
	if err := a.ready(); err != nil {
		return cred.Summary{}, err
	}
	sp := a.currentSpace()
	c := cred.Credential{ID: in.ID, Name: in.Name, Username: in.Username, Notes: in.Notes}
	if in.ID != "" {
		old, err := sp.Creds.Get(in.ID)
		if err != nil {
			return cred.Summary{}, err
		}
		c.Password, c.PrivateKey, c.Passphrase = old.Password, old.PrivateKey, old.Passphrase
	}
	if in.ClearPassword {
		c.Password = ""
	}
	if in.ClearKey {
		c.PrivateKey, c.Passphrase = "", ""
	}
	if in.Password != "" {
		c.Password = in.Password
	}
	if strings.TrimSpace(in.PrivateKey) != "" {
		c.PrivateKey, c.Passphrase = in.PrivateKey, in.Passphrase
	} else if in.Passphrase != "" && c.PrivateKey != "" {
		c.Passphrase = in.Passphrase
	}
	if c.PrivateKey != "" {
		if _, err := agent.ParseKey(c.PrivateKey, c.Passphrase); err != nil {
			return cred.Summary{}, err
		}
	}
	return sp.SaveCredential(c)
}

// DeleteCredential 删除当前空间里的凭据, 仍被连接引用时拒绝 (2312)。
func (a *App) DeleteCredential(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.currentSpace().DeleteCredential(id)
}

// ConfirmConnection 记下本人确认了一条团队连接的当前内容 (主机、凭据引用、跳板), 之后可以连接。
// 跳板链上需要确认的各跳一并确认: 界面上展示的是整条链。
func (a *App) ConfirmConnection(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	c, sp, err := a.findConn(id)
	if err != nil {
		return err
	}
	for depth := 0; depth <= 3; depth++ {
		sp.Confirm(c)
		if c.Jump == "" {
			break
		}
		if c, err = sp.Conns.Get(c.Jump); err != nil {
			break
		}
	}
	return nil
}

// ConnectionReview 是确认框里展示的一条团队连接 (不含秘密)。
type ConnectionReview struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Auth     string `json:"auth"`
	// Credential 是引用的凭据或保险库条目的名字
	Credential string `json:"credential"`
	Jump       string `json:"jump"`
}

// ReviewConnection 返回待确认连接及其跳板链的内容, 供确认框展示「会把凭据交给哪台机器」。
func (a *App) ReviewConnection(id string) ([]ConnectionReview, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	c, sp, err := a.findConn(id)
	if err != nil {
		return nil, err
	}
	var out []ConnectionReview
	for depth := 0; depth <= 3; depth++ {
		r := ConnectionReview{ID: c.ID, Name: c.Name, Protocol: c.Protocol, Host: c.Host, Port: c.Port,
			Username: c.Username, Auth: c.Auth}
		switch c.Auth {
		case "stored":
			if s, err := sp.Creds.Get(c.Credential); err == nil {
				r.Credential = s.Name
			}
		case "vault":
			r.Credential = strings.Trim(a.itemName(c.VaultItem)+" "+a.itemName(c.KeyItem), " ")
		}
		if c.Jump != "" {
			j, err := sp.Conns.Get(c.Jump)
			if err == nil {
				r.Jump = j.Name
			}
		}
		out = append(out, r)
		if c.Jump == "" {
			break
		}
		if c, err = sp.Conns.Get(c.Jump); err != nil {
			break
		}
	}
	return out, nil
}
