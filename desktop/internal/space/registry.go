package space

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/cred"
	"github.com/woodchen-ink/czlterm/desktop/internal/errcode"
	"github.com/woodchen-ink/czlterm/desktop/internal/script"
)

// Registry 管本机全部空间。并发安全。
type Registry struct {
	dataDir string

	mu     sync.Mutex
	spaces map[string]*Space
	notify func(key, collection, id string)
	// visible 是当前登录账号能看到的同步空间; 本机空间总可见。没登录时为空:
	// 别的账号留在本机的空间不出现在列表里, 也不被按 id 找到。
	visible map[string]bool
}

// spaceFile 是同步空间目录里的 space.json。
type spaceFile struct {
	Key string `json:"key"`
}

// Open 打开数据目录: 本机空间就是数据目录本身, 同步空间按 `spaces/*/space.json` 载入。
func Open(dataDir string) (*Registry, error) {
	r := &Registry{dataDir: dataDir, spaces: map[string]*Space{}}
	local, err := r.open(Local, dataDir, false)
	if err != nil {
		return nil, err
	}
	r.spaces[Local] = local
	entries, err := os.ReadDir(filepath.Join(dataDir, "spaces"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("2205 read spaces dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(dataDir, "spaces", e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "space.json"))
		if err != nil {
			continue
		}
		var f spaceFile
		if json.Unmarshal(raw, &f) != nil || !IsSynced(f.Key) || dirName(f.Key) != e.Name() {
			continue
		}
		sp, err := r.open(f.Key, dir, true)
		if err != nil {
			// 一个同步空间坏了不连累其它空间与本机数据; 它的内容下次同步时从云端重拉。
			continue
		}
		r.spaces[f.Key] = sp
	}
	return r, nil
}

// IsSynced 判断空间标识是否是同步空间 (me:<用户> / org:<组织>@<用户>)。
func IsSynced(key string) bool {
	for _, p := range []string{PersonalPrefix, OrgPrefix} {
		if rest, ok := strings.CutPrefix(key, p); ok && rest != "" {
			return true
		}
	}
	return false
}

// dirName 由空间标识得到目录名。标识里有用户与组织 id, 不直接落在路径上。
func dirName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:10])
}

func (r *Registry) open(key, dir string, synced bool) (*Space, error) {
	cs, err := conn.Open(dir)
	if err != nil {
		return nil, err
	}
	ss, err := script.Open(dir)
	if err != nil {
		return nil, err
	}
	cr, err := cred.Open(dir)
	if err != nil {
		return nil, err
	}
	sp := &Space{Key: key, Dir: dir, Conns: cs, Scripts: ss, Creds: cr, notify: r.poke}
	if synced {
		if sp.meta, err = openMeta(dir); err != nil {
			return nil, err
		}
	}
	if sp.Shared() {
		sp.trust = openTrust(dir)
	}
	return sp, nil
}

func (r *Registry) poke(key, collection, id string) {
	r.mu.Lock()
	fn := r.notify
	r.mu.Unlock()
	if fn != nil {
		fn(key, collection, id)
	}
}

// OnLocalEdit 设置同步空间本地写入后的回调 (云同步据此尽快推送)。
func (r *Registry) OnLocalEdit(fn func(key, collection, id string)) {
	r.mu.Lock()
	r.notify = fn
	r.mu.Unlock()
}

// Local 返回本机空间。
func (r *Registry) Local() *Space {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.spaces[Local]
}

// Get 返回空间, 本机没有时为 nil。
func (r *Registry) Get(key string) *Space {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.spaces[key]
}

// Ensure 返回同步空间, 没有时建目录。
func (r *Registry) Ensure(key string) (*Space, error) {
	if !IsSynced(key) {
		return nil, errcode.ErrSpaceUnknown
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if sp := r.spaces[key]; sp != nil {
		return sp, nil
	}
	dir := filepath.Join(r.dataDir, "spaces", dirName(key))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("2206 create space dir: %w", err)
	}
	raw, _ := json.Marshal(spaceFile{Key: key})
	if err := os.WriteFile(filepath.Join(dir, "space.json"), raw, 0o600); err != nil {
		return nil, fmt.Errorf("2206 create space dir: %w", err)
	}
	sp, err := r.open(key, dir, true)
	if err != nil {
		return nil, err
	}
	r.spaces[key] = sp
	return sp, nil
}

// Remove 删掉本机一个同步空间的全部数据。调用方先确认用户接受丢掉未上传的修改。
func (r *Registry) Remove(key string) error {
	if !IsSynced(key) {
		return errcode.ErrSpaceUnknown
	}
	r.mu.Lock()
	sp := r.spaces[key]
	delete(r.spaces, key)
	r.mu.Unlock()
	if sp == nil {
		return nil
	}
	if err := os.RemoveAll(sp.Dir); err != nil {
		return fmt.Errorf("2207 delete space: %w", err)
	}
	return nil
}

// SetVisible 设置当前账号可见的同步空间 (登录、空间列表刷新、退出登录时调用)。
func (r *Registry) SetVisible(keys []string) {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	r.mu.Lock()
	r.visible = m
	r.mu.Unlock()
}

// Visible 报告空间对当前账号是否可见。
func (r *Registry) Visible(key string) bool {
	if key == Local {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.visible[key] && r.spaces[key] != nil
}

// All 返回当前可见的空间, 本机在前, 其余按标识排序。
func (r *Registry) All() []*Space {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Space, 0, len(r.spaces))
	for key, sp := range r.spaces {
		if key == Local || r.visible[key] {
			out = append(out, sp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Key == Local) != (out[j].Key == Local) {
			return out[i].Key == Local
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// ordered 返回可见空间, prefer (通常是当前空间) 排在最前。不同空间可能有同 id 的文档
// (旧版两台电脑各有一份本机数据, 一台搬进了云同步), 优先按当前空间解析才不会改错地方。
func (r *Registry) ordered(prefer string) []*Space {
	all := r.All()
	for i, sp := range all {
		if sp.Key == prefer && i > 0 {
			out := append([]*Space{sp}, all[:i]...)
			return append(out, all[i+1:]...)
		}
	}
	return all
}

// FindConnection 在可见空间里按 id 找连接, prefer 空间优先。终端重连、MCP、跳板链按连接 id 定位。
func (r *Registry) FindConnection(prefer, id string) (conn.Connection, *Space, error) {
	var first error
	for _, sp := range r.ordered(prefer) {
		c, err := sp.Conns.Get(id)
		if err == nil {
			return c, sp, nil
		}
		if first == nil {
			first = err
		}
	}
	return conn.Connection{}, nil, first
}

// FindScript 在可见空间里按 id 找脚本, prefer 空间优先。
func (r *Registry) FindScript(prefer, id string) (script.Script, *Space, error) {
	var first error
	for _, sp := range r.ordered(prefer) {
		s, err := sp.Scripts.Get(id)
		if err == nil {
			return s, sp, nil
		}
		if first == nil {
			first = err
		}
	}
	return script.Script{}, nil, first
}
