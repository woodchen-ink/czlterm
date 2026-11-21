package space

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// 集合名, 与 CZL Connect 应用后台登记的一致。
const (
	CollConnections = "connections"
	CollScripts     = "scripts"
	CollCredentials = "credentials"
)

// DocState 是一份文档在本机的同步状态。
type DocState struct {
	// Rev 是本地内容对应的服务端版本, 从未上传过为 0。
	Rev int64 `json:"rev"`
	// Dirty 是出站序号, 0 表示没有待上传的修改; 每次本地修改换新序号, 推送按序号先后。
	Dirty int64 `json:"dirty,omitempty"`
	// Deleted 表示本地已删除 (墓碑): Dirty > 0 时待上传删除, 否则只记着版本, 拉到旧版本时不复活。
	Deleted bool `json:"deleted,omitempty"`
}

// Pending 是一条待上传的修改。
type Pending struct {
	Collection string
	ID         string
	DocState
}

// metaFile 是 `<空间目录>/sync.json` 的内容。
type metaFile struct {
	// Wire 是游标对应的线上格式版本; 本机升级了格式时归零重拉。
	Wire   int                 `json:"wire"`
	Cursor int64               `json:"cursor"`
	Seq    int64               `json:"seq"`
	Docs   map[string]DocState `json:"docs"`
}

// meta 管一个同步空间的版本、出站队列与拉取游标。整份放内存, 每次改动整体落盘 (个人规模几百条)。
type meta struct {
	path string
	mu   sync.Mutex
	f    metaFile
}

func docKey(collection, id string) string { return collection + "/" + id }

func splitKey(k string) (string, string) {
	for i := 0; i < len(k); i++ {
		if k[i] == '/' {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}

func openMeta(dir string) (*meta, error) {
	m := &meta{path: filepath.Join(dir, "sync.json"), f: metaFile{Docs: map[string]DocState{}}}
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("2201 read sync state: %w", err)
	}
	if err := json.Unmarshal(data, &m.f); err != nil {
		// 状态文件坏了: 当作从未同步, 全量重拉; 本地文件都在, 有未上传修改的会以冲突副本保住。
		m.f = metaFile{}
	}
	if m.f.Docs == nil {
		m.f.Docs = map[string]DocState{}
	}
	return m, nil
}

// save 落盘, 调用方持有 mu。
func (m *meta) save() error {
	data, err := json.Marshal(m.f)
	if err != nil {
		return fmt.Errorf("2202 encode sync state: %w", err)
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("2203 write sync state: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("2203 write sync state: %w", err)
	}
	return nil
}

// markDirty 记下一次本地修改, 返回新的出站序号。
func (m *meta) markDirty(collection, id string, deleted bool) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.f.Seq++
	st := m.f.Docs[docKey(collection, id)]
	st.Dirty, st.Deleted = m.f.Seq, deleted
	m.f.Docs[docKey(collection, id)] = st
	return m.f.Seq, m.save()
}

// state 返回文档状态, 从未记录过返回 nil。
func (m *meta) state(collection, id string) *DocState {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.f.Docs[docKey(collection, id)]
	if !ok {
		return nil
	}
	return &st
}

// pending 按出站序号升序返回待上传的修改, limit <= 0 为不限。
func (m *meta) pending(limit int) []Pending {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Pending
	for k, st := range m.f.Docs {
		if st.Dirty > 0 {
			c, id := splitKey(k)
			out = append(out, Pending{Collection: c, ID: id, DocState: st})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dirty < out[j].Dirty })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (m *meta) pendingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, st := range m.f.Docs {
		if st.Dirty > 0 {
			n++
		}
	}
	return n
}

// ack 在序号为 seq 的修改上传成功 (服务端版本 newRev) 后调用。期间又改过 (序号变了) 时保留新修改,
// 它若是基于同一版本做的, 基线改为 newRev。
func (m *meta) ack(collection, id string, seq, baseRev, newRev int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := docKey(collection, id)
	st, ok := m.f.Docs[k]
	switch {
	case !ok:
		return nil
	case st.Dirty == seq:
		st.Dirty, st.Rev = 0, newRev
	case st.Rev == baseRev:
		st.Rev = newRev
	default:
		return nil
	}
	m.f.Docs[k] = st
	return m.save()
}

// rebase 把待上传修改的基线改为 newBase, 用于以本地覆盖服务端。
func (m *meta) rebase(collection, id string, seq, baseRev, newBase int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := docKey(collection, id)
	st, ok := m.f.Docs[k]
	if !ok || (st.Dirty != seq && st.Rev != baseRev) {
		return nil
	}
	st.Rev = newBase
	m.f.Docs[k] = st
	return m.save()
}

// drop 放弃序号为 seq 的待上传修改 (冲突时采用服务端版本、服务端永久拒绝)。
func (m *meta) drop(collection, id string, seq int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := docKey(collection, id)
	st, ok := m.f.Docs[k]
	if !ok || st.Dirty != seq {
		return nil
	}
	st.Dirty = 0
	m.f.Docs[k] = st
	return m.save()
}

// commit 记下服务端接受写入后的版本 (Ack 已清过出站序号)。
func (m *meta) commit(collection, id string, rev int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := docKey(collection, id)
	st := m.f.Docs[k]
	if st.Dirty == 0 && rev > st.Rev {
		st.Rev = rev
		m.f.Docs[k] = st
		return m.save()
	}
	return nil
}

// applied 记下拉取落地后的状态: 内容已是服务端版本 rev, 没有待上传修改。
func (m *meta) applied(collection, id string, rev int64, deleted bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.f.Docs[docKey(collection, id)] = DocState{Rev: rev, Deleted: deleted}
	return m.save()
}

// forgetRev 把文档基线清零 (服务端拒收的修改放弃后, 下次全量拉取用服务端版本改回来)。
func (m *meta) forgetRev(collection, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := docKey(collection, id)
	if _, ok := m.f.Docs[k]; !ok {
		return nil
	}
	m.f.Docs[k] = DocState{}
	return m.save()
}

func (m *meta) cursor(wire int) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.f.Wire != wire {
		return 0
	}
	return m.f.Cursor
}

func (m *meta) setCursor(cursor int64, wire int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.f.Cursor, m.f.Wire = cursor, wire
	return m.save()
}

// reset 丢掉全部没有待上传修改的记录, 返回这些文档 (调用方删掉本地文件后从 0 重拉)。
func (m *meta) reset() ([]Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var clean []Pending
	for k, st := range m.f.Docs {
		if st.Dirty == 0 {
			c, id := splitKey(k)
			clean = append(clean, Pending{Collection: c, ID: id, DocState: st})
			delete(m.f.Docs, k)
		}
	}
	m.f.Cursor = 0
	return clean, m.save()
}
