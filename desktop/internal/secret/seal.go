package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
)

// 登录令牌、端到端加密私钥这类较大的秘密不直接放系统密钥库 (Windows 单条约 2.5 KB 上限),
// 而是用一把随机 AES-256 密钥加密后落盘; 那把密钥本身存在系统密钥库里。
// 文件拷到别的账号或电脑上解不开, 效果与 DPAPI / 钥匙串直接加密相同。

// SealKey 是文件加密密钥的条目名。
const SealKey = "seal-key"

var (
	sealMu  sync.Mutex
	sealAES cipher.AEAD
)

// sealVersion 是密文格式前缀, 以后换算法时据此区分。
const sealVersion byte = 1

func aead(create bool) (cipher.AEAD, error) {
	sealMu.Lock()
	defer sealMu.Unlock()
	if sealAES != nil {
		return sealAES, nil
	}
	enc, err := Get(SealKey)
	if err != nil {
		return nil, err
	}
	var key []byte
	if enc != "" {
		if key, err = base64.StdEncoding.DecodeString(enc); err != nil || len(key) != 32 {
			return nil, errors.New("8105 seal key in keychain is corrupted")
		}
	} else {
		if !create {
			return nil, errors.New("8106 seal key not found")
		}
		key = make([]byte, 32)
		_, _ = rand.Read(key)
		if err := Set(SealKey, base64.StdEncoding.EncodeToString(key)); err != nil {
			return nil, err
		}
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	sealAES = g
	return g, nil
}

// UseKeyForTest 让本进程用给定的 32 字节密钥加解密, 不碰系统密钥库 (测试与 CI 里钥匙串不可用)。
func UseKeyForTest(key []byte) {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	sealMu.Lock()
	sealAES = g
	sealMu.Unlock()
}

// Protect 加密一段秘密, 第一次使用时在系统密钥库里生成密钥。
func Protect(plain []byte) ([]byte, error) {
	g, err := aead(true)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	_, _ = rand.Read(nonce)
	out := append([]byte{sealVersion}, nonce...)
	return g.Seal(out, nonce, plain, []byte{sealVersion}), nil
}

// Unprotect 解开 Protect 的结果。密钥库里没有密钥 (换了账号、钥匙串被清) 时报错。
func Unprotect(sealed []byte) ([]byte, error) {
	g, err := aead(false)
	if err != nil {
		return nil, err
	}
	if len(sealed) < 1+g.NonceSize() || sealed[0] != sealVersion {
		return nil, errors.New("8107 sealed data is corrupted")
	}
	nonce := sealed[1 : 1+g.NonceSize()]
	plain, err := g.Open(nil, nonce, sealed[1+g.NonceSize():], []byte{sealVersion})
	if err != nil {
		return nil, fmt.Errorf("8107 sealed data is corrupted: %w", err)
	}
	return plain, nil
}
