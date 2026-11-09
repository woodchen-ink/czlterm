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

// 旧版设置升级后切到内置终端一次并写回; 之后用户改回外部终端不再被覆盖。
func TestMigrateBuiltinTerminal(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"terminal":"wt"}`), 0o600)
	// 与 app.go 一致: 新装用户的默认值已是内置终端且标记为迁移过。
	defaults := Settings{Terminal: "builtin", TerminalMigrated: true}
	s, err := Open(dir, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got.Terminal != "builtin" || !got.TerminalMigrated {
		t.Fatalf("expected builtin after migration, got %+v", got)
	}
	if _, err := s.Update(func(c *Settings) { c.Terminal = "wt" }); err != nil {
		t.Fatal(err)
	}
	s, _ = Open(dir, defaults)
	if s.Get().Terminal != "wt" {
		t.Fatalf("user choice overwritten: %q", s.Get().Terminal)
	}
}
