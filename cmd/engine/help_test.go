package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ceo-plugin/internal/virtualfs"
)

type helpCase struct {
	args   []string
	target string
}

func helpSyntaxCases() []helpCase {
	cases := []helpCase{{[]string{"help"}, ""}, {[]string{"--help"}, ""}, {[]string{"-h"}, ""}}
	for _, lane := range append(append([]string{}, virtualfs.PrimaryRoles...), "init") {
		cases = append(cases, helpCase{[]string{"help", lane}, lane})
		for _, flag := range []string{"help", "--help", "-h"} {
			cases = append(cases, helpCase{[]string{lane, flag}, lane})
		}
	}
	return cases
}

// Check the public output against the asset catalog, including row descriptions
// and absence of unrelated references. Do not use the help renderers as an oracle.
func assertHelpOutput(t *testing.T, text, target string) {
	t.Helper()
	if target == "init" {
		for _, want := range []string{"CEO Plugin Workspace Initialization", "ceo-plugin init", "nearest valid Git workspace root", "git init", "/AGENTS.md", "/CLAUDE.md", "user-authored content outside", "Idempotent", "tracked", "warning", "PATH", "no additional arguments"} {
			if !strings.Contains(text, want) {
				t.Errorf("init help missing %q", want)
			}
		}
		return
	}
	commands, err := getCatalog()
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]int{}
	commandRows := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		rows[line]++
		if strings.HasPrefix(line, "ceo-plugin ") {
			commandRows++
		}
	}
	canonical := target
	switch target {
	case "architect":
		canonical = "system-architect"
	case "mechanical-planner":
		canonical = "strategic-planner"
	}
	if target == "" {
		for _, heading := range []string{"Help & Discovery:", "Command Selection Guide", "Specialized Subcommands (Grouped by Lane)", "CEO — Orchestration & Coordination", "System Architect — Architecture & SPEC", "QA — Verification Protocols & Quality Rules", "Supervisor — Zero-Trust & Audit Rules", "Strategic Planner — Plan & Ledger Templates"} {
			if !strings.Contains(text, heading) {
				t.Errorf("missing heading %q", heading)
			}
		}
	} else if !strings.Contains(text, "Role: "+target+" (") {
		t.Fatalf("wrong role: %s", text)
	}
	expectedRows := 0
	for _, cmd := range commands {
		if target != "" && !((cmd.Reference == "" && cmd.Lane == target) || (cmd.Reference != "" && cmd.Lane == canonical)) {
			continue
		}
		lane := cmd.Lane
		if target != "" {
			lane = target
		}
		fields := []string{"ceo-plugin", lane}
		if cmd.Reference != "" {
			fields = append(fields, cmd.Reference)
		}
		fields = append(fields, cmd.Description)
		row := strings.Join(fields, " ")
		if rows[row] != 1 {
			t.Errorf("expected one complete command/description row %q, got %d", row, rows[row])
		}
		expectedRows++
	}
	if target != "" && commandRows != expectedRows {
		t.Fatalf("lane help leaked or omitted commands: got %d want %d\n%s", commandRows, expectedRows, text)
	}
	if target == "coder" && !strings.Contains(text, "No specialized subcommands") {
		t.Fatal("missing empty role guidance")
	}
	if canonical != target && !strings.Contains(text, "shared with '"+canonical+"'") {
		t.Fatal("alias reference source not explained")
	}
}

func TestCLIContextualHelpMatrix(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", "") // Help, especially init help, must not require Git.
	for _, tc := range helpSyntaxCases() {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, diag bytes.Buffer
			handled, code := runCLI(&out, &diag, tc.args)
			if !handled || code != 0 || diag.Len() != 0 {
				t.Fatalf("handled=%v code=%d stderr=%s", handled, code, diag.String())
			}
			assertHelpOutput(t, out.String(), tc.target)
		})
	}
	entries, err := os.ReadDir(".")
	if err != nil || len(entries) != 0 {
		t.Fatalf("help mutated workspace: %v %v", entries, err)
	}
}

func TestCLIHelpPreservesExistingWorkspace(t *testing.T) {
	t.Chdir(t.TempDir())
	files := map[string]string{"AGENTS.md": "<!-- ceo-plugin:start --> malformed", "CLAUDE.md": "owner text\r\n", ".gitignore": "owner-ignore\n"}
	before := map[string]os.FileInfo{}
	for name, content := range files {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = info
	}
	for _, tc := range helpSyntaxCases() {
		var out, diag bytes.Buffer
		if _, code := runCLI(&out, &diag, tc.args); code != 0 || diag.Len() != 0 {
			t.Fatalf("help attempted sync/init: %v %s", tc.args, diag.String())
		}
	}
	for name, content := range files {
		raw, err := os.ReadFile(name)
		if err != nil || string(raw) != content {
			t.Fatalf("help changed %s: %v", name, err)
		}
		info, err := os.Stat(name)
		if err != nil || !os.SameFile(before[name], info) || !before[name].ModTime().Equal(info.ModTime()) {
			t.Fatalf("help rewrote %s: %v", name, err)
		}
	}
	entries, err := os.ReadDir(".")
	if err != nil || len(entries) != len(files) {
		t.Fatalf("help created files: %v %v", entries, err)
	}
}

