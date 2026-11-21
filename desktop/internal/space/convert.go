package space

import (
	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/cred"
)

// VaultReader 按连接引用的 Vaultwarden 条目 (密码条目 vaultItem、私钥条目 keyItem, 可只有其一)
// 读出凭据内容, 由调用方接保险库。读不到 (条目已删、缺密码或私钥) 返回错误, 该连接保留原引用。
type VaultReader func(vaultItem, keyItem string) (cred.Credential, error)

// ConvertResult 是一次 Vaultwarden 凭据转换的结果。
type ConvertResult struct {
	// Converted 改为引用保存的凭据的连接数; Credentials 新建的凭据数 (共用同一条目的连接共用一条凭据)
	Converted   int `json:"converted"`
	Credentials int `json:"credentials"`
	// Skipped 读不到条目、保留 Vaultwarden 引用的连接数
	Skipped int `json:"skipped"`
}

// NeedsVault 报告 ids (连同各自的跳板链) 里是否有引用 Vaultwarden 的连接; ids 为空表示整个空间。
func NeedsVault(sp *Space, ids []string) bool {
	for _, c := range vaultConns(sp, ids) {
		if c.Auth == conn.AuthVault {
			return true
		}
	}
	return false
}

// ConvertVault 把 sp 里 ids (连同各自的跳板链; 为空表示整个空间) 中引用 Vaultwarden 的连接
// 改为引用保存的凭据: 读出条目内容存成本空间的凭据, 再改写连接。凭据随空间端到端加密同步,
// 别的电脑与团队成员不需要 Vaultwarden 也能连接。
func ConvertVault(sp *Space, ids []string, read VaultReader) (ConvertResult, error) {
	var res ConvertResult
	if err := sp.writable(); err != nil {
		return res, err
	}
	made := map[string]string{} // vaultItem|keyItem → 凭据 id
	for _, c := range vaultConns(sp, ids) {
		if c.Auth != conn.AuthVault {
			continue
		}
		key := c.VaultItem + "|" + c.KeyItem
		id, ok := made[key]
		if !ok {
			cr, err := read(c.VaultItem, c.KeyItem)
			if err != nil {
				res.Skipped++
				continue
			}
			sum, err := sp.SaveCredential(cr)
			if err != nil {
				return res, err
			}
			id = sum.ID
			made[key] = id
			res.Credentials++
		}
		c.Auth, c.Credential, c.VaultItem, c.KeyItem = conn.AuthStored, id, "", ""
		if _, err := sp.SaveConnection(c); err != nil {
			return res, err
		}
		res.Converted++
	}
	return res, nil
}

// vaultConns 取 ids 与各自跳板链上的连接 (去重, 跳板在前); ids 为空取整个空间。
func vaultConns(sp *Space, ids []string) []conn.Connection {
	if len(ids) == 0 {
		list, _ := sp.Conns.List()
		return list
	}
	seen := map[string]bool{}
	var out []conn.Connection
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		if id == "" || seen[id] || depth > conn.MaxJumpDepth {
			return
		}
		c, err := sp.Conns.Get(id)
		if err != nil {
			return
		}
		seen[id] = true
		walk(c.Jump, depth+1)
		out = append(out, c)
	}
	for _, id := range ids {
		walk(id, 0)
	}
	return out
}
