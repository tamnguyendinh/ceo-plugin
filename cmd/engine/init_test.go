package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ceo-plugin/internal/aicontext"
	"ceo-plugin/internal/fileupdate"
)

func TestCLIInit(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	var out, diagnostic bytes.Buffer
	for _, args := range [][]string{{"init", "extra"}, {"init", "--help", "extra"}, {"init", "a", "b"}} {
		handled, code := runCLI(&out, &diagnostic, args)
		if !handled || code != 1 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "extra arguments") {
			t.Fatalf("%v: %d %s", args, code, diagnostic.String())
		}
		out.Reset()
		diagnostic.Reset()
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("invalid arity changed filesystem")
	}
	handled, code := runCLI(&out, &diagnostic, []string{"init"})
	if !handled || code != 0 || !strings.Contains(out.String(), "initialized successfully") || !strings.Contains(out.String(), ".gitignore") || diagnostic.Len() != 0 {
		t.Fatalf("%d %s %s", code, out.String(), diagnostic.String())
	}
	for _, name := range []string{".git", "AGENTS.md", "CLAUDE.md", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	diagnostic.Reset()
	_, code = runCLI(&out, &diagnostic, []string{"init"})
	if code != 0 || strings.Count(out.String(), "unchanged") != 3 {
		t.Fatalf("%d %s %s", code, out.String(), diagnostic.String())
	}
	out.Reset()
	diagnostic.Reset()
	_, code = runCLI(&out, &diagnostic, []string{"help"})
	if code != 0 || !strings.Contains(out.String(), "Workspace Initialization: ceo-plugin init") {
		t.Fatal("missing help")
	}
}

func TestCLIInitErrors(t *testing.T) {
	t.Run("missing-git", func(t *testing.T) {
		root := t.TempDir()
		t.Chdir(root)
		t.Setenv("PATH", "")
		var out, diag bytes.Buffer
		_, code := runCLI(&out, &diag, []string{"init"})
		if code != 1 || out.Len() != 0 || !strings.Contains(diag.String(), "git_not_found") {
			t.Fatalf("%d %s %s", code, out.String(), diag.String())
		}
		if entries, _ := os.ReadDir(root); len(entries) != 0 {
			t.Fatal("missing Git wrote files")
		}
	})
	t.Run("unsafe", func(t *testing.T) {
		root := t.TempDir()
		t.Chdir(root)
		t.Setenv("USERPROFILE", root)
		var out, diag bytes.Buffer
		_, code := runCLI(&out, &diag, []string{"init"})
		if code != 1 || out.Len() != 0 || !strings.Contains(diag.String(), "unsafe_workspace_root") {
			t.Fatalf("%d %s %s", code, out.String(), diag.String())
		}
	})
	t.Run("preflight", func(t *testing.T) {
		root := t.TempDir()
		t.Chdir(root)
		if err := os.Mkdir("AGENTS.md", 0700); err != nil {
			t.Fatal(err)
		}
		var out, diag bytes.Buffer
		_, code := runCLI(&out, &diag, []string{"init"})
		if code != 1 || out.Len() != 0 || !strings.Contains(diag.String(), "stage=preflight") {
			t.Fatalf("%d %s %s", code, out.String(), diag.String())
		}
		if _, err := os.Stat(".git"); !os.IsNotExist(err) {
			t.Fatal("preflight initialized Git")
		}
	})
}

