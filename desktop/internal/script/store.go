package script

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
)

// Store 读写 `<data>/scripts/*.json`。与连接存储一样不做内存缓存, 同步会在进程外改文件。
//
// 写入与 git 同步的互斥由调用方负责 (包在 conn.Store.Exclusive 里), 本类型只保证自身读写不交错。
type Store struct {
	dir string
	mu  sync.Mutex
}

// Open 打开数据目录下的脚本存储。
func Open(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "scripts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("2110 create scripts dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// List 返回全部脚本, 按分组、名称排序。单个文件损坏时跳过并在 errs 中报告。
func (s *Store) List() (list []Script, errs []error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return []Script{}, []error{fmt.Errorf("2111 read scripts dir: %w", err)}
	}
	list = []Script{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		sc, err := s.read(strings.TrimSuffix(name, ".json"))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		list = append(list, sc)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Group != list[j].Group {
			return list[i].Group < list[j].Group
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return list, errs
}

// Get 按 ID 读取脚本。
func (s *Store) Get(id string) (Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(id)
}

// Save 校验并写入脚本, ID 为空时分配新 ID。返回写入后的脚本。
func (s *Store) Save(sc Script) (Script, error) {
	if sc.ID == "" {
		sc.ID = conn.NewID()
	}
	if !conn.ValidID(sc.ID) {
		return sc, fmt.Errorf("2114 invalid script id")
	}
	if err := sc.Normalize(); err != nil {
		return sc, err
	}
	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return sc, fmt.Errorf("2115 encode script: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	// 先写临时文件再改名, 写到一半崩溃不会留下半截 JSON。
	path := s.path(sc.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return sc, fmt.Errorf("2116 write script: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return sc, fmt.Errorf("2116 write script: %w", err)
	}
	return sc, nil
}

// Delete 删除脚本, 不存在时视为成功。
func (s *Store) Delete(id string) error {
	if !conn.ValidID(id) {
		return fmt.Errorf("2114 invalid script id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("2118 delete script: %w", err)
	}
	return nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *Store) read(id string) (Script, error) {
	var sc Script
	if !conn.ValidID(id) {
		return sc, fmt.Errorf("2114 invalid script id")
	}
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return sc, fmt.Errorf("2112 script %q not found", id)
	}
	if err != nil {
		return sc, fmt.Errorf("2113 read script: %w", err)
	}
	if err := json.Unmarshal(data, &sc); err != nil {
		return sc, fmt.Errorf("2117 parse script %s: %w", id, err)
	}
	// 文件名才是权威 ID: 手工复制文件后忘了改 id 字段时, 不让两个文件互相覆盖。
	sc.ID = id
	return sc, nil
}
