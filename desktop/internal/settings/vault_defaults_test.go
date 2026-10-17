package settings

import (
	"os"
	"path/filepath"
	"testing"
)

// TestVaultDefaults 新安装和缺失字段采用免空闲锁定与记住解锁, 显式偏好保存后仍保留。
func TestVaultDefaults(t *testing.T) {
	for _, tc := range []struct {
		name     string
		raw      string
		remember bool
		minutes  int
	}{
		{"new install", "", true, 0},
		{"legacy missing fields", `{}`, true, 0},
		{"explicit disabled and timeout", `{"vaultRemember":false,"vaultAutoLockMinutes":60}`, false, 60},
		{"explicit enabled and zero", `{"vaultRemember":true,"vaultAutoLockMinutes":0}`, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.raw != "" {
				if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(tc.raw), 0600); err != nil {
					t.Fatal(err)
				}
			}
			defaults := Settings{VaultRemember: true, VaultAutoLockMinutes: 0}
			s, err := Open(dir, defaults)
			if err != nil {
				t.Fatal(err)
			}
			got := s.Get()
			if got.VaultRemember != tc.remember || got.VaultAutoLockMinutes != tc.minutes {
				t.Fatalf("remember=%v timeout=%d, want %v/%d", got.VaultRemember, got.VaultAutoLockMinutes, tc.remember, tc.minutes)
			}
			if _, err := s.Update(func(*Settings) {}); err != nil {
				t.Fatal(err)
			}
			s, err = Open(dir, defaults)
			if err != nil {
				t.Fatal(err)
			}
			got = s.Get()
			if got.VaultRemember != tc.remember || got.VaultAutoLockMinutes != tc.minutes {
				t.Fatal("saved preference changed after reopening")
			}
		})
	}
}
