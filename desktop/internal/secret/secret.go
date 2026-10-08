// Package secret 把本机凭据 (git 同步密钥) 存进系统密钥库: macOS Keychain、Windows 凭据管理器。
// 不写进 settings.json: 设置文件可能被用户拷来拷去, 凭据不该跟着走。
package secret

import (
	"errors"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"

	"github.com/woodchen-ink/czlterm/desktop/internal/paths"
)

// GitKey 是 git 同步密钥的条目名。
const GitKey = "git-secret"

// VaultSessionKey 是「记住解锁」保存的 bw 会话密钥。
const VaultSessionKey = "bw-session"

func service() string {
	if v := os.Getenv(paths.InstanceEnv); v != "" {
		return "czlterm-" + v
	}
	return "czlterm"
}

// Get 读取凭据, 不存在时返回空串。
func Get(key string) (string, error) {
	v, err := keyring.Get(service(), key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("8101 read keychain: %w", err)
	}
	return v, nil
}

// Set 写入凭据; value 为空时删除。
func Set(key, value string) error {
	if value == "" {
		err := keyring.Delete(service(), key)
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("8102 delete keychain item: %w", err)
		}
		return nil
	}
	if err := keyring.Set(service(), key, value); err != nil {
		// Windows 凭据管理器单条上限约 2.5 KB, 4096 位 RSA 私钥放不下。
		if errors.Is(err, keyring.ErrSetDataTooBig) {
			return errors.New("8103 secret is too large for the system keychain, use an ed25519 key or an access token")
		}
		return fmt.Errorf("8104 write keychain: %w", err)
	}
	return nil
}
