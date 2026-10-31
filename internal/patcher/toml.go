package patcher

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/pelletier/go-toml/v2"
)

func TransformTOML(raw []byte, exists bool, binary string, remove bool) ([]byte, error) {
	if !exists && remove {
		return nil, nil
	}
	root := map[string]interface{}{}
	if exists {
		if err := toml.Unmarshal(raw, &root); err != nil {
			return nil, err
		}
	}
	before, err := toml.Marshal(root)
	if err != nil {
		return nil, err
	}
	servers := map[string]interface{}{}
	if v, ok := root["mcp_servers"]; ok {
		var valid bool
		servers, valid = v.(map[string]interface{})
		if !valid {
			return nil, fmt.Errorf("mcp_servers must be a table")
		}
	}
	entry := map[string]interface{}{}
	for _, key := range []string{currentKey, legacyKey} {
		if v, ok := servers[key]; ok {
			fields, ok := v.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("%s must be a table", key)
			}
			for k, val := range fields {
				if k == "command" || k == "args" {
					continue
				}
				if prior, ok := entry[k]; !remove && ok && !reflect.DeepEqual(prior, val) {
					return nil, fmt.Errorf("conflicting plugin field %q", k)
				}
				entry[k] = val
			}
		}
	}
	if remove {
		_, a := servers[currentKey]
		_, b := servers[legacyKey]
		if !a && !b {
			return raw, nil
		}
	} else {
		if _, ok := entry["url"]; ok {
			return nil, fmt.Errorf("plugin url conflicts with stdio command")
		}
		entry["command"] = filepath.Clean(binary)
		entry["args"] = []string{"mcp"}
	}
	delete(servers, currentKey)
	delete(servers, legacyKey)
	if !remove {
		servers[currentKey] = entry
	}
	root["mcp_servers"] = servers
	out, err := toml.Marshal(root)
	if err != nil {
		return nil, err
	}
	var check map[string]interface{}
	if err := toml.Unmarshal(out, &check); err != nil {
		return nil, err
	}
	if bytes.Equal(before, out) {
		return raw, nil
	}
	return out, nil
}

func PatchCodexTOML(path, binary string) error {
	_, err := updateConfig(path, binary, true, false)
	return err
}
func UnpatchCodexTOML(path string) error { _, err := updateConfig(path, "", true, true); return err }
