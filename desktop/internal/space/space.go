// Package space 把本地数据分成空间: 本机 (永远在、永不上传)、个人云同步、团队。
//
// 每个空间一个目录, 里面的 connections/ scripts/ credentials/ 由 conn.Store / script.Store /
// cred.Store 读写。本机空间就是数据目录本身 (旧版数据原地可用); 同步空间在
// `data/spaces/<摘要>/`, 带 space.json (空间标识) 与 sync.json (版本、出站队列、游标, 见 meta.go)。
//
// 同步空间的写入都经 Space 的方法, 并在空间写锁里完成「查同步状态 + 写文件 + 改同步状态」:
// 本地保存、拉取落地、读出待推送内容、推送确认互相不交错, 不会把刚写的修改当成已上传清掉。
// 本地写入先记出站序号再写文件 (写到一半崩溃最多多传一次旧内容), 再通知云同步尽快推送。
package space

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/cred"
	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
	"github.com/woodchen-ink/czlterm/desktop/internal/script"
)

// Local 是本机空间的标识。
const Local = "local"

// 同步空间标识前缀。个人空间 me:<用户>, 团队 org:<组织>@<用户>: 都带上账号,
// 同一台电脑换账号登录看不到、也推不了上一个人的数据。
const (
	PersonalPrefix = "me:"
	OrgPrefix      = "org:"
)

// ErrLocalChanged 表示拉到的版本没落地: 本地有未上传的修改, 推送时以冲突处理。
var ErrLocalChanged = errors.New("local copy has unpushed changes")

// Space 是一个空间。并发安全。
type Space struct {
	Key     string
	Dir     string
	Conns   *conn.Store
	Scripts *script.Store
	Creds   *cred.Store

	// mu 是空间写锁, 见包注释。读 (列表、Get) 不需要它。
	mu       sync.Mutex
	meta     *meta  // 本机空间为 nil
	trust    *trust // 只有团队空间有, 见 trust.go
	readOnly atomic.Bool
	notify   func(key, collection, id string)
}

// Synced 报告是否是同步空间。
func (s *Space) Synced() bool { return s.meta != nil }

// Shared 报告是否是团队空间 (内容由多人写入)。
func (s *Space) Shared() bool { return strings.HasPrefix(s.Key, OrgPrefix) }

// ReadOnly 报告空间当前是否只读 (查看者、订阅到期、已被移出团队), 由云同步按服务端状态设置。
func (s *Space) ReadOnly() bool { return s.readOnly.Load() }

// SetReadOnly 设置只读。
func (s *Space) SetReadOnly(v bool) { s.readOnly.Store(v) }

func (s *Space) writable() error {
	if s.readOnly.Load() {
		return errcode.ErrSyncReadOnly
	}
	return nil
}

// touch 记下一次本地修改, 调用方持有 mu。
func (s *Space) touch(collection, id string, deleted bool) error {
	if s.meta == nil {
		return nil
	}
	_, err := s.meta.markDirty(collection, id, deleted)
	return err
}

func (s *Space) poke(collection, id string) {
	if s.meta != nil && s.notify != nil {
		s.notify(s.Key, collection, id)
	}
}

// ─── 本地编辑 ───────────────────────────────────────────────────────────────

// SaveConnection 新建或更新连接。本人保存的团队连接同时记为已确认 (见 trust.go)。
func (s *Space) SaveConnection(c conn.Connection) (conn.Connection, error) {
	s.mu.Lock()
	saved, err := s.saveConnectionLocked(c)
	s.mu.Unlock()
	if err == nil {
		s.poke(CollConnections, saved.ID)
	}
	return saved, err
}