func TestCLIHelpErrorsAndNormalization(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"help", "non-existent-role"}, {"unknown", "--help"}, {"unknown", "help"}, {"unknown", "-h"}, {"help", ""}} {
		var out, diag bytes.Buffer
		handled, code := runCLI(&out, &diag, args)
		if !handled || code != 1 || out.Len() != 0 || !strings.Contains(diag.String(), "Valid roles:") || strings.Contains(diag.String(), "Subcommands") {
			t.Fatalf("unknown target: %v %d %s %s", args, code, out.String(), diag.String())
		}
		for _, role := range virtualfs.PrimaryRoles {
			if !strings.Contains(diag.String(), role) {
				t.Fatalf("missing valid role %s", role)
			}
		}
	}
	for _, args := range [][]string{{"help", "ceo", "extra"}, {"init", "--help", "extra"}, {"ceo", "help", "extra"}, {"ceo", "timer-rules", "--help"}, {"--help", "ceo"}, {"-h", "ceo"}} {
		var out, diag bytes.Buffer
		if _, code := runCLI(&out, &diag, args); code != 1 || out.Len() != 0 || diag.Len() == 0 {
			t.Fatalf("bad arity: %v %d %s %s", args, code, out.String(), diag.String())
		}
	}
	for _, tc := range []helpCase{{[]string{" HELP ", " QA "}, "qa"}, {[]string{" SYSTEM_ARCHITECT ", " --HELP "}, "system-architect"}, {[]string{" INIT ", " -H "}, "init"}} {
		var out, diag bytes.Buffer
		if _, code := runCLI(&out, &diag, tc.args); code != 0 || diag.Len() != 0 {
			t.Fatalf("normalization: %v %s", tc.args, diag.String())
		}
		assertHelpOutput(t, out.String(), tc.target)
	}
}

type failedHelpWriter struct{}

func (failedHelpWriter) Write([]byte) (int, error) { return 0, errors.New("help output unavailable") }

func TestCLIHelpOutputFailure(t *testing.T) {
	for _, writer := range []io.Writer{shortOutput{}, failedHelpWriter{}} {
		for _, args := range [][]string{{"help"}, {"help", "ceo"}, {"init", "--help"}} {
			var diag bytes.Buffer
			if _, code := runCLI(writer, &diag, args); code != 1 || diag.Len() == 0 {
				t.Fatalf("output failure hidden: %v %s", args, diag.String())
			}
		}
	}
}

func TestNativeContextualHelpMatrix(t *testing.T) {
	root := t.TempDir()
	for _, tc := range helpSyntaxCases() {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cmd := exec.Command(nativeEnginePath, tc.args...)
			cmd.Dir = root
			var diag bytes.Buffer
			cmd.Stderr = &diag
			out, err := cmd.Output()
			if err != nil || diag.Len() != 0 {
				t.Fatalf("native help: %v %s", err, diag.String())
			}
			assertHelpOutput(t, string(out), tc.target)
		})
	}
	for _, args := range [][]string{{"help", "unknown"}, {"unknown", "--help"}, {"help", "ceo", "extra"}, {"init", "--help", "extra"}} {
		cmd := exec.Command(nativeEnginePath, args...)
		cmd.Dir = root
		var out, diag bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &diag
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || out.Len() != 0 || diag.Len() == 0 {
			t.Fatalf("native error contract: %v %v %s %s", args, err, out.String(), diag.String())
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("native help created context/Git: %v %v", entries, err)
	}
	t.Logf("native syntax matrix: %d valid forms and 4 errors; no files created", len(helpSyntaxCases()))
}

func TestNativeHelpPreservesDefaultMCP(t *testing.T) {
	for _, args := range [][]string{nil, {"mcp"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, nativeEnginePath, args...)
		cmd.Dir = t.TempDir()
		cmd.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":123,\"method\":\"ping\"}\n")
		var diag bytes.Buffer
		cmd.Stderr = &diag
		out, err := cmd.Output()
		cancel()
		if err != nil || diag.Len() != 0 {
			t.Fatalf("MCP mode %v: %v %s", args, err, diag.String())
		}
		var response struct {
			JSONRPC string
			ID      int
			Result  map[string]any
		}
		if err := json.Unmarshal(out, &response); err != nil || response.JSONRPC != "2.0" || response.ID != 123 || response.Result == nil {
			t.Fatalf("help contaminated MCP %v: %v %s", args, err, out)
		}
		if _, err := os.Stat(filepath.Join(cmd.Dir, "AGENTS.md")); !os.IsNotExist(err) {
			t.Fatalf("MCP ping wrote context: %v", err)
		}
	}
}
