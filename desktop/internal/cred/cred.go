// Package cred 是不用 Vaultwarden 时的凭据库: 用户名、密码、SSH 私钥与私钥口令。
//
// 每条凭据一个文件 `<空间目录>/credentials/<id>.sec`, 整份 JSON 经 secret.Protect 加密
// (系统钥匙串里的随机 AES 密钥), 不留明文。连接只以 id 引用凭据 (auth=stored), 连接文件里没有秘密。
// 同步空间里的凭据随云同步上传, 线上同样只是端到端加密后的密文; 团队空间里的凭据全体成员可解密。
// 凭据内容不经 MCP, 也不回传给界面 (列表与编辑只给摘要, 改秘密时整项重填)。
package cred

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/woodchen-ink/czlterm/desktop/internal/conn"
	"github.com/woodchen-ink/czlterm/desktop/internal/secret"
)

// 长度上限: 私钥按 16 KB 封顶 (8192 位 RSA 的 PEM 约 6.5 KB), 其余字段 1 KB。
const (
	maxKey   = 16 * 1024
	maxField = 1024
)

// Credential 是一条凭据。Password 与 PrivateKey 至少有一项。
type Credential struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
	// PrivateKey 是 OpenSSH / PEM 文本; Passphrase 是它的口令, 没加密的私钥留空。
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`
	Notes      string `json:"notes"`
	// UpdatedAt 为 RFC3339 UTC 时间。
	UpdatedAt string `json:"updatedAt"`
}

// Summary 是给界面的摘要, 不含任何秘密。
type Summary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Username    string `json:"username"`
	HasPassword bool   `json:"hasPassword"`
	HasKey      bool   `json:"hasKey"`
	Notes       string `json:"notes"`
	UpdatedAt   string `json:"updatedAt"`
}

// Summary 返回摘要。
func (c Credential) Summary() Summary {
	return Summary{ID: c.ID, Name: c.Name, Username: c.Username, HasPassword: c.Password != "",
		HasKey: c.PrivateKey != "", Notes: c.Notes, UpdatedAt: c.UpdatedAt}
}

// Normalize 清理输入并校验, 返回带码的错误。
func (c *Credential) Normalize() error {
	c.Name = strings.TrimSpace(c.Name)
	c.Username = strings.TrimSpace(c.Username)
	c.Notes = strings.TrimSpace(c.Notes)
	// 私钥前后的空白与 Windows 换行规整掉, 末尾补一个换行 (部分 ssh 实现要求)。
	c.PrivateKey = strings.TrimSpace(strings.ReplaceAll(c.PrivateKey, "\r\n", "\n"))
	if c.PrivateKey != "" {
		c.PrivateKey += "\n"
	}
	if c.Password == "" && c.PrivateKey == "" {
		return errors.New("2301 credential needs a password or a private key")
	}
	if len(c.PrivateKey) > maxKey {
		return errors.New("2302 private key is too large")
	}
	for _, f := range []string{c.Name, c.Username, c.Password, c.Passphrase, c.Notes} {
		if len(f) > maxField {
			return errors.New("2303 credential field is too long")
		}
	}
	if strings.ContainsAny(c.Username, " \t\r\n'\"`$;&|<>") || strings.HasPrefix(c.Username, "-") {
		return errors.New("2004 username contains invalid characters")
	}
	if c.Name == "" {
		c.Name = c.Username
	}
	if c.Name == "" {
		c.Name = "credential"
	}
	return nil
}

// Store 读写一个空间的凭据目录。
type Store struct {
	dir string
	mu  sync.Mutex
}

// Open 打开空间目录下的凭据存储。
func Open(spaceDir string) (*Store, error) {
	dir := filepath.Join(spaceDir, "credentials")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("2304 create credentials dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".sec") }

// List 返回全部凭据的摘要, 按名称排序。读不出的文件 (换了系统账号、钥匙串被清) 在 errs 里报告。
func (s *Store) List() (list []Summary, errs []error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return []Summary{}, []error{fmt.Errorf("2305 read credentials dir: %w", err)}
	}
	list = []Summary{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sec") {
			continue
		}
		c, err := s.Get(strings.TrimSuffix(name, ".sec"))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		list = append(list, c.Summary())
	}
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	return list, errs
}

// Get 读取并解密一条凭据。
func (s *Store) Get(id string) (Credential, error) {
	var c Credential
	if !conn.ValidID(id) {
		return c, errors.New("2306 invalid credential id")
	}
	s.mu.Lock()
	sealed, err := os.ReadFile(s.path(id))
	s.mu.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		return c, fmt.Errorf("2307 credential %q not found", id)
	}
	if err != nil {
		return c, fmt.Errorf("2308 read credential: %w", err)
	}
	raw, err := secret.Unprotect(sealed)
	if err != nil {
		return c, fmt.Errorf("2309 decrypt credential: %w", err)
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("2309 decrypt credential: %w", err)
	}
	c.ID = id
	return c, nil
}

// Save 校验并保存, id 为空时分配新 id。返回保存后的摘要。
func (s *Store) Save(c Credential) (Summary, error) {
	if c.ID == "" {
		c.ID = conn.NewID()
	}
	if err := c.Normalize(); err != nil {
		return Summary{}, err
	}
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return c.Summary(), s.write(c)
}

// Put 写入从别处来的凭据 (云同步拉取、空间之间搬移): 照常校验, 保留原 UpdatedAt。
func (s *Store) Put(c Credential) error {
	if !conn.ValidID(c.ID) {
		return errors.New("2306 invalid credential id")
	}
	if err := c.Normalize(); err != nil {
		return err
	}
	if c.UpdatedAt == "" {
		c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return s.write(c)
}

func (s *Store) write(c Credential) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("2310 encode credential: %w", err)
	}
	sealed, err := secret.Protect(raw)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path(c.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return fmt.Errorf("2310 write credential: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("2310 write credential: %w", err)
	}
	return nil
}

// Delete 删除凭据, 不存在视为成功。是否仍被连接引用由调用方检查。
func (s *Store) Delete(id string) error {
	if !conn.ValidID(id) {
		return errors.New("2306 invalid credential id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("2311 delete credential: %w", err)
	}
	return nil
}