func (s *Space) saveConnectionLocked(c conn.Connection) (conn.Connection, error) {
	if err := s.writable(); err != nil {
		return c, err
	}
	if c.ID == "" {
		c.ID = conn.NewID()
	}
	// 先校验再标脏, 字段不合法时不留下多余的出站记录。
	probe := c
	if err := probe.Normalize(); err != nil {
		return c, err
	}
	if c.Auth == conn.AuthStored {
		if _, err := s.Creds.Get(c.Credential); err != nil {
			return c, err
		}
	}
	if err := s.touch(CollConnections, c.ID, false); err != nil {
		return c, err
	}
	saved, err := s.Conns.Save(c)
	if err == nil && s.trust != nil {
		s.trust.set(saved)
	}
	return saved, err
}

// DeleteConnection 删除连接。仍被当作跳板机时拒绝。先记墓碑再删文件, 崩溃时删除仍会上传。
func (s *Space) DeleteConnection(id string) error {
	s.mu.Lock()
	err := s.deleteConnectionLocked(id)
	s.mu.Unlock()
	if err == nil {
		s.poke(CollConnections, id)
	}
	return err
}

func (s *Space) deleteConnectionLocked(id string) error {
	if err := s.writable(); err != nil {
		return err
	}
	if name := s.Conns.UsedAsJump(id); name != "" {
		return fmt.Errorf("2017 connection is used as jump host by %q", name)
	}
	if err := s.touch(CollConnections, id, true); err != nil {
		return err
	}
	return s.Conns.Remove(id)
}

// SaveScript 新建或更新脚本。
func (s *Space) SaveScript(sc script.Script) (script.Script, error) {
	s.mu.Lock()
	saved, err := s.saveScriptLocked(sc)
	s.mu.Unlock()
	if err == nil {
		s.poke(CollScripts, saved.ID)
	}
	return saved, err
}

func (s *Space) saveScriptLocked(sc script.Script) (script.Script, error) {
	if err := s.writable(); err != nil {
		return sc, err
	}
	if sc.ID == "" {
		sc.ID = conn.NewID()
	}
	probe := sc
	if err := probe.Normalize(); err != nil {
		return sc, err
	}
	if err := s.touch(CollScripts, sc.ID, false); err != nil {
		return sc, err
	}
	return s.Scripts.Save(sc)
}

// DeleteScript 删除脚本。
func (s *Space) DeleteScript(id string) error {
	s.mu.Lock()
	err := s.writable()
	if err == nil {
		if err = s.touch(CollScripts, id, true); err == nil {
			err = s.Scripts.Delete(id)
		}
	}
	s.mu.Unlock()
	if err == nil {
		s.poke(CollScripts, id)
	}
	return err
}

// SaveCredential 新建或更新凭据, 返回摘要。
func (s *Space) SaveCredential(c cred.Credential) (cred.Summary, error) {
	s.mu.Lock()
	sum, err := s.saveCredentialLocked(c)
	s.mu.Unlock()
	if err == nil {
		s.poke(CollCredentials, sum.ID)
	}
	return sum, err
}

func (s *Space) saveCredentialLocked(c cred.Credential) (cred.Summary, error) {
	if err := s.writable(); err != nil {
		return cred.Summary{}, err
	}
	if c.ID == "" {
		c.ID = conn.NewID()
	}
	probe := c
	if err := probe.Normalize(); err != nil {
		return cred.Summary{}, err
	}
	if err := s.touch(CollCredentials, c.ID, false); err != nil {
		return cred.Summary{}, err
	}
	sum, err := s.Creds.Save(c)
	if err == nil && s.trust != nil {
		// 凭据内容变了, 引用它的团队连接按本人重新确认过处理 (改的人就是自己)
		list, _ := s.Conns.List()
		for _, x := range list {
			if x.Credential == c.ID {
				s.trust.set(x)
			}
		}
	}
	return sum, err
}

