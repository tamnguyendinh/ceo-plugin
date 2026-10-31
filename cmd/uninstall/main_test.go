package main

import (
	"bytes"
	"ceo-plugin/internal/aicontext"
	"ceo-plugin/internal/installstate"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"ceo-plugin/internal/patcher"
)

// TestRollback_Patcher_MissingFiles tests that unpatching non-existent JSON/TOML configuration
// files returns nil gracefully without panicking or failing.
func TestRollback_Patcher_MissingFiles(t *testing.T) {
	tempDir := t.TempDir()

	missingJSON := filepath.Join(tempDir, "non_existent.json")
	if err := patcher.UnpatchJSONConfig(missingJSON); err != nil {
		t.Errorf("expected nil error for missing JSON config, got: %v", err)
	}

	missingTOML := filepath.Join(tempDir, "non_existent.toml")
	if err := patcher.UnpatchCodexTOML(missingTOML); err != nil {
		t.Errorf("expected nil error for missing TOML config, got: %v", err)
	}
}

// TestRollback_Patcher_JSONIdempotency verifies that unpatching JSON MCP configs cleanly removes
// both "ceo-plugin" and legacy "anvien-ceo-plugin" keys while preserving custom servers,
// and behaves idempotently across multiple invocations.
func TestRollback_Patcher_JSONIdempotency(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "settings.json")

	initialJSON := `{
  "mcpServers": {
    "ceo-plugin": {
      "command": "C:\\Users\\User\\.ceo-plugin\\bin\\ceo-plugin.exe",
      "args": ["mcp"]
    },
    "anvien-ceo-plugin": {
      "command": "C:\\legacy\\anvien-ceo-plugin.exe",
      "args": ["mcp"]
    },
    "user-custom-tool": {
      "command": "node",
      "args": ["C:\\tools\\server.js"]
    }
  }
}`

	if err := os.WriteFile(configPath, []byte(initialJSON), 0644); err != nil {
		t.Fatalf("failed to write initial JSON config: %v", err)
	}

	// First pass: unpatch
	if err := patcher.UnpatchJSONConfig(configPath); err != nil {
		t.Fatalf("UnpatchJSONConfig returned unexpected error: %v", err)
	}

	data1, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config after first unpatch: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data1, &parsed); err != nil {
		t.Fatalf("failed to parse JSON after first unpatch: %v", err)
	}

	mcpServers, ok := parsed["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected mcpServers object in JSON")
	}

	if _, exists := mcpServers["ceo-plugin"]; exists {
		t.Errorf("expected 'ceo-plugin' to be removed from mcpServers")
	}
	if _, exists := mcpServers["anvien-ceo-plugin"]; exists {
		t.Errorf("expected 'anvien-ceo-plugin' to be removed from mcpServers")
	}
	if custom, exists := mcpServers["user-custom-tool"]; !exists {
		t.Errorf("expected 'user-custom-tool' to be preserved")
	} else {
		customMap, ok := custom.(map[string]interface{})
		if !ok || customMap["command"] != "node" {
			t.Errorf("expected 'user-custom-tool' to remain unmodified, got: %v", custom)
		}
	}

	// Second pass: idempotent verification
	if err := patcher.UnpatchJSONConfig(configPath); err != nil {
		t.Fatalf("second UnpatchJSONConfig returned unexpected error: %v", err)
	}

	data2, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config after second unpatch: %v", err)
	}

	if string(data1) != string(data2) {
		t.Errorf("UnpatchJSONConfig is not idempotent:\nPass 1:\n%s\nPass 2:\n%s", string(data1), string(data2))
	}
}

