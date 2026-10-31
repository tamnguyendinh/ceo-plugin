package patcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"ceo-plugin/internal/fileupdate"
)

type ConfigResult struct {
	IDE    string `json:"ide"`
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
type Target struct {
	IDE, Path string
	TOML      bool
}

func updateConfig(path, binary string, isTOML, remove bool) (fileupdate.Result, error) {
	if remove {
		if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
			return fileupdate.Result{Path: path, Status: "unchanged"}, nil
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fileupdate.Result{Path: path, Status: "failed"}, err
	}
	return fileupdate.Update(context.Background(), path, func(raw []byte, exists bool) ([]byte, error) {
		if isTOML {
			return TransformTOML(raw, exists, binary, remove)
		}
		return TransformJSON(raw, exists, binary, remove)
	})
}

func GlobalTargets() ([]Target, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	appData := os.Getenv("APPDATA")
	targets := []Target{{"Antigravity", filepath.Join(home, ".gemini", "config", "mcp_config.json"), false}, {"Codex", filepath.Join(home, ".codex", "config.toml"), true}, {"Cursor", filepath.Join(home, ".cursor", "mcp.json"), false}}
	if appData != "" {
		targets = append(targets, Target{"Claude", filepath.Join(appData, "Claude", "claude_desktop_config.json"), false})
	} else {
		targets = append(targets, Target{IDE: "Claude"})
	}
	return targets, nil
}

// Preflight validates all known targets without creating directories or writing files.
func Preflight(targets []Target, binary string, remove bool) error {
	var errs []error
	for _, target := range targets {
		if target.Path == "" {
			errs = append(errs, fmt.Errorf("%s: unresolved config path", target.IDE))
			continue
		}
		raw, err := os.ReadFile(target.Path)
		exists := err == nil
		if os.IsNotExist(err) {
			err = nil
		}
		if err == nil {
			if target.TOML {
				_, err = TransformTOML(raw, exists, binary, remove)
			} else {
				_, err = TransformJSON(raw, exists, binary, remove)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", target.IDE, target.Path, err))
		}
	}
	return errors.Join(errs...)
}

func Apply(targets []Target, binary string, remove bool) ([]ConfigResult, error) {
	var results []ConfigResult
	var errs []error
	for _, target := range targets {
		r := ConfigResult{IDE: target.IDE, Path: target.Path}
		if target.Path == "" {
			r.Status = "skipped"
			r.Error = "unresolved config path"
			errs = append(errs, fmt.Errorf("%s: %s", r.IDE, r.Error))
		} else {
			v, err := updateConfig(target.Path, binary, target.TOML, remove)
			r.Status = v.Status
			if err != nil {
				r.Error = err.Error()
				errs = append(errs, fmt.Errorf("%s: %w", r.IDE, err))
			}
		}
		results = append(results, r)
	}
	return results, errors.Join(errs...)
}

func PatchGlobalConfig(binary string) ([]ConfigResult, error) {
	targets, err := GlobalTargets()
	if err != nil {
		return nil, err
	}
	return Apply(targets, binary, false)
}
func UnpatchGlobalConfig() ([]ConfigResult, error) {
	targets, err := GlobalTargets()
	if err != nil {
		return nil, err
	}
	return Apply(targets, "", true)
}