func TestCLIInitOutputContract(t *testing.T) {
	r := aicontext.InitResult{Status: "success", Stage: "complete", GitAction: "existing", WorkspaceRoot: "workspace", Files: []aicontext.FileResult{
		{Result: fileupdate.Result{Path: ".gitignore", Status: "updated"}},
		{Result: fileupdate.Result{Path: "AGENTS.md", Status: "updated"}},
		{Result: fileupdate.Result{Path: "CLAUDE.md", Status: "unchanged"}},
	}}
	var out, diag bytes.Buffer
	r.Warnings = []string{"AGENTS.md is already tracked; the index is unchanged."}
	if code := writeInitResult(&out, &diag, r, nil); code != 0 || !strings.Contains(diag.String(), "already tracked") || strings.Contains(out.String(), "Warning") {
		t.Fatalf("%d %s %s", code, out.String(), diag.String())
	}
	out.Reset()
	diag.Reset()
	if code := writeInitResult(&out, shortOutput{}, r, nil); code != 1 || strings.Contains(out.String(), "initialized successfully") {
		t.Fatal("ignored warning short write")
	}
	r.Warnings = nil
	if code := writeInitResult(shortOutput{}, &diag, r, nil); code != 1 || !strings.Contains(diag.String(), "result output failed") {
		t.Fatal("ignored result short write")
	}
	out.Reset()
	diag.Reset()
	r.Status, r.Stage = "partial", "context"
	r.Files[2].Status = "recovery_required"
	r.Files[2].Recovery = []string{"recovery-path"}
	if code := writeInitResult(&out, &diag, r, errors.New("injected sync failure")); code != 1 || out.Len() != 0 || !strings.Contains(diag.String(), "recovery-path") || !strings.Contains(diag.String(), "retained") {
		t.Fatal("lost partial result", diag.String())
	}
	out.Reset()
	if code := writeInitResult(&out, shortOutput{}, r, errors.New("failure")); code != 1 || !strings.Contains(out.String(), "Diagnostic output failed") {
		t.Fatal("diagnostic fallback missing")
	}
}

func TestNativeBinaryInit(t *testing.T) {
	if _, err := os.Stat(nativeEnginePath); err != nil {
		t.Fatal("run build.ps1 before native tests:", err)
	}
	root := t.TempDir()
	run := func(dir string, args ...string) (string, string, error) {
		cmd := exec.Command(nativeEnginePath, args...)
		cmd.Dir = dir
		var out, diag bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &diag
		err := cmd.Run()
		return out.String(), diag.String(), err
	}
	out, diag, err := run(root, "init")
	if err != nil || diag != "" || !strings.Contains(out, "initialized successfully") {
		t.Fatalf("%v %s %s", err, out, diag)
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		content, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil || !bytes.Contains(content, []byte("### Workspace Initialization — `ceo-plugin init`")) ||
			!bytes.Contains(content, []byte("### Command Help & Discovery")) ||
			!bytes.Contains(content, []byte(`& "$HOME\.ceo-plugin\bin\ceo-plugin.exe" init`)) ||
			!bytes.Contains(content, []byte("runs `git init` in the current directory")) {
			t.Fatalf("native init did not render invocation/effects in %s: %v", name, readErr)
		}
		cmd := exec.Command("git", "-C", root, "check-ignore", "--no-index", "--", name)
		if b, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(b)) != name {
			t.Fatalf("ignore %s: %v %s", name, err, b)
		}
	}
	sub := filepath.Join(root, "nested")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	out, diag, err = run(sub, "init")
	if err != nil || diag != "" || strings.Count(out, "unchanged") != 3 {
		t.Fatalf("%v %s %s", err, out, diag)
	}
	if entries, _ := os.ReadDir(sub); len(entries) != 0 {
		t.Fatal("nested init wrote files")
	}
	out, diag, err = run(root, "init", "extra")
	if err == nil || out != "" || !strings.Contains(diag, "extra arguments") {
		t.Fatal(err, out, diag)
	}
	cmd := exec.Command("git", "-C", root, "add", "-f", "AGENTS.md")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	out, diag, err = run(root, "init")
	if err != nil || !strings.Contains(diag, "already tracked") || !strings.Contains(out, "initialized successfully") {
		t.Fatal(err, out, diag)
	}
	t.Log("native init: fresh repo, effective ignore, nested idempotence, invalid arity, tracked warning")
}
