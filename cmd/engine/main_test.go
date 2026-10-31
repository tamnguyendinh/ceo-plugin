package main

import (
	"bytes"
	"ceo-plugin/internal/aicontext"
	"ceo-plugin/internal/virtualfs"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var testAssetsRoot string
var nativeEnginePath string

func TestCatalogCoverageAndRawOutput(t *testing.T) {
	commands, err := getCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 41 {
		t.Fatalf("expected 41 command rows, got %d", len(commands))
	}
	seen := map[string]bool{}
	tree, err := getAssetTree()
	if err != nil {
		t.Fatal(err)
	}
	block := aicontext.RenderManagedBlock(commands)
	for _, c := range commands {
		args := []string{c.Lane}
		if c.Reference != "" {
			args = append(args, c.Reference)
		}
		var out, diagnostic bytes.Buffer
		_, code := runCLI(&out, &diagnostic, args)
		entry, err := tree.FindByURI(c.URI)
		if err != nil || code != 0 || out.String() != entry.Content {
			t.Fatalf("%v: code=%d err=%v", args, code, err)
		}
		seen[c.URI] = true
		if !strings.Contains(block, "`ceo-plugin "+strings.Join(args, " ")+"`") {
			t.Fatalf("missing row %v", args)
		}
	}
	if len(seen) != tree.Count() {
		t.Fatal("guide does not cover every resource")
	}
	metadata := map[string]string{}
	for k, v := range assetDescriptions {
		metadata[k] = v
	}
	delete(metadata, "assets/qa/SKILL.md")
	if _, err := virtualfs.BuildCatalog(tree, metadata); err == nil {
		t.Fatal("missing metadata accepted")
	}
}

type shortOutput struct{}

func (shortOutput) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestCLIValidationAndSyncFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"ceo", "timer-rules", "extra"}, {"mcp", "extra"}, {"help", "extra"}, {"unknown"}} {
		var out, diagnostic bytes.Buffer
		_, code := runCLI(&out, &diagnostic, args)
		if code != 1 || out.Len() != 0 {
			t.Fatalf("invalid request accepted: %v", args)
		}
	}
	entries, _ := os.ReadDir(".")
	if len(entries) != 0 {
		t.Fatal("invalid request wrote context")
	}
	var missingOut, missingDiagnostic bytes.Buffer
	_, missingCode := runCLI(&missingOut, &missingDiagnostic, []string{"ceo"})
	entries, err := os.ReadDir(".")
	if err != nil || missingCode != 0 || missingOut.Len() == 0 || missingDiagnostic.Len() != 0 || len(entries) != 2 {
		t.Fatalf("document read failed to create context: %v %s", err, missingDiagnostic.String())
	}
	if err := os.Remove("AGENTS.md"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("AGENTS.md", 0700); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	_, code := runCLI(&out, &diagnostic, []string{"ceo"})
	tree, _ := getAssetTree()
	entry, _ := tree.GetSkill("ceo")
	if code != 0 || out.String() != entry.Content || !strings.Contains(diagnostic.String(), "context_sync=failed") {
		t.Fatal("sync failure hid payload/diagnostic")
	}
	if _, code := runCLI(shortOutput{}, io.Discard, []string{"ceo"}); code != 1 {
		t.Fatal("short stdout write ignored")
	}
}

