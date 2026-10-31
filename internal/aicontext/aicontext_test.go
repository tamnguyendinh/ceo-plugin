package aicontext

import (
	"bytes"
	"ceo-plugin/internal/virtualfs"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testBlock() string {
	return RenderManagedBlock([]virtualfs.Command{
		{Lane: "ceo", Description: "Load CEO instructions"},
		{Lane: "ceo", Reference: "timer-rules", Description: "Read timer rules"},
	})
}

var expectedGoldenMarkdown = testBlock()

func TestRenderManagedBlock(t *testing.T) {
	commands := []virtualfs.Command{
		{Lane: "ceo", Description: "Load CEO instructions"},
		{Lane: "ceo", Reference: "timer-rules", Description: "Read timer rules"},
		{Lane: "system-architect", Reference: "mode-1-spec-authoring-and-architecture", Description: "Read mode 1"},
		{Lane: "qa", Reference: "reporting", Description: "Read | reporting\nwith `quotes`"},
		{Lane: "supervisor", Reference: "claim-and-authority", Description: "Read claim"},
		{Lane: "strategic-planner", Reference: "plan-template", Description: "Read plan template"},
	}
	block := RenderManagedBlock(commands)
	if block != RenderManagedBlock(commands) || strings.Count(block, StartMarker) != 1 || strings.Count(block, EndMarker) != 1 {
		t.Fatal("non-deterministic block")
	}
	if !strings.Contains(block, "`ceo-plugin qa reporting`") || !strings.Contains(block, "&#124;") || !strings.Contains(block, "&#96;") {
		t.Fatal("missing/invalid table row")
	}
	if !strings.Contains(block, "### How to use ceo-plugin:") ||
		!strings.Contains(block, "In Windows:") ||
		!strings.Contains(block, "PowerShell") ||
		!strings.Contains(block, "CMD") ||
		!strings.Contains(block, "Git Bash") ||
		!strings.Contains(block, "do not create agents") {
		t.Fatal("missing invocation contract")
	}
	headings := []string{
		"### 1. Primary Lane Commands (Loads Core SKILL.md Only)",
		"### 2. CEO — Orchestration & Coordination Subcommands",
		"### 3. System Architect — Architecture & SPEC Subcommands",
		"### 4. QA — Verification Protocols & Quality Rules",
		"### 5. Supervisor — Zero-Trust & Audit Rules",
		"### 6. Strategic Planner — Plan & Ledger Templates",
	}
	for _, h := range headings {
		if !strings.Contains(block, h) {
			t.Fatalf("missing heading: %s", h)
		}
	}
	for _, text := range []string{
		"### Workspace Initialization — `ceo-plugin init`",
		`& "$HOME\.ceo-plugin\bin\ceo-plugin.exe" init`,
		`"%USERPROFILE%\.ceo-plugin\bin\ceo-plugin.exe" init`,
		`"$HOME/.ceo-plugin/bin/ceo-plugin.exe" init`,
		"runs `git init` in the current directory",
		"`/AGENTS.md` and `/CLAUDE.md`",
		"preserving user content outside the managed block",
		"Already tracked context files remain tracked",
	} {
		if !strings.Contains(block, text) {
			t.Fatalf("missing init guidance: %s", text)
		}
	}
}

// Test 6: upsertSection Scenarios
func TestUpsertSection_Scenarios(t *testing.T) {
	block := testBlock()

	t.Run("Scenario 1: File does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "NEW.md")

		if err := upsertSection(target, block); err != nil {
			t.Fatalf("upsertSection failed: %v", err)
		}

		content, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}

		expected := block + "\n"
		if string(content) != expected {
			t.Fatalf("file content mismatch.\nGot:\n%s\nExpected:\n%s", string(content), expected)
		}
	})

	t.Run("Scenario 2: File has current marker", func(t *testing.T) {
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "AGENTS.md")

		initial := "# Prefix Rule\n\nSome custom instructions.\n\n" +
			"<!-- ceo-plugin:start -->\nOld content\n<!-- ceo-plugin:end -->\n\n" +
			"## Suffix Rule\nKeep this intact.\n"

		if err := os.WriteFile(target, []byte(initial), 0644); err != nil {
			t.Fatalf("failed to write initial file: %v", err)
		}

		if err := upsertSection(target, block); err != nil {
			t.Fatalf("upsertSection failed: %v", err)
		}

		content, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}

		text := string(content)
		if !strings.Contains(text, "# Prefix Rule") {
			t.Errorf("prefix was lost")
		}
		if !strings.Contains(text, "## Suffix Rule\nKeep this intact.") {
			t.Errorf("suffix was lost")
		}
		if strings.Contains(text, "Old content") {
			t.Errorf("old content was not replaced")
		}
		if strings.Count(text, StartMarker) != 1 || strings.Count(text, EndMarker) != 1 {
			t.Errorf("marker count abnormal, expected exactly 1 start and 1 end marker")
		}
	})

	t.Run("Scenario 3: File has legacy marker", func(t *testing.T) {
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "LEGACY.md")

		initial := "# Legacy Prefix\n\n" +
			"<!-- anvien-ceo-plugin:start -->\nLegacy 2024 content\n<!-- anvien-ceo-plugin:end -->\n\n" +
			"## Suffix Rule\n"

		if err := os.WriteFile(target, []byte(initial), 0644); err != nil {
			t.Fatalf("failed to write initial file: %v", err)
		}

		if err := upsertSection(target, block); err != nil {
			t.Fatalf("upsertSection failed: %v", err)
		}

		content, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}

		text := string(content)
		if strings.Contains(text, "anvien-ceo-plugin") {
			t.Errorf("legacy marker was not upgraded")
		}
		if !strings.Contains(text, StartMarker) || !strings.Contains(text, EndMarker) {
			t.Errorf("new marker missing after upgrade")
		}
		if !strings.Contains(text, "# Legacy Prefix") {
			t.Errorf("prefix was lost during legacy upgrade")
		}
	})

	t.Run("Scenario 4: File has content but no markers", func(t *testing.T) {
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "NO_MARKERS.md")

		initial := "# Custom Instructions\n\nNo markers here.\n"
		if err := os.WriteFile(target, []byte(initial), 0644); err != nil {
			t.Fatalf("failed to write initial file: %v", err)
		}

		if err := upsertSection(target, block); err != nil {
			t.Fatalf("upsertSection failed: %v", err)
		}

		content, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}

		text := string(content)
		if !strings.HasPrefix(text, "# Custom Instructions\n\nNo markers here.") {
			t.Errorf("prefix content modified unexpectedly")
		}
		if !strings.Contains(text, StartMarker) || !strings.Contains(text, EndMarker) {
			t.Errorf("managed block was not appended")
		}
	})

	t.Run("Scenario 5: Idempotency", func(t *testing.T) {
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "IDEMPOTENT.md")

		initial := "# Rules\n\nCustom.\n\n<!-- ceo-plugin:start -->\nInitial\n<!-- ceo-plugin:end -->\n\n# Postfix\n"
		if err := os.WriteFile(target, []byte(initial), 0644); err != nil {
			t.Fatalf("failed to write initial file: %v", err)
		}

		// First upsert
		if err := upsertSection(target, block); err != nil {
			t.Fatalf("first upsertSection failed: %v", err)
		}
		firstRun, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("failed to read file after first run: %v", err)
		}

		// Second upsert
		if err := upsertSection(target, block); err != nil {
			t.Fatalf("second upsertSection failed: %v", err)
		}
		secondRun, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("failed to read file after second run: %v", err)
		}

		if string(firstRun) != string(secondRun) {
			t.Fatalf("upsertSection is not idempotent!\nFirst run:\n%s\nSecond run:\n%s", string(firstRun), string(secondRun))
		}
	})
}

