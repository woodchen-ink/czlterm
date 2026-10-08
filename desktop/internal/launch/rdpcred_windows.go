package launch

import "github.com/danieljoos/wincred"

// storeRDPCredential 写入 mstsc 读取的域凭据 TERMSRV/<host>, 返回还原函数。
// 走 API 而不是 cmdkey, 密码不出现在命令行上。用户原先保存过同名凭据时, 还原函数把它写回。
func storeRDPCredential(host, user, password string) (func(), error) {
	target := "TERMSRV/" + host
	backup, _ := wincred.GetDomainPassword(target)

	c := wincred.NewDomainPassword(target)
	c.UserName = user
	c.CredentialBlob = utf16le(password)
	c.Persist = wincred.PersistSession
	if err := c.Write(); err != nil {
		return nil, err
	}
	return func() {
		if backup != nil {
			_ = backup.Write()
			return
		}
		if cur, err := wincred.GetDomainPassword(target); err == nil {
			_ = cur.Delete()
		}
	}, nil
}

// utf16le 按 Windows 凭据管理器的要求把密码编码成 UTF-16LE。
func utf16le(s string) []byte {
	var out []byte
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			hi, lo := 0xd800+(r>>10), 0xdc00+(r&0x3ff)
			out = append(out, byte(hi), byte(hi>>8), byte(lo), byte(lo>>8))
			continue
		}
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}
