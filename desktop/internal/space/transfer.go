package space

import (
	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/cred"
	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
	"github.com/woodchen-ink/czlterm/desktop/internal/script"
)

// localCopySuffix 加在搬移时与目标里同 id 但内容不同的那份本机数据名称后。
const localCopySuffix = " (local copy)"

// MoveResult 是一次搬移的结果。Renamed 是目标里已有同 id、内容不同, 另存为新 id 的条数;
// Merged 是目标里已有同 id、内容相同, 只删掉来源的条数。
type MoveResult struct {
	Connections int `json:"connections"`
	Scripts     int `json:"scripts"`
	Credentials int `json:"credentials"`
	Renamed     int `json:"renamed"`
	Merged      int `json:"merged"`
}

// MoveAll 把 from 的全部连接、脚本与凭据搬到 to, 尽量保留 id (跳板关系、机器信息缓存跟着走)。
//
// 目标里已有同 id 的 (旧版两台电脑各有一份同样的本机数据, 另一台已经搬过): 内容相同就只删来源;
// 不同就以新 id 另存一份「(local copy)」, 不覆盖目标里可能更新的版本。
// 先全部写进目标再删来源: 中途失败时来源完好, 再搬一次即可。
func MoveAll(from, to *Space) (MoveResult, error) {
	var res MoveResult
	if from == to {
		return res, errcode.ErrSpaceSame
	}
	if err := from.writable(); err != nil {
		return res, err
	}
	if err := to.writable(); err != nil {
		return res, err
	}
	conns, _ := from.Conns.List()
	scripts, _ := from.Scripts.List()
	creds, _ := from.Creds.List()

	to.mu.Lock()
	// 凭据先搬, 换了 id 的记下来改写连接里的引用
	credIDs := map[string]string{}
	for _, sum := range creds {
		c, err := from.Creds.Get(sum.ID)
		if err != nil {
			to.mu.Unlock()
			return res, err
		}
		id, err := moveOne(to, CollCredentials, c.ID, c, func() any { c.ID, c.Name = conn.NewID(), c.Name+localCopySuffix; return c }, &res)
		if err != nil {
			to.mu.Unlock()
			return res, err
		}
		credIDs[sum.ID] = id
	}
	for _, c := range conns {
		if id, ok := credIDs[c.Credential]; ok {
			c.Credential = id
		}
		if _, err := moveOne(to, CollConnections, c.ID, c, func() any { c.ID, c.Name = conn.NewID(), c.Name+localCopySuffix; return c }, &res); err != nil {
			to.mu.Unlock()
			return res, err
		}
	}
	for _, s := range scripts {
		if _, err := moveOne(to, CollScripts, s.ID, s, func() any { s.ID, s.Name = conn.NewID(), s.Name+localCopySuffix; return s }, &res); err != nil {
			to.mu.Unlock()
			return res, err
		}
	}
	to.mu.Unlock()

	from.mu.Lock()
	defer from.mu.Unlock()
	for _, c := range conns {
		if err := from.removeLocked(CollConnections, c.ID); err != nil {
			return res, err
		}
	}
	for _, s := range scripts {
		if err := from.removeLocked(CollScripts, s.ID); err != nil {
			return res, err
		}
	}
	for _, c := range creds {
		if err := from.removeLocked(CollCredentials, c.ID); err != nil {
			return res, err
		}
	}
	res.Connections, res.Scripts, res.Credentials = len(conns), len(scripts), len(creds)
	to.poke(CollConnections, "")
	from.poke(CollConnections, "")
	return res, nil
}

// moveOne 把一条文档写进 to (调用方持有 to.mu), 返回它在目标里的 id。
func moveOne(to *Space, collection, id string, v any, renamed func() any, res *MoveResult) (string, error) {
	existing, err := to.read(collection, id)
	if err == nil {
		if sameContent(existing, v) {
			res.Merged++
			return id, nil
		}
		v = renamed()
		res.Renamed++
	}
	if err := to.putLocked(v); err != nil {
		return "", err
	}
	switch d := v.(type) {
	case conn.Connection:
		return d.ID, nil
	case script.Script:
		return d.ID, nil
	case cred.Credential:
		return d.ID, nil
	}
	return id, nil
}

// sameContent 比较两份同类文档, 忽略更新时间。
func sameContent(a, b any) bool {
	switch x := a.(type) {
	case conn.Connection:
		y, ok := b.(conn.Connection)
		x.UpdatedAt, y.UpdatedAt = "", ""
		return ok && x == y
	case script.Script:
		y, ok := b.(script.Script)
		x.UpdatedAt, y.UpdatedAt = "", ""
		return ok && x == y
	case cred.Credential:
		y, ok := b.(cred.Credential)
		x.UpdatedAt, y.UpdatedAt = "", ""
		return ok && x == y
	}
	return false
}

// CopyConnection 把一条连接复制到 to, 换新 id; 它的跳板链与引用的凭据一并复制 (各自换新 id 并改写引用),
// 目标空间里不会出现指向来源空间的引用。返回新连接。
func CopyConnection(from, to *Space, id string) (conn.Connection, error) {
	if from == to {
		return conn.Connection{}, errcode.ErrSpaceSame
	}
	if err := to.writable(); err != nil {
		return conn.Connection{}, err
	}
	c, err := from.Conns.Get(id)
	if err != nil {
		return c, err
	}
	chain := []conn.Connection{c}
	for cur := c; cur.Jump != "" && len(chain) <= conn.MaxJumpDepth; {
		j, err := from.Conns.Get(cur.Jump)
		if err != nil {
			break
		}
		chain = append(chain, j)
		cur = j
	}
	to.mu.Lock()
	defer to.mu.Unlock()
	credIDs := map[string]string{}
	// 从链尾往前写: 先有跳板机, 再有引用它的连接。
	next := ""
	for i := len(chain) - 1; i >= 0; i-- {
		cp := chain[i]
		if cp.Auth == conn.AuthStored && cp.Credential != "" {
			newCred, ok := credIDs[cp.Credential]
			if !ok {
				src, err := from.Creds.Get(cp.Credential)
				if err != nil {
					return c, err
				}
				src.ID = conn.NewID()
				if err := to.putLocked(src); err != nil {
					return c, err
				}
				newCred = src.ID
				credIDs[cp.Credential] = newCred
			}
			cp.Credential = newCred
		}
		cp.ID, cp.Jump = conn.NewID(), next
		if err := to.putLocked(cp); err != nil {
			return c, err
		}
		if to.trust != nil {
			to.trust.set(cp)
		}
		next = cp.ID
		chain[i] = cp
	}
	go to.poke(CollConnections, chain[0].ID)
	return to.Conns.Get(chain[0].ID)
}

// CopyScript 把一条脚本复制到 to, 换新 id。
func CopyScript(from, to *Space, id string) error {
	if from == to {
		return errcode.ErrSpaceSame
	}
	if err := to.writable(); err != nil {
		return err
	}
	s, err := from.Scripts.Get(id)
	if err != nil {
		return err
	}
	s.ID = conn.NewID()
	to.mu.Lock()
	err = to.putLocked(s)
	to.mu.Unlock()
	if err == nil {
		to.poke(CollScripts, s.ID)
	}
	return err
}