// TestRollback_Patcher_CodexTOMLIdempotency verifies that unpatching Codex TOML configs cleanly
// removes [mcp_servers.ceo-plugin] and legacy [mcp_servers.anvien-ceo-plugin] sections while
// preserving user configurations, and operates idempotently.
func TestRollback_Patcher_CodexTOMLIdempotency(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")

	initialTOML := `model = "gpt-4o"
temperature = 0.7

[mcp_servers.ceo-plugin]
command = "C:\\Users\\User\\.ceo-plugin\\bin\\ceo-plugin.exe"
args = ["mcp"]

[mcp_servers.custom_service]
command = "python"
args = ["-m", "custom_mcp"]
`

	if err := os.WriteFile(configPath, []byte(initialTOML), 0644); err != nil {
		t.Fatalf("failed to write initial TOML config: %v", err)
	}

	// First pass: unpatch
	if err := patcher.UnpatchCodexTOML(configPath); err != nil {
		t.Fatalf("UnpatchCodexTOML returned unexpected error: %v", err)
	}

	data1, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read TOML config after first unpatch: %v", err)
	}
	content1 := string(data1)

	if strings.Contains(content1, "[mcp_servers.ceo-plugin]") {
		t.Errorf("expected [mcp_servers.ceo-plugin] section to be removed")
	}
	if strings.Contains(content1, "ceo-plugin.exe") {
		t.Errorf("expected ceo-plugin.exe to be removed")
	}
	if !strings.Contains(content1, "[mcp_servers.custom_service]") {
		t.Errorf("expected [mcp_servers.custom_service] section to be preserved")
	}
	if !strings.Contains(content1, `gpt-4o`) {
		t.Errorf("expected model configuration to be preserved")
	}

	// Second pass: idempotent verification
	if err := patcher.UnpatchCodexTOML(configPath); err != nil {
		t.Fatalf("second UnpatchCodexTOML returned unexpected error: %v", err)
	}

	data2, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read TOML config after second unpatch: %v", err)
	}
	content2 := string(data2)

	if content1 != content2 {
		t.Errorf("UnpatchCodexTOML is not idempotent:\nPass 1:\n%s\nPass 2:\n%s", content1, content2)
	}
}

func TestMain(m *testing.M) {
	if root := os.Getenv("CEO_TEST_BOOTSTRAP_ROOT"); root != "" && len(os.Args) == 5 && os.Args[1] == "--internal-remove" {
		executing, err := os.Executable()
		if err != nil {
			panic(err)
		}
		ops := uninstallOps{removePath: func(string) error { return nil }, unregister: func() error { return nil },
			expectedHash: os.Args[2], expectedStamp: os.Args[3], expectedGeneration: os.Args[4]}
		targets := []patcher.Target{{IDE: "fixture", Path: filepath.Join(root, "config.json")}}
		err = runUninstall(root, executing, targets, os.Stdout, ops)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func prepareInstallation(t *testing.T) (string, string, []patcher.Target) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "install")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	ownExe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.ReadFile(ownExe)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"ceo-plugin.exe": []byte("fixture engine"), "ceo-plugin-uninstall.exe": executable, "owner.txt": []byte("keep exactly")} {
		if err := os.WriteFile(filepath.Join(bin, name), data, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := installstate.NewIdentity(root); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "config.json")
	if err := os.WriteFile(config, []byte("{\"mcpServers\":{\"ceo-plugin\":{},\"other\":{}},\"id\":9007199254740993}"), 0600); err != nil {
		t.Fatal(err)
	}
	return root, ownExe, []patcher.Target{{IDE: "fixture", Path: config}}
}

func TestUninstallSynchronousAndRetry(t *testing.T) {
	root, executing, targets := prepareInstallation(t)

	// Pre-create instructions files in mock home
	home := filepath.Dir(root)
	instructionTargets := aicontext.DefaultInstructionTargets(home, "")
	if err := aicontext.ExportMCPInstructions(instructionTargets, io.Discard); err != nil {
		t.Fatalf("setup instructions failed: %v", err)
	}
	for _, it := range instructionTargets {
		if it.Path == "" {
			continue
		}
		if _, err := os.Stat(it.Path); err != nil {
			t.Fatalf("expected instructions file before uninstall: %s", it.Path)
		}
	}

	var out bytes.Buffer
	calls := 0
	ops := uninstallOps{removePath: func(string) error { calls++; return nil }, unregister: func() error { calls++; return nil }}
	if err := runUninstall(root, executing, targets, &out, ops); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || !strings.Contains(out.String(), "complete:") {
		t.Fatalf("bad terminal result: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "ceo-plugin.exe")); !os.IsNotExist(err) {
		t.Fatal("engine remains")
	}
	b, err := os.ReadFile(filepath.Join(root, "bin", "owner.txt"))
	if err != nil || string(b) != "keep exactly" {
		t.Fatal("deleted unowned file")
	}
	// Assert instructions files and directories are completely cleaned up
	for _, it := range instructionTargets {
		if it.Path == "" {
			continue
		}
		if _, err := os.Stat(it.Path); !os.IsNotExist(err) {
			t.Fatalf("instructions file remains after uninstall: %s", it.Path)
		}
		dir := filepath.Dir(it.Path)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("empty ceo-plugin directory remains after uninstall: %s", dir)
		}
	}
	if err := runUninstall(root, executing, targets, &out, ops); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallFailurePreservesRetry(t *testing.T) {
	root, executing, targets := prepareInstallation(t)
	var out bytes.Buffer
	ops := uninstallOps{removePath: func(string) error { return fmt.Errorf("denied PATH") }, unregister: func() error { t.Fatal("unregistered after PATH failure"); return nil }}
	if err := runUninstall(root, executing, targets, &out, ops); err == nil {
		t.Fatal("hidden PATH failure")
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "ceo-plugin-uninstall.exe")); err != nil {
		t.Fatal("lost retry entrypoint")
	}
	if strings.Contains(out.String(), "complete:") {
		t.Fatal("false success")
	}
}