// Test 7: Live Workspace Sync and Idempotence
func TestSyncAIContext_LiveWorkspace(t *testing.T) {
	t.Chdir(t.TempDir())
	// 1. Sync live workspace
	if err := SyncAIContext(testBlock()); err != nil {
		t.Fatalf("SyncAIContext failed: %v", err)
	}

	root := findWorkspaceRoot()
	agentsPath := filepath.Join(root, "AGENTS.md")
	claudePath := filepath.Join(root, "CLAUDE.md")

	agents1, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}

	claude1, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}

	// Verify both contain expectedGoldenMarkdown
	if !strings.Contains(string(agents1), expectedGoldenMarkdown) {
		t.Fatalf("AGENTS.md does not contain expectedGoldenMarkdown.\nGot:\n%s", string(agents1))
	}
	if !strings.Contains(string(claude1), expectedGoldenMarkdown) {
		t.Fatalf("CLAUDE.md does not contain expectedGoldenMarkdown.\nGot:\n%s", string(claude1))
	}

	// Verify exact count of markers in each file (exactly 1 start marker, exactly 1 end marker)
	if strings.Count(string(agents1), StartMarker) != 1 || strings.Count(string(agents1), EndMarker) != 1 {
		t.Fatalf("AGENTS.md marker count invalid: Start=%d, End=%d",
			strings.Count(string(agents1), StartMarker), strings.Count(string(agents1), EndMarker))
	}
	if strings.Count(string(claude1), StartMarker) != 1 || strings.Count(string(claude1), EndMarker) != 1 {
		t.Fatalf("CLAUDE.md marker count invalid: Start=%d, End=%d",
			strings.Count(string(claude1), StartMarker), strings.Count(string(claude1), EndMarker))
	}

	// 2. Second sync to verify Idempotence (0 drift)
	if err := SyncAIContext(testBlock()); err != nil {
		t.Fatalf("second SyncAIContext failed: %v", err)
	}

	agents2, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("failed to read AGENTS.md after second sync: %v", err)
	}

	claude2, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md after second sync: %v", err)
	}

	if string(agents1) != string(agents2) {
		t.Fatalf("AGENTS.md drifted after consecutive sync!\nFirst run len: %d, Second run len: %d",
			len(agents1), len(agents2))
	}

	if string(claude1) != string(claude2) {
		t.Fatalf("CLAUDE.md drifted after consecutive sync!\nFirst run len: %d, Second run len: %d",
			len(claude1), len(claude2))
	}
}

