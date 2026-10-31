package aicontext

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedInstructions_NonEmpty(t *testing.T) {
	content := EmbeddedInstructionsContent()
	if len(content) == 0 {
		t.Fatal("embeddedInstructions payload is empty")
	}
	expectedHeader := "# CEO Plugin — MCP Server Instructions & Operating Guide"
	if !strings.Contains(string(content), expectedHeader) {
		t.Fatalf("embeddedInstructions does not contain expected header: %q", expectedHeader)
	}
}

func TestDefaultInstructionTargets(t *testing.T) {
	home := filepath.Join("C:", "Users", "TestUser")
	appData := filepath.Join(home, "AppData", "Roaming")

	targetsWithAppData := DefaultInstructionTargets(home, appData)
	if len(targetsWithAppData) != 4 {
		t.Fatalf("expected 4 targets, got %d", len(targetsWithAppData))
	}
	expectedClaude := filepath.Join(appData, "Claude", "mcp", "ceo-plugin", "instructions.md")
	if targetsWithAppData[3].IDE != "Claude" || targetsWithAppData[3].Path != expectedClaude {
		t.Fatalf("unexpected Claude target with appData: %+v", targetsWithAppData[3])
	}

	targetsWithoutAppData := DefaultInstructionTargets(home, "")
	if len(targetsWithoutAppData) != 4 {
		t.Fatalf("expected 4 targets, got %d", len(targetsWithoutAppData))
	}
	if targetsWithoutAppData[3].IDE != "Claude" || targetsWithoutAppData[3].Path != "" {
		t.Fatalf("unexpected Claude target without appData: %+v", targetsWithoutAppData[3])
	}
}

func TestExportAndRemoveMCPInstructions_Lifecycle(t *testing.T) {
	base := t.TempDir()

	targets := []InstructionTarget{
		{
			IDE:  "Antigravity",
			Path: filepath.Join(base, ".gemini", "antigravity", "mcp", "ceo-plugin", "instructions.md"),
		},
		{
			IDE:  "Codex",
			Path: filepath.Join(base, ".codex", "mcp", "ceo-plugin", "instructions.md"),
		},
		{
			IDE:  "Cursor",
			Path: filepath.Join(base, ".cursor", "mcp", "ceo-plugin", "instructions.md"),
		},
		{
			IDE:  "Claude",
			Path: filepath.Join(base, "AppData", "Claude", "mcp", "ceo-plugin", "instructions.md"),
		},
	}

	var buf bytes.Buffer
	// 1. Initial Export
	if err := ExportMCPInstructions(targets, &buf); err != nil {
		t.Fatalf("ExportMCPInstructions failed: %v", err)
	}

	for _, target := range targets {
		data, err := os.ReadFile(target.Path)
		if err != nil {
			t.Fatalf("failed to read installed instructions for %s at %s: %v", target.IDE, target.Path, err)
		}
		if !bytes.Equal(data, embeddedInstructions) {
			t.Fatalf("installed content mismatch for %s at %s", target.IDE, target.Path)
		}
		if !strings.Contains(buf.String(), target.IDE+": instructions installed") {
			t.Fatalf("expected output log for %s installation, got: %s", target.IDE, buf.String())
		}
	}

	// 2. Idempotency Check: Export again with unchanged contents
	buf.Reset()
	if err := ExportMCPInstructions(targets, &buf); err != nil {
		t.Fatalf("ExportMCPInstructions repeat failed: %v", err)
	}
	for _, target := range targets {
		if !strings.Contains(buf.String(), target.IDE+": instructions unchanged") {
			t.Fatalf("expected 'instructions unchanged' for %s, got: %s", target.IDE, buf.String())
		}
	}

	// 3. Removal Lifecycle
	buf.Reset()
	if err := RemoveMCPInstructions(targets, &buf); err != nil {
		t.Fatalf("RemoveMCPInstructions failed: %v", err)
	}

	for _, target := range targets {
		if _, err := os.Stat(target.Path); !os.IsNotExist(err) {
			t.Fatalf("instructions.md still exists after remove for %s: %s", target.IDE, target.Path)
		}
		dir := filepath.Dir(target.Path)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("empty ceo-plugin directory was not removed for %s: %s", target.IDE, dir)
		}
		if !strings.Contains(buf.String(), target.IDE+": instructions removed") {
			t.Fatalf("expected output log for %s removal, got: %s", target.IDE, buf.String())
		}
		if !strings.Contains(buf.String(), target.IDE+": empty directory removed") {
			t.Fatalf("expected output log for %s directory removal, got: %s", target.IDE, buf.String())
		}
	}

	// 4. Repeated Removal is a no-op / idempotent
	buf.Reset()
	if err := RemoveMCPInstructions(targets, &buf); err != nil {
		t.Fatalf("repeat RemoveMCPInstructions failed: %v", err)
	}
}

func TestExportMCPInstructions_UnresolvedPathSkipped(t *testing.T) {
	targets := []InstructionTarget{
		{
			IDE:  "Claude",
			Path: "",
		},
	}
	var buf bytes.Buffer
	if err := ExportMCPInstructions(targets, &buf); err != nil {
		t.Fatalf("unexpected error on empty path: %v", err)
	}
	if !strings.Contains(buf.String(), "Claude: instructions skipped (unresolved path)") {
		t.Fatalf("expected skipped message in log, got: %s", buf.String())
	}
}

func TestRemoveMCPInstructions_PreservesNonEmptyDirectory(t *testing.T) {
	base := t.TempDir()
	targetPath := filepath.Join(base, "mcp", "ceo-plugin", "instructions.md")
	userFile := filepath.Join(base, "mcp", "ceo-plugin", "custom.json")

	targets := []InstructionTarget{
		{
			IDE:  "CustomIDE",
			Path: targetPath,
		},
	}

	var buf bytes.Buffer
	if err := ExportMCPInstructions(targets, &buf); err != nil {
		t.Fatalf("ExportMCPInstructions failed: %v", err)
	}

	// Add user file in the ceo-plugin directory
	if err := os.WriteFile(userFile, []byte("user data"), 0644); err != nil {
		t.Fatalf("failed to write user file: %v", err)
	}

	buf.Reset()
	if err := RemoveMCPInstructions(targets, &buf); err != nil {
		t.Fatalf("RemoveMCPInstructions failed: %v", err)
	}

	// instructions.md must be gone
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("instructions.md still exists: %s", targetPath)
	}

	// ceo-plugin directory and custom.json must be preserved
	dir := filepath.Dir(targetPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatalf("ceo-plugin directory was incorrectly deleted when non-empty")
	}
	if _, err := os.Stat(userFile); os.IsNotExist(err) {
		t.Fatalf("user file was deleted")
	}
}
