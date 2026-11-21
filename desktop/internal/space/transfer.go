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
	out, err := CopyConnections(from, to, []string{id})
	if err != nil {
		return conn.Connection{}, err
	}
	return out[0], nil
}

// CopyGroup 把 from 里分组 (文件夹) 为 group 的全部连接复制到 to, 文件夹名与彼此的跳板关系保持不变。
// group 为空表示未分组的连接。返回复制的连接 (不含为跳板一并带过去的组外连接)。
func CopyGroup(from, to *Space, group string) ([]conn.Connection, error) {
	list, _ := from.Conns.List()
	var ids []string
	for _, c := range list {
		if c.Group == group {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return nil, errcode.ErrGroupEmpty
	}
	return CopyConnections(from, to, ids)
}

// CopyConnections 把一批连接复制到 to, 各换新 id。同一批里共用的跳板机与凭据只复制一份,
// 批内互为跳板的关系改写到新 id; 批外的跳板机一并复制过去。返回与 ids 同序的新连接。
func CopyConnections(from, to *Space, ids []string) ([]conn.Connection, error) {
	if from == to {
		return nil, errcode.ErrSpaceSame
	}
	if err := to.writable(); err != nil {
		return nil, err
	}
	to.mu.Lock()
	defer to.mu.Unlock()

	connIDs := map[string]string{}
	credIDs := map[string]string{}
	var written []string
	// copyOne 先复制跳板机再写引用它的连接; 跳板缺失时照旧链断在这里 (与单条复制一致)。
	var copyOne func(id string, depth int) (string, error)
	copyOne = func(id string, depth int) (string, error) {
		if nid, ok := connIDs[id]; ok {
			return nid, nil
		}
		c, err := from.Conns.Get(id)
		if err != nil {
			return "", err
		}
		next := ""
		if c.Jump != "" && depth < conn.MaxJumpDepth {
			if j, err := copyOne(c.Jump, depth+1); err == nil {
				next = j
			}
		}
		if c.Auth == conn.AuthStored && c.Credential != "" {
			nc, ok := credIDs[c.Credential]
			if !ok {
				src, err := from.Creds.Get(c.Credential)
				if err != nil {
					return "", err
				}
				src.ID = conn.NewID()
				if err := to.putLocked(src); err != nil {
					return "", err
				}
				nc = src.ID
				credIDs[c.Credential] = nc
			}
			c.Credential = nc
		}
		c.ID, c.Jump = conn.NewID(), next
		if err := to.putLocked(c); err != nil {
			return "", err
		}
		if to.trust != nil {
			to.trust.set(c)
		}
		connIDs[id] = c.ID
		written = append(written, c.ID)
		return c.ID, nil
	}

	newIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		nid, err := copyOne(id, 0)
		if err != nil {
			return nil, err
		}
		newIDs = append(newIDs, nid)
	}
	go func() {
		for _, id := range written {
			to.poke(CollConnections, id)
		}
	}()
	out := make([]conn.Connection, 0, len(newIDs))
	for _, id := range newIDs {
		c, err := to.Conns.Get(id)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
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
