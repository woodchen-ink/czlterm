package main

import (
	"strings"
	"testing"
)

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