// DeleteCredential 删除凭据。仍被连接引用时拒绝。
func (s *Space) DeleteCredential(id string) error {
	s.mu.Lock()
	err := s.writable()
	if err == nil {
		list, _ := s.Conns.List()
		for _, c := range list {
			if c.Auth == conn.AuthStored && c.Credential == id {
				err = fmt.Errorf("2312 credential is used by connection %q", c.Name)
				break
			}
		}
	}
	if err == nil {
		if err = s.touch(CollCredentials, id, true); err == nil {
			err = s.Creds.Delete(id)
		}
	}
	s.mu.Unlock()
	if err == nil {
		s.poke(CollCredentials, id)
	}
	return err
}

// put 写入一条文档 (搬移、复制、冲突副本) 并标脏, 不查只读、不查跳板链。调用方持有 mu。
func (s *Space) putLocked(v any) error {
	switch d := v.(type) {
	case conn.Connection:
		if err := s.touch(CollConnections, d.ID, false); err != nil {
			return err
		}
		return s.Conns.Put(d)
	case script.Script:
		if err := s.touch(CollScripts, d.ID, false); err != nil {
			return err
		}
		return s.Scripts.Put(d)
	case cred.Credential:
		if err := s.touch(CollCredentials, d.ID, false); err != nil {
			return err
		}
		return s.Creds.Put(d)
	}
	return fmt.Errorf("2204 unknown document %T", v)
}

// removeLocked 删除一条文档并记墓碑, 不查引用。调用方持有 mu。
func (s *Space) removeLocked(collection, id string) error {
	if err := s.touch(collection, id, true); err != nil {
		return err
	}
	switch collection {
	case CollConnections:
		return s.Conns.Remove(id)
	case CollScripts:
		return s.Scripts.Delete(id)
	case CollCredentials:
		return s.Creds.Delete(id)
	}
	return fmt.Errorf("2204 unknown collection %q", collection)
}

// read 读出本地文档, 调用方持有 mu 或只读用途。
func (s *Space) read(collection, id string) (any, error) {
	switch collection {
	case CollConnections:
		return s.Conns.Get(id)
	case CollScripts:
		return s.Scripts.Get(id)
	case CollCredentials:
		return s.Creds.Get(id)
	}
	return nil, fmt.Errorf("2204 unknown collection %q", collection)
}

// ─── 给云同步的出站与落地接口 ─────────────────────────────────────────────

// Outgoing 是一条待推送的修改与它此刻的内容。Missing 为 true 表示文件已不在 (保存失败留下的记录)。
type Outgoing struct {
	Pending
	Data    json.RawMessage
	Missing bool
	Err     error
}

// Outgoing 按出站序号取待推送的修改, 在写锁里同时读出内容: 序号与内容是同一时刻的,
// 推送途中再改会换新序号, Ack 时不会误清。limit <= 0 为不限。
func (s *Space) Outgoing(limit int) []Outgoing {
	if s.meta == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Outgoing
	for _, p := range s.meta.pending(0) {
		if limit > 0 && len(out) >= limit {
			break
		}
		o := Outgoing{Pending: p}
		if !p.Deleted {
			v, err := s.read(p.Collection, p.ID)
			switch {
			case err != nil && strings.Contains(err.Error(), "not found"):
				o.Missing = true
			case err != nil:
				o.Err = err
			default:
				o.Data, o.Err = json.Marshal(v)
			}
		}
		out = append(out, o)
	}
	return out
}

// PendingCount 返回待上传的修改条数。
func (s *Space) PendingCount() int {
	if s.meta == nil {
		return 0
	}
	return s.meta.pendingCount()
}

// State 返回一份文档的同步状态, 本机空间或从未记录返回 nil。
func (s *Space) State(collection, id string) *DocState {
	if s.meta == nil {
		return nil
	}
	return s.meta.state(collection, id)
}

