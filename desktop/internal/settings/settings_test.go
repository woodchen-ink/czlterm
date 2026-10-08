package settings

import (
	"os"
	"path/filepath"
	"testing"
)

// 旧版设置文件没有 mcpAllowAccess: 开过写 / 执行的用户升级后访问开关应为开。
func TestMigrateAllowAccess(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"mcpEnabled":true,"mcpAllowExec":true}`), 0o600)
	s, err := Open(dir, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Get().MCPAllowAccess {
		t.Fatal("expected mcpAllowAccess migrated to true")
	}
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"mcpAllowExec":true,"mcpAllowAccess":false}`), 0o600)
	s, _ = Open(dir, Settings{})
	if s.Get().MCPAllowAccess {
		t.Fatal("explicit false must be kept")
	}
}
