package conn

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store 读写 `<data>/connections/*.json`。
//
// 不做内存缓存: 同步 (git pull) 会在进程外改文件, 每次读盘最简单也不会读到旧数据;
// 个人规模下几百个小文件的读取耗时可以忽略。
type Store struct {
	dir string
	mu  sync.Mutex
	// writeMu 串行化写入与 git 同步: rebase 进行中写入的文件会被 rebase --abort 一起回滚掉。
	writeMu sync.Mutex
}

// Exclusive 在没有写入进行时执行 fn, 期间的 Save / Delete 等待。读不受影响。
func (s *Store) Exclusive(fn func() error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return fn()
}

// Open 打开数据目录下的连接存储。
func Open(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "connections")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("2010 create connections dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// List 返回全部连接, 按分组、名称排序。单个文件损坏时跳过并在 errs 中报告, 不让一个坏文件挡住整个列表。
func (s *Store) List() (list []Connection, errs []error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return []Connection{}, []error{fmt.Errorf("2011 read connections dir: %w", err)}
	}
	list = []Connection{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		c, err := s.read(strings.TrimSuffix(name, ".json"))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Group != list[j].Group {
			return list[i].Group < list[j].Group
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return list, errs
}

// Get 按 ID 读取连接。
func (s *Store) Get(id string) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(id)
}

// FindByName 按名称 (不区分大小写) 或 ID 查找, 供 MCP 按人类可读的名字定位连接。
func (s *Store) FindByName(name string) (Connection, error) {
	list, _ := s.List()
	for _, c := range list {
		if c.ID == name {
			return c, nil
		}
	}
	var hit []Connection
	for _, c := range list {
		if strings.EqualFold(c.Name, name) {
			hit = append(hit, c)
		}
	}
	switch len(hit) {
	case 1:
		return hit[0], nil
	case 0:
		return Connection{}, fmt.Errorf("2012 connection %q not found", name)
	default:
		return Connection{}, fmt.Errorf("2013 connection name %q is ambiguous, use the id", name)
	}
}

// Save 校验并写入连接, ID 为空时分配新 ID。返回写入后的连接。
func (s *Store) Save(c Connection) (Connection, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	if !ValidID(c.ID) {
		return c, fmt.Errorf("2014 invalid connection id")
	}
	if err := c.Normalize(); err != nil {
		return c, err
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()

	if c.Jump != "" {
		if err := s.checkJumpChain(c); err != nil {
			return c, err
		}
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return c, fmt.Errorf("2015 encode connection: %w", err)
	}
	data = append(data, '\n')
	// 先写临时文件再改名, 写到一半崩溃不会留下半截 JSON。
	path := s.path(c.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return c, fmt.Errorf("2016 write connection: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return c, fmt.Errorf("2016 write connection: %w", err)
	}
	return c, nil
}

// Delete 删除连接。仍被其他连接当作跳板机时拒绝删除。
func (s *Store) Delete(id string) error {
	if !ValidID(id) {
		return fmt.Errorf("2014 invalid connection id")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	list, _ := s.List()
	for _, c := range list {
		if c.Jump == id {
			return fmt.Errorf("2017 connection is used as jump host by %q", c.Name)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("2018 delete connection: %w", err)
	}
	return nil
}

// MaxJumpDepth 是跳板链的最大长度, 防止配置成环或过长的链。
const MaxJumpDepth = 3

// checkJumpChain 确认跳板链存在、都是 SSH、无环且不超过 MaxJumpDepth。调用方持有锁。
func (s *Store) checkJumpChain(c Connection) error {
	seen := map[string]bool{c.ID: true}
	next := c.Jump
	for depth := 1; next != ""; depth++ {
		if depth > MaxJumpDepth {
			return fmt.Errorf("2019 jump chain longer than %d", MaxJumpDepth)
		}
		if seen[next] {
			return fmt.Errorf("2020 jump chain has a loop")
		}
		seen[next] = true
		j, err := s.read(next)
		if err != nil {
			return fmt.Errorf("2021 jump host not found")
		}
		if j.Protocol != ProtoSSH {
			return fmt.Errorf("2022 jump host must be an ssh connection")
		}
		next = j.Jump
	}
	return nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *Store) read(id string) (Connection, error) {
	var c Connection
	if !ValidID(id) {
		return c, fmt.Errorf("2014 invalid connection id")
	}
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return c, fmt.Errorf("2012 connection %q not found", id)
	}
	if err != nil {
		return c, fmt.Errorf("2023 read connection: %w", err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("2024 parse connection %s: %w", id, err)
	}
	// 文件名才是权威 ID: 手工复制文件后忘了改 id 字段时, 不让两个文件互相覆盖。
	c.ID = id
	return c, nil
}
