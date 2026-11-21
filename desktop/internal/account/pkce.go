package account

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// randomToken 生成 URL 安全的随机串, n 为随机字节数。
func randomToken(n int) string {
	b := make([]byte, n)
	// crypto/rand.Read 在 Go 1.24+ 不会返回错误
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// newVerifier 生成 PKCE code_verifier (43 字符, 符合 RFC 7636 的 43-128 要求)。
func newVerifier() string { return randomToken(32) }

// challengeS256 计算 S256 code_challenge。
func challengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
