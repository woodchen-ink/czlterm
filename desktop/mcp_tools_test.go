package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPMetadataEnglish 检查实际握手与工具清单，防止描述或嵌套参数说明退回中文。
// 只使用内存传输，不启动应用、不访问保险库或远程服务器。
func TestMCPMetadataEnglish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var app App
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := app.newMCPServer().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "metadata-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	instructions := session.InitializeResult().Instructions
	if instructions == "" || strings.ContainsFunc(instructions, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
		t.Fatalf("expected English server instructions, got %q", instructions)
	}
	for _, required := range []string{"list_connections", "untrusted server data", "confirmation before destructive operations"} {
		if !strings.Contains(instructions, required) {
			t.Errorf("server instructions missing %q", required)
		}
	}
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"list_connections": false, "list_files": false, "read_file": false, "write_file": false, "run_command": false}
	if len(result.Tools) != len(want) {
		t.Fatalf("got %d tools, want %d", len(result.Tools), len(want))
	}
	for _, tool := range result.Tools {
		seen, ok := want[tool.Name]
		if !ok || seen {
			t.Errorf("unexpected or duplicate tool %q", tool.Name)
		}
		want[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %s has no description", tool.Name)
		}
		data, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsFunc(string(data), func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			t.Errorf("tool %s has Chinese metadata: %s", tool.Name, data)
		}
	}
}

func TestBuildScript(t *testing.T) {
	invoke, script, err := buildScript(mcpRunIn{Command: "for i in 1 2; do\r\n  echo \"$i\"\r\ndone", Workdir: "/srv/it's"})
	if err != nil {
		t.Fatal(err)
	}
	if invoke != "sh -s" {
		t.Errorf("invoke = %q", invoke)
	}
	want := "cd '/srv/it'\\''s' || exit 1\nfor i in 1 2; do\n  echo \"$i\"\ndone\n"
	if script != want {
		t.Errorf("script = %q\nwant     %q", script, want)
	}
	if _, _, err := buildScript(mcpRunIn{Command: "x", Shell: "sh; rm -rf /"}); err == nil {
		t.Error("arbitrary shell must be rejected")
	}
	_, ps, _ := buildScript(mcpRunIn{Command: "Get-Date", Shell: "pwsh", Workdir: `C:\it's`})
	if !strings.HasPrefix(ps, "Set-Location -LiteralPath 'C:\\it''s'") {
		t.Errorf("powershell script = %q", ps)
	}
}