// Apply 让拉到的服务端版本落地。本地有未上传修改时返回 ErrLocalChanged, 不覆盖;
// 本地版本已不旧于 rev 时跳过。内容校验不过返回 errcode.ErrSyncPayload。
func (s *Space) Apply(collection, id string, rev int64, deleted bool, value json.RawMessage) error {
	if s.meta == nil {
		return errcode.ErrSpaceUnknown
	}
	if !conn.ValidID(id) {
		return errcode.ErrSyncPayload.Wrap(fmt.Errorf("invalid id %q", id))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.meta.state(collection, id); st != nil {
		if st.Dirty > 0 {
			return ErrLocalChanged
		}
		if st.Rev >= rev {
			return nil
		}
	}
	var err error
	switch collection {
	case CollConnections:
		if deleted {
			err = s.Conns.Remove(id)
			break
		}
		var c conn.Connection
		if err := json.Unmarshal(value, &c); err != nil {
			return errcode.ErrSyncPayload.Wrap(err)
		}
		c.ID = id
		if err := s.Conns.Put(c); err != nil {
			return errcode.ErrSyncPayload.Wrap(err)
		}
	case CollScripts:
		if deleted {
			err = s.Scripts.Delete(id)
			break
		}
		var sc script.Script
		if err := json.Unmarshal(value, &sc); err != nil {
			return errcode.ErrSyncPayload.Wrap(err)
		}
		sc.ID = id
		if err := s.Scripts.Put(sc); err != nil {
			return errcode.ErrSyncPayload.Wrap(err)
		}
	case CollCredentials:
		if deleted {
			err = s.Creds.Delete(id)
			break
		}
		var c cred.Credential
		if err := json.Unmarshal(value, &c); err != nil {
			return errcode.ErrSyncPayload.Wrap(err)
		}
		c.ID = id
		if err := s.Creds.Put(c); err != nil {
			return errcode.ErrSyncPayload.Wrap(err)
		}
	default:
		// 别的版本新加的集合: 不认识就不落地, 游标照常前进
		return nil
	}
	if err != nil {
		return err
	}
	return s.meta.applied(collection, id, rev, deleted)
}

// Ack / Rebase / Drop / Commit / ForgetRev / Cursor / SetCursor 是 SDK 出站队列与游标的落点。
// 序号比较在 meta 里做: 只有序号仍是推送时那个, 才清脏或丢弃。

func (s *Space) Ack(collection, id string, seq, baseRev, newRev int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta.ack(collection, id, seq, baseRev, newRev)
}

func (s *Space) Rebase(collection, id string, seq, baseRev, newBase int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta.rebase(collection, id, seq, baseRev, newBase)
}

func (s *Space) Drop(collection, id string, seq int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta.drop(collection, id, seq)
}

func (s *Space) Commit(collection, id string, rev int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta.commit(collection, id, rev)
}

func (s *Space) ForgetRev(collection, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta.forgetRev(collection, id)
}

func (s *Space) Cursor(wire int) int64 { return s.meta.cursor(wire) }

func (s *Space) SetCursor(cursor int64, wire int) error { return s.meta.setCursor(cursor, wire) }

// Reset 在游标过期时丢掉全部已同步且没有待上传修改的本地文档, 随后从 0 全量重拉。
func (s *Space) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clean, err := s.meta.reset()
	if err != nil {
		return err
	}
	for _, d := range clean {
		switch d.Collection {
		case CollConnections:
			_ = s.Conns.Remove(d.ID)
		case CollScripts:
			_ = s.Scripts.Delete(d.ID)
		case CollCredentials:
			_ = s.Creds.Delete(d.ID)
		}
	}
	return nil
}

// SaveConflictCopy 把本地修改另存为新 id 的副本 (名称加后缀) 并标脏, 返回新 id。
func (s *Space) SaveConflictCopy(collection, id, suffix string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	newID := conn.NewID()
	v, err := s.read(collection, id)
	if err != nil {
		return "", err
	}
	switch d := v.(type) {
	case conn.Connection:
		d.ID, d.Name = newID, d.Name+suffix
		v = d
	case script.Script:
		d.ID, d.Name = newID, d.Name+suffix
		v = d
	case cred.Credential:
		d.ID, d.Name = newID, d.Name+suffix
		v = d
	}
	return newID, s.putLocked(v)
}