func TestUninstallRejectsChangedGeneration(t *testing.T) {
	root, executing, targets := prepareInstallation(t)
	self := filepath.Join(root, "bin", "ceo-plugin-uninstall.exe")
	payload, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	info, err := os.Stat(self)
	if err != nil {
		t.Fatal(err)
	}
	ops := uninstallOps{expectedHash: hex.EncodeToString(hash[:]), expectedStamp: strconv.FormatInt(info.ModTime().UnixNano()/100+621355968000000000, 10), expectedGeneration: "old"}
	if err := runUninstall(root, executing, targets, io.Discard, ops); err == nil || !strings.Contains(err.Error(), "installation changed") {
		t.Fatalf("missing identity guard: %v", err)
	}
	raw, _ := os.ReadFile(targets[0].Path)
	if !bytes.Contains(raw, []byte("ceo-plugin")) {
		t.Fatal("changed config before identity guard")
	}
}

func TestUnregisterFailureRestoresRetryFiles(t *testing.T) {
	root, executing, targets := prepareInstallation(t)
	before, err := os.ReadFile(filepath.Join(root, "bin", "ceo-plugin-uninstall.exe"))
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := installstate.ReadIdentity(root)
	ops := uninstallOps{removePath: func(string) error { return nil }, unregister: func() error { return fmt.Errorf("fixture registration denied") }}
	if err := runUninstall(root, executing, targets, io.Discard, ops); err == nil {
		t.Fatal("hidden registration error")
	}
	after, err := os.ReadFile(filepath.Join(root, "bin", "ceo-plugin-uninstall.exe"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("retry uninstaller not restored")
	}
	got, _ := installstate.ReadIdentity(root)
	if got != identity {
		t.Fatal("retry identity not restored")
	}
}

func TestBootstrapNativeSuccessAndFailure(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprintf("malformed=%v", malformed), func(t *testing.T) {
			root, _, targets := prepareInstallation(t)
			if malformed {
				if err := os.WriteFile(targets[0].Path, []byte("{broken"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			stage := t.TempDir()
			powershell, err := installstate.PowerShellPath()
			if err != nil {
				t.Fatal(err)
			}
			script := installstate.BootstrapScript(filepath.Join(root, "bin", "ceo-plugin-uninstall.exe"), true, 0, 0, "")
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", installstate.EncodedScript(script))
			cmd.Env = append(os.Environ(), "CEO_TEST_BOOTSTRAP_ROOT="+root, "TEMP="+stage, "TMP="+stage)
			output, err := cmd.CombinedOutput()
			t.Logf("native bootstrap: %s", output)
			if (err != nil) != malformed {
				t.Fatalf("unexpected exit %v: %s", err, output)
			}
			entries, readErr := os.ReadDir(stage)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("temporary worker not cleaned: %v %v", entries, readErr)
			}
			_, statErr := os.Stat(filepath.Join(root, "bin", "ceo-plugin-uninstall.exe"))
			if malformed && statErr != nil {
				t.Fatal("failed cleanup lost uninstaller")
			}
			if !malformed && !os.IsNotExist(statErr) {
				t.Fatal("installed uninstaller remains")
			}
		})
	}
}