// BenchmarkSyncAIContext measures live workspace sync latency
func BenchmarkSyncAIContext(b *testing.B) {
	b.Chdir(b.TempDir())
	for i := 0; i < b.N; i++ {
		_ = SyncAIContext(testBlock())
	}
}

func TestManagedBlockPreservesBytesAndRejectsAmbiguity(t *testing.T) {
	prefix := []byte("\xef\xbb\xbf    owner rule\r\n\r\n")
	suffix := []byte("\r\n    owner suffix  \r\n\r\n")
	raw := append(append(bytes.Clone(prefix), []byte(StartMarker+"\r\nold\r\n"+EndMarker)...), suffix...)
	block := []byte(StartMarker + "\nnew\n" + EndMarker)
	out, err := UpsertManagedBlock(raw, true, block)
	if err != nil || !bytes.HasPrefix(out, prefix) || !bytes.HasSuffix(out, suffix) {
		t.Fatalf("outside bytes changed: %q %v", out, err)
	}
	for _, bad := range []string{StartMarker, EndMarker + "\n" + StartMarker, StartMarker + "\n" + StartMarker + "\n" + EndMarker, " " + StartMarker + "\n " + EndMarker, StartMarker + "  \n" + EndMarker + "  "} {
		if _, err := UpsertManagedBlock([]byte(bad), true, block); err == nil {
			t.Fatalf("accepted ambiguous block: %s", bad)
		}
	}
	example := "```markdown\n" + StartMarker + "\nexample\n" + EndMarker + "\n```\n"
	out, err = UpsertManagedBlock([]byte(example), true, block)
	if err != nil || !bytes.HasPrefix(out, []byte(example)) {
		t.Fatalf("modified code example: %v", err)
	}
}

func TestSyncNoOpAndPreflightFailure(t *testing.T) {
	root := t.TempDir()
	block := testBlock()
	if _, err := SyncWorkspace(context.Background(), root, block); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "AGENTS.md")
	before, _ := os.Stat(p)
	r, err := SyncWorkspace(context.Background(), root, block)
	after, _ := os.Stat(p)
	if err != nil || r.Status != "unchanged" || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("not a no-op: %+v %v", r, err)
	}
	owner := []byte("owner unchanged\n")
	if err := os.WriteFile(p, owner, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(StartMarker), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncWorkspace(context.Background(), root, block); err == nil {
		t.Fatal("expected malformed marker error")
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(owner, got) {
		t.Fatal("preflight changed sibling")
	}
}

func TestWorkspaceRootBoundaries(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b", "c", "d", "e", "f", "g", "h")
	if err := os.MkdirAll(sub, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "AGENTS.md"), []byte("nested"), 0600); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	canonicalSub, err := filepath.EvalSymlinks(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveWorkspace(sub, "", "cli"); got.Root != canonicalRoot {
		t.Fatalf("wrong root: %+v, want %q", got, canonicalRoot)
	}
	if got := ResolveWorkspace(sub, "", "mcp"); got.Root != "" {
		t.Fatalf("inferred untrusted MCP cwd: %+v", got)
	}
	if got := ResolveWorkspace(sub, sub, "mcp"); got.Root != canonicalSub {
		t.Fatalf("ignored explicit subworkspace: %+v", got)
	}
}