func BenchmarkEmbeddedAssets(b *testing.B) {
	b.Run("Index", func(b *testing.B) {
		for b.Loop() {
			tree, err := virtualfs.NewTreeFromFS(embeddedAssets)
			if err != nil {
				b.Fatal(err)
			}
			runtime.KeepAlive(tree)
		}
	})
	tree, err := virtualfs.NewTreeFromFS(embeddedAssets)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("Skill", func(b *testing.B) {
		for b.Loop() {
			if _, err := tree.GetSkill("ceo"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Reference", func(b *testing.B) {
		for b.Loop() {
			if _, err := tree.GetReference("ceo", "timer-rules"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("URI", func(b *testing.B) {
		for b.Loop() {
			if _, err := tree.FindByURI("ram://ceo/SKILL.md"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestEmbeddedRetainedHeap(t *testing.T) {
	// Run alone for the performance gate; signed delta also exposes noisy background GC.
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	tree, err := virtualfs.NewTreeFromFS(embeddedAssets)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(tree)
	delta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("embedded retained heap signed delta: %d bytes; total allocated: %d bytes", delta, after.TotalAlloc-before.TotalAlloc)
	if delta < 0 {
		t.Skip("noisy heap baseline; isolated measurement required")
	}
	if delta >= 500*1024 {
		t.Fatalf("retained tree exceeds 500 KiB: %d", delta)
	}
}

func TestMain(m *testing.M) {
	var err error
	nativeEnginePath, err = filepath.Abs("../../dist/bin/ceo-plugin.exe")
	if err != nil {
		panic(err)
	}
	testAssetsRoot, err = filepath.Abs("assets")
	if err != nil {
		panic(err)
	}
	root, err := os.MkdirTemp("", "ceo-engine-tests-")
	if err != nil {
		panic(err)
	}
	old, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	if err = os.Chdir(root); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.Chdir(old)
	_ = os.RemoveAll(root) // Only the directory created by this test process.
	os.Exit(code)
}

func TestNativeBinaryAssetsSyncProtocolAndEOF(t *testing.T) {
	if _, err := os.Stat(nativeEnginePath); err != nil {
		t.Fatal("run build.ps1 before native tests: ", err)
	}
	root := t.TempDir()
	commands, err := getCatalog()
	if err != nil {
		t.Fatal(err)
	}
	tree, _ := getAssetTree()
	for _, c := range commands {
		root = t.TempDir() // Every documented command must bootstrap a fresh workspace.
		args := []string{c.Lane}
		if c.Reference != "" {
			args = append(args, c.Reference)
		}
		cmd := exec.Command(nativeEnginePath, args...)
		cmd.Dir = root
		var diagnostic bytes.Buffer
		cmd.Stderr = &diagnostic
		output, err := cmd.Output()
		entry, _ := tree.FindByURI(c.URI)
		if err != nil || computeSHA256(output) != entry.SHA256() || diagnostic.Len() != 0 {
			t.Fatalf("native %v: %v %s", args, err, diagnostic.String())
		}
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			raw, err := os.ReadFile(filepath.Join(root, name))
			if err != nil || !bytes.Contains(raw, []byte(aicontext.StartMarker)) {
				t.Fatalf("native %v did not create %s: %v", args, name, err)
			}
		}
	}
	t.Logf("native CLI: %d commands match embedded SHA-256", len(commands))
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 2 {
		t.Fatalf("native document reads did not create exactly two context files: %v %v", entries, err)
	}
	path := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(path, []byte("owner notes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(nativeEnginePath, "coder")
	cmd.Dir = root
	if _, err := cmd.Output(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(raw, []byte("owner notes\n")) || !bytes.Contains(raw, []byte(aicontext.StartMarker)) {
		t.Fatalf("native existing file not updated: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "CLAUDE.md")); err != nil || !bytes.Contains(raw, []byte(aicontext.StartMarker)) {
		t.Fatalf("native read did not create missing sibling: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(nativeEnginePath, "coder")
	cmd.Dir = root
	if _, err := cmd.Output(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("native no-op rewrote context")
	}
	badRoot := t.TempDir()
	cmd = exec.Command(nativeEnginePath, "ceo", "timer-rules", "extra")
	cmd.Dir = badRoot
	if err := cmd.Run(); err == nil {
		t.Fatal("native invalid arity accepted")
	}
	entries, _ = os.ReadDir(badRoot)
	if len(entries) != 0 {
		t.Fatal("native invalid arity wrote context")
	}

	mcpRoot := t.TempDir()
	rootURI := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(mcpRoot)}
	var input bytes.Buffer
	fmt.Fprintln(&input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{"roots":{}},"clientInfo":{"name":"native-fixture","version":"1"}}}`)
	fmt.Fprintln(&input, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	rootReply, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": "ceo-plugin/roots/1", "result": map[string]interface{}{"roots": []map[string]string{{"uri": rootURI.String()}}}})
	fmt.Fprintln(&input, string(rootReply))
	for i, e := range tree.ListEntries() {
		packet, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": i + 10, "method": "resources/read", "params": map[string]string{"uri": e.URI}})
		fmt.Fprintln(&input, string(packet))
	}
	fmt.Fprintln(&input, `{"jsonrpc":"2.0","id":99,"method":"tools/call","params":{"name":"get_lane_skill","arguments":{"lane":"ceo"}}}`)
	fmt.Fprintln(&input, `{"jsonrpc":"2.0","id":9007199254740993,"method":"ping"}`)
	fmt.Fprintln(&input, `{bad}`)
	cmd = exec.Command(nativeEnginePath, "mcp")
	cmd.Dir = mcpRoot
	cmd.Stdin = &input
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	output, err := cmd.Output()
	if err != nil || diagnostic.Len() != 0 {
		t.Fatalf("native MCP %v: %s", err, diagnostic.String())
	}
	packets := bytes.Split(bytes.TrimSpace(output), []byte("\n"))
	if len(packets) != tree.Count()+5 {
		t.Fatalf("unexpected packet count %d", len(packets))
	}
	for i, e := range tree.ListEntries() {
		var result struct {
			Result struct{ Contents []struct{ Text string } }
		}
		if err := json.Unmarshal(packets[i+2], &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Result.Contents) != 1 || computeSHA256([]byte(result.Result.Contents[0].Text)) != e.SHA256() {
			t.Fatalf("native resource mismatch %s", e.URI)
		}
	}
	if !bytes.Contains(output, []byte(`"id":9007199254740993`)) || !bytes.Contains(output, []byte(`"code":-32700`)) {
		t.Fatal("native protocol regression")
	}
	if _, err := os.Stat(filepath.Join(mcpRoot, "AGENTS.md")); err != nil {
		t.Fatal("native MCP roots did not sync")
	}
	t.Logf("native MCP: %d resource hashes, roots-bound sync, exact large ID and parse error verified", tree.Count())
	cmd = exec.Command(nativeEnginePath, "mcp")
	cmd.Dir = t.TempDir()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err = stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	t.Logf("native EOF exit: %s", time.Since(start))
}

func TestNativeConcurrentContextSync(t *testing.T) {
	if _, err := os.Stat(nativeEnginePath); err != nil {
		t.Fatal("run build.ps1 first")
	}
	root := t.TempDir()
	owner := strings.Repeat("    owner instruction with trailing spaces  \r\n", 300)
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(owner), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errors := make(chan error, 32)
	workers := make(chan struct{}, 16)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			workers <- struct{}{}
			defer func() { <-workers }()
			cmd := exec.Command(nativeEnginePath, "coder")
			cmd.Dir = root
			var diagnostic bytes.Buffer
			cmd.Stderr = &diagnostic
			if err := cmd.Run(); err != nil {
				errors <- err
			} else if diagnostic.Len() != 0 {
				errors <- fmt.Errorf("%s", diagnostic.String())
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.HasPrefix(raw, []byte(owner)) {
			t.Fatalf("native concurrent sync lost owner bytes: %s %v", name, err)
		}
	}
	t.Log("32 CLI invocations / 16 concurrent workers: both 300-line owner prefixes byte-exact")
}

func findAssetsDir(t *testing.T) string {
	t.Helper()
	candidates := []string{
		testAssetsRoot,
		"assets",
		"cmd/engine/assets",
		"../../cmd/engine/assets",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	t.Fatalf("could not locate assets directory from candidates: %v", candidates)
	return ""
}

func computeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// TestRunCLI_PrimaryCommands_8Roles xác nhận 8 vai trò chính thức và aliases chỉ xuất duy nhất SKILL.md nguyên bản
func TestRunCLI_PrimaryCommands_8Roles(t *testing.T) {
	assetsDir := findAssetsDir(t)

	tests := []struct {
		name             string
		roleArg          string
		expectedDiskFile string
		expectedBytes    int
	}{
		{
			name:             "CEO Role (81.7% token reduction, exactly 13,294 bytes)",
			roleArg:          "ceo",
			expectedDiskFile: filepath.Join(assetsDir, "ceo", "SKILL.md"),
			expectedBytes:    13294,
		},
		{
			name:             "System Architect Role",
			roleArg:          "system-architect",
			expectedDiskFile: filepath.Join(assetsDir, "system-architect", "SKILL.md"),
			expectedBytes:    7911,
		},
		{
			name:             "Architect Alias",
			roleArg:          "architect",
			expectedDiskFile: filepath.Join(assetsDir, "system-architect", "SKILL.md"),
			expectedBytes:    7911,
		},
		{
			name:             "Coder Role",
			roleArg:          "coder",
			expectedDiskFile: filepath.Join(assetsDir, "coder", "SKILL.md"),
			expectedBytes:    16647,
		},
		{
			name:             "QA Role",
			roleArg:          "qa",
			expectedDiskFile: filepath.Join(assetsDir, "qa", "SKILL.md"),
			expectedBytes:    5922,
		},
		{
			name:             "Supervisor Role",
			roleArg:          "supervisor",
			expectedDiskFile: filepath.Join(assetsDir, "supervisor", "SKILL.md"),
			expectedBytes:    4713,
		},
		{
			name:             "Mechanical Planner Role",
			roleArg:          "mechanical-planner",
			expectedDiskFile: filepath.Join(assetsDir, "planner", "SKILL.md"),
			expectedBytes:    10499,
		},
		{
			name:             "Strategic Planner Role",
			roleArg:          "strategic-planner",
			expectedDiskFile: filepath.Join(assetsDir, "planner", "SKILL.md"),
			expectedBytes:    10499,
		},
		{
			name:             "CEO Plugin Alias",
			roleArg:          "ceo-plugin",
			expectedDiskFile: filepath.Join(assetsDir, "ceo", "SKILL.md"),
			expectedBytes:    13294,
		},
		{
			name:             "Planner Alias",
			roleArg:          "planner",
			expectedDiskFile: filepath.Join(assetsDir, "planner", "SKILL.md"),
			expectedBytes:    10499,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diskBytes, err := os.ReadFile(tc.expectedDiskFile)
			if err != nil {
				t.Fatalf("failed to read expected disk file %s: %v", tc.expectedDiskFile, err)
			}

			if tc.expectedBytes > 0 && len(diskBytes) != tc.expectedBytes {
				t.Fatalf("disk file size mismatch for %s: expected %d bytes, got %d bytes", tc.expectedDiskFile, tc.expectedBytes, len(diskBytes))
			}

			expectedHash := computeSHA256(diskBytes)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			handled, exitCode := runCLI(&stdout, &stderr, []string{tc.roleArg})

			if !handled {
				t.Fatalf("expected handled=true for role %q, got false", tc.roleArg)
			}
			if exitCode != 0 {
				t.Fatalf("expected exitCode=0 for role %q, got %d, stderr: %s", tc.roleArg, exitCode, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Errorf("expected empty stderr, got: %s", stderr.String())
			}

			outBytes := stdout.Bytes()
			if len(outBytes) != len(diskBytes) {
				t.Fatalf("output length mismatch for %s: expected %d bytes, got %d bytes", tc.roleArg, len(diskBytes), len(outBytes))
			}

			actualHash := computeSHA256(outBytes)
			if actualHash != expectedHash {
				t.Fatalf("SHA-256 hash mismatch for role %q:\nexpected: %s\nactual:   %s", tc.roleArg, expectedHash, actualHash)
			}
		})
	}
}

// TestRunCLI_Subcommands xác nhận lệnh con xuất đúng duy nhất file reference hoặc template tương ứng (100% SHA-256 match)
func TestRunCLI_Subcommands(t *testing.T) {
	assetsDir := findAssetsDir(t)

	tests := []struct {
		name             string
		roleArg          string
		subArg           string
		expectedDiskFile string
		expectedBytes    int
	}{
		{
			name:             "CEO Template-Prompt-for-Opening-a-Session",
			roleArg:          "ceo",
			subArg:           "Template-Prompt-for-Opening-a-Session",
			expectedDiskFile: filepath.Join(assetsDir, "ceo", "references", "Template-Prompt-for-Opening-a-Session.md"),
			expectedBytes:    9395,
		},
		{
			name:             "CEO timer-rules",
			roleArg:          "ceo",
			subArg:           "timer-rules",
			expectedDiskFile: filepath.Join(assetsDir, "ceo", "references", "timer-rules.md"),
			expectedBytes:    17598,
		},
		{
			name:             "System Architect mode-1-spec-authoring-and-architecture",
			roleArg:          "system-architect",
			subArg:           "mode-1-spec-authoring-and-architecture",
			expectedDiskFile: filepath.Join(assetsDir, "system-architect", "references", "mode-1-spec-authoring-and-architecture.md"),
			expectedBytes:    11762,
		},
		{
			name:             "Architect mode-1-spec-authoring-and-architecture (Inherited from system-architect)",
			roleArg:          "architect",
			subArg:           "mode-1-spec-authoring-and-architecture",
			expectedDiskFile: filepath.Join(assetsDir, "system-architect", "references", "mode-1-spec-authoring-and-architecture.md"),
			expectedBytes:    11762,
		},
		{
			name:             "Strategic Planner plan-template",
			roleArg:          "strategic-planner",
			subArg:           "plan-template",
			expectedDiskFile: filepath.Join(assetsDir, "planner", "templates", "plan.template.md"),
			expectedBytes:    15479,
		},
		{
			name:             "Strategic Planner plan.template.md (With extension)",
			roleArg:          "strategic-planner",
			subArg:           "plan.template.md",
			expectedDiskFile: filepath.Join(assetsDir, "planner", "templates", "plan.template.md"),
			expectedBytes:    15479,
		},
		{
			name:             "QA backend-qa-protocol",
			roleArg:          "qa",
			subArg:           "backend-qa-protocol",
			expectedDiskFile: filepath.Join(assetsDir, "qa", "references", "backend-qa-protocol.md"),
			expectedBytes:    4275,
		},
		{
			name:             "Supervisor evidence-and-source-inspection",
			roleArg:          "supervisor",
			subArg:           "evidence-and-source-inspection",
			expectedDiskFile: filepath.Join(assetsDir, "supervisor", "references", "evidence-and-source-inspection.md"),
			expectedBytes:    2736,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diskBytes, err := os.ReadFile(tc.expectedDiskFile)
			if err != nil {
				t.Fatalf("failed to read expected disk file %s: %v", tc.expectedDiskFile, err)
			}

			if tc.expectedBytes > 0 && len(diskBytes) != tc.expectedBytes {
				t.Fatalf("disk file size mismatch for %s: expected %d bytes, got %d bytes", tc.expectedDiskFile, tc.expectedBytes, len(diskBytes))
			}

			expectedHash := computeSHA256(diskBytes)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			handled, exitCode := runCLI(&stdout, &stderr, []string{tc.roleArg, tc.subArg})

			if !handled {
				t.Fatalf("expected handled=true for subcommand %s %s, got false", tc.roleArg, tc.subArg)
			}
			if exitCode != 0 {
				t.Fatalf("expected exitCode=0 for subcommand %s %s, got %d, stderr: %s", tc.roleArg, tc.subArg, exitCode, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Errorf("expected empty stderr, got: %s", stderr.String())
			}

			outBytes := stdout.Bytes()
			if len(outBytes) != len(diskBytes) {
				t.Fatalf("output length mismatch for %s %s: expected %d bytes, got %d bytes", tc.roleArg, tc.subArg, len(diskBytes), len(outBytes))
			}

			actualHash := computeSHA256(outBytes)
			if actualHash != expectedHash {
				t.Fatalf("SHA-256 hash mismatch for subcommand %s %s:\nexpected: %s\nactual:   %s", tc.roleArg, tc.subArg, expectedHash, actualHash)
			}
		})
	}
}

// TestRunCLI_HelpAndMCP xác nhận các cờ help, --help, -h và chuyển tiếp cờ mcp
func TestRunCLI_HelpAndMCP(t *testing.T) {
	helpFlags := [][]string{
		{"help"},
		{"--help"},
		{"-h"},
		{"HELP"},
	}

	for _, flags := range helpFlags {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			handled, exitCode := runCLI(&stdout, &stderr, flags)

			if !handled {
				t.Fatalf("expected handled=true for help flag %v", flags)
			}
			if exitCode != 0 {
				t.Fatalf("expected exitCode=0 for help flag %v, got %d", flags, exitCode)
			}
			out := stdout.String()
			if !strings.Contains(out, "Command Selection Guide") {
				t.Errorf("expected output to contain 'Command Selection Guide', got: %s", out)
			}
			if !strings.Contains(out, "Specialized Subcommands (Grouped by Lane)") {
				t.Errorf("expected grouped subcommands, got: %s", out)
			}
			if !strings.Contains(out, "MCP Server Mode") {
				t.Errorf("expected output to contain 'MCP Server Mode', got: %s", out)
			}
			if !strings.Contains(out, "~/.ceo-plugin/bin/ceo-plugin.exe") {
				t.Errorf("expected output to contain executable fallback path, got: %s", out)
			}
		})
	}

	// Kiểm tra cờ mcp
	mcpFlags := [][]string{
		{"mcp"},
		{"MCP"},
	}
	for _, flags := range mcpFlags {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			handled, exitCode := runCLI(&stdout, &stderr, flags)

			if handled {
				t.Fatalf("expected handled=false for mcp flag %v (delegate to runMCPServer), got true", flags)
			}
			if exitCode != 0 {
				t.Fatalf("expected exitCode=0 for mcp flag %v, got %d", flags, exitCode)
			}
		})
	}

	// Kiểm tra args rỗng
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	handled, exitCode := runCLI(&stdout, &stderr, []string{})
	if handled {
		t.Fatalf("expected handled=false for empty args, got true")
	}
	if exitCode != 0 {
		t.Fatalf("expected exitCode=0 for empty args, got %d", exitCode)
	}
}

// TestRunCLI_InvalidArguments kiểm tra các trường hợp đối số sai cú pháp
func TestRunCLI_InvalidArguments(t *testing.T) {
	t.Run("Non-existent role", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		handled, exitCode := runCLI(&stdout, &stderr, []string{"non-existent-role"})

		if !handled {
			t.Fatalf("expected handled=true for invalid role, got false")
		}
		if exitCode != 1 {
			t.Fatalf("expected exitCode=1 for invalid role, got %d", exitCode)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout for invalid role, got: %s", stdout.String())
		}
		errStr := stderr.String()
		if !strings.Contains(errStr, `Lệnh không hợp lệ: role "non-existent-role" không tồn tại`) {
			t.Errorf("expected error message to contain role not found, got: %s", errStr)
		}
		if !strings.Contains(errStr, "Command Selection Guide") {
			t.Errorf("expected stderr to contain usage guide, got: %s", errStr)
		}
	})

	t.Run("Non-existent reference for existing role", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		handled, exitCode := runCLI(&stdout, &stderr, []string{"ceo", "non-existent-subcommand"})

		if !handled {
			t.Fatalf("expected handled=true for invalid reference, got false")
		}
		if exitCode != 1 {
			t.Fatalf("expected exitCode=1 for invalid reference, got %d", exitCode)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout for invalid reference, got: %s", stdout.String())
		}
		errStr := stderr.String()
		if !strings.Contains(errStr, `Lệnh không hợp lệ: không tìm thấy quy chế/biểu mẫu "non-existent-subcommand" trong vai trò "ceo"`) {
			t.Errorf("expected error message to contain reference not found, got: %s", errStr)
		}
		if !strings.Contains(errStr, "Specialized Subcommands (Grouped by Lane)") {
			t.Errorf("expected stderr to contain usage guide, got: %s", errStr)
		}
	})

	t.Run("Non-existent role with subcommand", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		handled, exitCode := runCLI(&stdout, &stderr, []string{"unknown-role", "some-subcommand"})

		if !handled {
			t.Fatalf("expected handled=true for invalid role with subcommand, got false")
		}
		if exitCode != 1 {
			t.Fatalf("expected exitCode=1 for invalid role, got %d", exitCode)
		}
		if stdout.Len() != 0 {
			t.Errorf("expected empty stdout, got: %s", stdout.String())
		}
		errStr := stderr.String()
		if !strings.Contains(errStr, `Lệnh không hợp lệ: role "unknown-role" không tồn tại`) {
			t.Errorf("expected error message to contain role not found, got: %s", errStr)
		}
	})
}

// Asset source files remain unchanged; context writes are tested separately.
func TestRunCLI_AssetSourceUnchanged(t *testing.T) {
	assetsDir := findAssetsDir(t)

	type fileState struct {
		modTime time.Time
		size    int64
	}

	beforeStates := make(map[string]fileState)
	err := filepath.WalkDir(assetsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		beforeStates[p] = fileState{
			modTime: info.ModTime(),
			size:    info.Size(),
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to record file states before CLI execution: %v", err)
	}

	// Thực thi một loạt các lệnh CLI chính và lệnh con
	commands := [][]string{
		{"ceo"},
		{"system-architect"},
		{"coder"},
		{"qa"},
		{"supervisor"},
		{"mechanical-planner"},
		{"strategic-planner"},
		{"ceo", "Template-Prompt-for-Opening-a-Session"},
		{"ceo", "timer-rules"},
		{"strategic-planner", "plan-template"},
		{"qa", "backend-qa-protocol"},
		{"supervisor", "evidence-and-source-inspection"},
	}

	for _, cmd := range commands {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		handled, exitCode := runCLI(&stdout, &stderr, cmd)
		if !handled || exitCode != 0 {
			t.Fatalf("CLI command %v failed: handled=%v, exitCode=%d, stderr=%s", cmd, handled, exitCode, stderr.String())
		}
	}

	// Kiểm tra lại toàn bộ trạng thái file sau khi thực thi
	afterStatesCount := 0
	err = filepath.WalkDir(assetsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		afterStatesCount++
		info, err := d.Info()
		if err != nil {
			return err
		}
		before, ok := beforeStates[p]
		if !ok {
			t.Errorf("new file detected after CLI execution: %s", p)
			return nil
		}
		if info.Size() != before.size {
			t.Errorf("file size changed for %s: before=%d, after=%d", p, before.size, info.Size())
		}
		if !info.ModTime().Equal(before.modTime) {
			t.Errorf("file modification time changed for %s: before=%v, after=%v", p, before.modTime, info.ModTime())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to check file states after CLI execution: %v", err)
	}

	if afterStatesCount != len(beforeStates) {
		t.Fatalf("total file count changed: before=%d, after=%d", len(beforeStates), afterStatesCount)
	}
}
