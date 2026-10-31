package aicontext

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Workspace struct {
	Root   string `json:"root,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ResolveWorkspace never treats global AGENTS.md as proof of workspace ownership.
// MCP needs a host-provided root; a process cwd alone is not such a binding.
func ResolveWorkspace(cwd, explicitRoot, mode string) Workspace {
	if explicitRoot != "" {
		return validateRoot(explicitRoot)
	}
	if mode == "mcp" {
		return Workspace{Reason: "workspace_unresolved"}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return Workspace{Reason: err.Error()}
	}
	for dir := abs; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return validateRoot(dir)
		} else if !os.IsNotExist(err) {
			return Workspace{Reason: err.Error()}
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return validateRoot(abs)
}

func validateRoot(root string) Workspace {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Workspace{Reason: err.Error()}
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return Workspace{Reason: err.Error()}
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Workspace{Reason: fmt.Sprintf("invalid workspace directory: %s", root)}
	}
	home, _ := os.UserHomeDir()
	for _, denied := range []string{home, os.Getenv("WINDIR"), filepath.Join(os.Getenv("WINDIR"), "System32"), filepath.Join(home, ".ceo-plugin"), filepath.Join(home, ".ceo-plugin", "bin")} {
		if denied != "" && strings.EqualFold(filepath.Clean(abs), filepath.Clean(denied)) {
			return Workspace{Reason: "unsafe_workspace_root"}
		}
	}
	if filepath.Dir(abs) == abs {
		return Workspace{Reason: "unsafe_workspace_root"}
	}
	return Workspace{Root: abs}
}

func findWorkspaceRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return ResolveWorkspace(cwd, "", "cli").Root
}
