package patcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
)

const currentKey = "ceo-plugin"
const legacyKey = "anvien-ceo-plugin"

// object rejects duplicate keys in the object being edited. Unrelated values
// remain RawMessages, avoiding float64 round trips and schema assumptions.
func object(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("expected JSON object")
	}
	m := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, err
		}
		k, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("invalid object key")
		}
		if _, exists := m[k]; exists {
			return nil, fmt.Errorf("duplicate key %q", k)
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, err
		}
		m[k] = value
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return m, nil
}

func equalJSON(a, b []byte) bool {
	decode := func(raw []byte) interface{} {
		var v interface{}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if d.Decode(&v) != nil {
			return nil
		}
		return v
	}
	return reflect.DeepEqual(decode(a), decode(b))
}

func TransformJSON(raw []byte, exists bool, binary string, remove bool) ([]byte, error) {
	if !exists && remove {
		return nil, nil
	}
	bom := bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	input := raw
	if bom {
		input = input[3:]
	}
	root := map[string]json.RawMessage{}
	var err error
	if exists {
		root, err = object(input)
		if err != nil {
			return nil, err
		}
	}
	servers := map[string]json.RawMessage{}
	if v, ok := root["mcpServers"]; ok {
		servers, err = object(v)
		if err != nil {
			return nil, fmt.Errorf("mcpServers: %w", err)
		}
	}
	entry := map[string]json.RawMessage{}
	for _, key := range []string{currentKey, legacyKey} {
		if v, ok := servers[key]; ok {
			fields, err := object(v)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			for k, val := range fields {
				if k == "command" || k == "args" {
					continue
				}
				if prior, ok := entry[k]; !remove && ok && !equalJSON(prior, val) {
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
		entry["command"], err = json.Marshal(filepath.Clean(binary))
		if err != nil {
			return nil, err
		}
		entry["args"] = json.RawMessage(`["mcp"]`)
	}
	delete(servers, currentKey)
	delete(servers, legacyKey)
	if !remove {
		servers[currentKey], err = json.Marshal(entry)
		if err != nil {
			return nil, err
		}
	}
	root["mcpServers"], err = json.Marshal(servers)
	if err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("invalid encoded JSON")
	}
	if exists && equalJSON(input, out) {
		return raw, nil
	}
	if bom {
		out = append([]byte{0xef, 0xbb, 0xbf}, out...)
	}
	return out, nil
}

func PatchJSONConfig(path, binary string) error {
	_, err := updateConfig(path, binary, false, false)
	return err
}
func UnpatchJSONConfig(path string) error { _, err := updateConfig(path, "", false, true); return err }
