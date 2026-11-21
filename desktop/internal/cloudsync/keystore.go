package cloudsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/woodchen-ink/go-web-utils/syncsdk"

	"github.com/woodchen-ink/czlterm/desktop/internal/secret"
)

// KeyStore 实现同步 SDK 的 syncsdk.KeyStore: 用户私钥与各空间密钥每个名称一个文件,
// 文件名是名称的摘要 (名称里带用户与空间 id, 不落在文件名上), 内容经 Protect 加密。
type KeyStore struct {
	dir string
}

// NewKeyStore 在 dir 下存放密钥文件, 目录按需创建。
func NewKeyStore(dir string) *KeyStore {
	return &KeyStore{dir: dir}
}

func (k *KeyStore) path(name string) string {
	sum := sha256.Sum256([]byte(name))
	return filepath.Join(k.dir, hex.EncodeToString(sum[:])+".key")
}

// Get 实现 syncsdk.KeyStore, 不存在返回 syncsdk.ErrKeyNotFound。
func (k *KeyStore) Get(_ context.Context, name string) ([]byte, error) {
	sealed, err := os.ReadFile(k.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, syncsdk.ErrKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return secret.Unprotect(sealed)
}

// Put 实现 syncsdk.KeyStore, 先写临时文件再改名, 写到一半不会留下坏文件。
func (k *KeyStore) Put(_ context.Context, name string, plain []byte) error {
	sealed, err := secret.Protect(plain)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(k.dir, 0o700); err != nil {
		return err
	}
	final := k.path(name)
	tmp := final + ".part"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// Delete 实现 syncsdk.KeyStore, 不存在不算错误。
func (k *KeyStore) Delete(_ context.Context, name string) error {
	if err := os.Remove(k.path(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

var _ syncsdk.KeyStore = (*KeyStore)(nil)
