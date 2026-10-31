package patcher

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestJSONInvalidInputUnchanged(t *testing.T) {
	for _, raw := range []string{`{"mcpServers":{"other":{}},}`, `null`, `[]`, `{"mcpServers":null}`, `{"mcpServers":{},"mcpServers":{}}`, `{} {}`, `{"mcpServers":{"ceo-plugin":4}}`} {
		t.Run(raw, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.Stat(p)
			if err := PatchJSONConfig(p, `C:\test\ceo-plugin.exe`); err == nil {
				t.Fatal("accepted invalid input")
			}
			got, _ := os.ReadFile(p)
			after, _ := os.Stat(p)
			if string(got) != raw || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("invalid config was modified")
			}
		})
	}
}

func TestJSONPreservesNumbersAndPluginExtras(t *testing.T) {
	raw := []byte(`{"externalId":9007199254740993,"mcpServers":{"other":{"n":9007199254740993},"anvien-ceo-plugin":{"env":{"X":"fixture"}}}}`)
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := PatchJSONConfig(p, `C:\test\ceo-plugin.exe`); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(p)
	info, _ := os.Stat(p)
	if bytes.Count(first, []byte("9007199254740993")) != 2 || !bytes.Contains(first, []byte("fixture")) {
		t.Fatalf("lost values: %s", first)
	}
	if err := PatchJSONConfig(p, `C:\test\ceo-plugin.exe`); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(p)
	after, _ := os.Stat(p)
	if !bytes.Equal(first, second) || !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("second patch is not no-op")
	}
	if err := UnpatchJSONConfig(p); err != nil {
		t.Fatal(err)
	}
	last, _ := os.ReadFile(p)
	root, err := object(last)
	if err != nil {
		t.Fatal(err)
	}
	servers, _ := object(root["mcpServers"])
	if len(servers) != 1 || string(root["externalId"]) != "9007199254740993" {
		t.Fatalf("unpatch damaged other data: %s", last)
	}
	if err := UnpatchJSONConfig(p); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(p)
	if !bytes.Equal(last, again) {
		t.Fatal("unpatch not idempotent")
	}
}

func TestTOMLQuotedHeaderNestedEnvAndValueTypes(t *testing.T) {
	raw := []byte("owner_id = 9007199254740993\nwhen = 2026-10-28T12:34:56Z\nlocal_date = 2026-10-28\n[mcp_servers.other]\ncommand = 'node'\n[mcp_servers.\"ceo-plugin\"] # valid comment\ncommand = 'old'\n[mcp_servers.\"ceo-plugin\".env]\nX = 'fixture'\n")
	var original map[string]interface{}
	if err := toml.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := PatchCodexTOML(p, `C:\new\ceo-plugin.exe`); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(p)
	var parsed map[string]interface{}
	if err := toml.Unmarshal(first, &parsed); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"owner_id", "when", "local_date"} {
		if !reflect.DeepEqual(original[key], parsed[key]) {
			t.Fatalf("changed %s: %#v -> %#v", key, original[key], parsed[key])
		}
	}
	if err := PatchCodexTOML(p, `C:\new\ceo-plugin.exe`); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(p)
	if !bytes.Equal(first, second) {
		t.Fatal("patch not idempotent")
	}
	if err := UnpatchCodexTOML(p); err != nil {
		t.Fatal(err)
	}
	last, _ := os.ReadFile(p)
	parsed = nil
	if err := toml.Unmarshal(last, &parsed); err != nil {
		t.Fatal(err)
	}
	servers := parsed["mcp_servers"].(map[string]interface{})
	if len(servers) != 1 || servers["other"] == nil {
		t.Fatalf("wrong remaining servers: %v", servers)
	}
	if err := UnpatchCodexTOML(p); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(p)
	if !bytes.Equal(last, again) {
		t.Fatal("unpatch not idempotent")
	}
}

func TestMissingUnpatchAndGlobalFailure(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing", "config.json")
	if err := UnpatchJSONConfig(missing); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatal("unpatch created parent")
	}
	p := filepath.Join(dir, "bad.toml")
	raw := []byte("[broken")
	if err := os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	results, err := Apply([]Target{{"fixture", p, true}}, "exe", false)
	if err == nil || results[0].Status != "failed" {
		t.Fatalf("hidden failure: %v %v", results, err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(raw, got) {
		t.Fatal("overwrote invalid TOML")
	}
}

func TestConflictingLegacyExtrasFail(t *testing.T) {
	raw := []byte(`{"mcpServers":{"ceo-plugin":{"env":{"X":"one"}},"anvien-ceo-plugin":{"env":{"X":"two"}}}}`)
	if _, err := TransformJSON(raw, true, "exe", false); err == nil {
		t.Fatal("lost conflicting extras")
	}
	if !json.Valid(raw) {
		t.Fatal("bad fixture")
	}
}

func TestGlobalTargetsAntigravityConfigPath(t *testing.T) {
	targets, err := GlobalTargets()
	if err != nil {
		t.Fatalf("GlobalTargets failed: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}
	expectedPath := filepath.Join(home, ".gemini", "config", "mcp_config.json")

	var antigravityTarget *Target
	for i := range targets {
		if targets[i].IDE == "Antigravity" {
			antigravityTarget = &targets[i]
			break
		}
	}

	if antigravityTarget == nil {
		t.Fatal("Antigravity target not found in GlobalTargets()")
	}
	if antigravityTarget.Path != expectedPath {
		t.Fatalf("expected Antigravity path %q, got %q", expectedPath, antigravityTarget.Path)
	}
	if antigravityTarget.TOML {
		t.Fatal("expected Antigravity format to be JSON (TOML=false)")
	}
}

func TestAntigravityConfigPatchAndUnpatch(t *testing.T) {
	tempHome := t.TempDir()
	antigravityConfig := filepath.Join(tempHome, ".gemini", "config", "mcp_config.json")
	target := Target{IDE: "Antigravity", Path: antigravityConfig, TOML: false}

	binaryPath := `C:\Users\TestUser\.ceo-plugin\bin\ceo-plugin.exe`
	results, err := Apply([]Target{target}, binaryPath, false)
	if err != nil {
		t.Fatalf("Apply patch failed: %v", err)
	}
	if len(results) != 1 || results[0].Status != "created" {
		t.Fatalf("unexpected patch result: %+v", results)
	}

	raw, err := os.ReadFile(antigravityConfig)
	if err != nil {
		t.Fatalf("failed to read patched config: %v", err)
	}
	var parsed struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	server, ok := parsed.MCPServers["ceo-plugin"]
	if !ok {
		t.Fatalf("ceo-plugin server missing in config: %s", string(raw))
	}
	if server.Command != binaryPath || len(server.Args) != 1 || server.Args[0] != "mcp" {
		t.Fatalf("unexpected server config: %+v", server)
	}

	// Unpatch
	unpatchResults, err := Apply([]Target{target}, "", true)
	if err != nil {
		t.Fatalf("Apply unpatch failed: %v", err)
	}
	if len(unpatchResults) != 1 || unpatchResults[0].Status != "updated" {
		t.Fatalf("unexpected unpatch result: %+v", unpatchResults)
	}
	rawAfter, _ := os.ReadFile(antigravityConfig)
	if bytes.Contains(rawAfter, []byte("ceo-plugin")) {
		t.Fatalf("ceo-plugin still present after unpatch: %s", string(rawAfter))
	}
}

