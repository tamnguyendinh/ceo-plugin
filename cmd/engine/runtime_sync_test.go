package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"ceo-plugin/internal/aicontext"
)

func TestNativeRuntimeSyncCreatesContextInGitRoot(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", root)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, b)
	}
	sub := filepath.Join(root, "nested")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"missing", "deleted"} {
		if phase == "deleted" {
			for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
				if err := os.Remove(filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
			}
		}
		cmd = exec.Command(nativeEnginePath, "ceo")
		cmd.Dir = sub
		var diagnostic bytes.Buffer
		cmd.Stderr = &diagnostic
		b, err := cmd.Output()
		if err != nil || len(b) == 0 || diagnostic.Len() != 0 {
			t.Fatalf("%s: %v %s", phase, err, diagnostic.String())
		}
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			raw, err := os.ReadFile(filepath.Join(root, name))
			if err != nil || !bytes.Contains(raw, []byte(aicontext.StartMarker)) {
				t.Fatalf("%s missing %s: %v", phase, name, err)
			}
		}
		if entries, err := os.ReadDir(sub); err != nil || len(entries) != 0 {
			t.Fatalf("%s wrote into nested directory: %v %v", phase, entries, err)
		}
		if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
			t.Fatalf("runtime unexpectedly took init's ignore responsibility: %v", err)
		}
	}
	t.Log("native CLI: missing and deleted context files created in nearest Git root from nested cwd")
}

func TestNativeMCPRootsAndRuntimeCreateContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root := t.TempDir()
	cmd := exec.CommandContext(ctx, nativeEnginePath, "mcp")
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("native MCP exit: %v %s", err, diagnostic.String())
		}
		if diagnostic.Len() != 0 {
			t.Errorf("unexpected MCP diagnostic: %s", diagnostic.String())
		}
	}()
	enc, dec := json.NewEncoder(stdin), json.NewDecoder(stdout)
	send := func(packet any) {
		t.Helper()
		if err := enc.Encode(packet); err != nil {
			t.Fatal(err)
		}
	}
	read := func() map[string]json.RawMessage {
		t.Helper()
		var packet map[string]json.RawMessage
		if err := dec.Decode(&packet); err != nil {
			t.Fatal(err)
		}
		if packet["error"] != nil {
			t.Fatalf("protocol error: %s", packet["error"])
		}
		return packet
	}
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"roots": map[string]any{}}, "clientInfo": map[string]string{"name": "runtime-sync-test", "version": "1"}}})
	read()
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	request := read()
	if string(request["method"]) != `"roots/list"` {
		t.Fatalf("missing roots request: %v", request)
	}
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(root)}
	send(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{"roots": []map[string]string{{"uri": u.String()}}}})
	// Ping is a processing barrier: no tool has run before checking creation.
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "ping"})
	read()
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		path := filepath.Join(root, name)
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(raw, []byte(aicontext.StartMarker)) {
			t.Fatalf("roots did not create %s: %v", name, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	tree, err := getAssetTree()
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := tree.GetSkill("ceo")
	for _, method := range []string{"tools/call", "prompts/get"} {
		for _, phase := range []string{"missing", "owner-update", "unchanged", "missing-sibling"} {
			status := "updated"
			if phase == "unchanged" {
				status = "unchanged"
			}
			if phase == "owner-update" {
				if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("owner notes\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "missing-sibling" {
				if err := os.Remove(filepath.Join(root, "CLAUDE.md")); err != nil {
					t.Fatal(err)
				}
			}
			params := map[string]any{"name": "ceo"}
			if method == "tools/call" {
				params = map[string]any{"name": "get_lane_skill", "arguments": map[string]string{"lane": "ceo"}}
			}
			send(map[string]any{"jsonrpc": "2.0", "id": 3, "method": method, "params": params})
			packet := read()
			var result struct {
				Meta     map[string]aicontext.SyncResult `json:"_meta"`
				Content  []struct{ Text string }
				Messages []struct{ Content struct{ Text string } }
			}
			if err := json.Unmarshal(packet["result"], &result); err != nil {
				t.Fatal(err)
			}
			r := result.Meta["ceo-plugin/context-sync"]
			if r.Status != status || len(r.Files) != 2 {
				t.Fatalf("%s/%s: want %s, got %+v", method, phase, status, r)
			}
			for _, f := range r.Files {
				if (phase == "missing" || (phase == "missing-sibling" && filepath.Base(f.Path) == "CLAUDE.md")) && f.Status != "created" {
					t.Fatalf("%s/%s: missing file not reported created: %+v", method, phase, f)
				}
			}
			text := ""
			if len(result.Content) == 1 {
				text = result.Content[0].Text
			} else if len(result.Messages) == 1 {
				text = result.Messages[0].Content.Text
			}
			if text != entry.Content {
				t.Fatal("sync changed raw document payload")
			}
			for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
				raw, err := os.ReadFile(filepath.Join(root, name))
				if err != nil || !bytes.Contains(raw, []byte(aicontext.StartMarker)) {
					t.Fatalf("%s/%s missing context %s: %v", method, phase, name, err)
				}
				if name == "AGENTS.md" && phase != "missing" && !bytes.HasPrefix(raw, []byte("owner notes\n")) {
					t.Fatalf("%s/%s lost owner text", method, phase)
				}
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 2 {
				t.Fatalf("%s/%s unexpected workspace files: %v %v", method, phase, entries, err)
			}
		}
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			if err := os.Remove(filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Log("native MCP: roots creates both files before tools; tool and prompt recreate deleted files, restore missing sibling, preserve owner text and payload, and report no-op")
}
