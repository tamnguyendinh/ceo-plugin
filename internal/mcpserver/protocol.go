package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var integerID = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

func decodeMessage(line []byte) (map[string]interface{}, interface{}, int, error) {
	var msg map[string]interface{}
	d := json.NewDecoder(bytes.NewReader(line))
	d.UseNumber()
	if !json.Valid(line) {
		return nil, nil, -32700, fmt.Errorf("Parse error")
	}
	if err := d.Decode(&msg); err != nil || msg == nil {
		return nil, nil, -32600, fmt.Errorf("Invalid Request")
	}
	id, present := msg["id"]
	if present {
		switch v := id.(type) {
		case string:
		case json.Number:
			if !integerID.MatchString(string(v)) {
				return nil, nil, -32600, fmt.Errorf("Invalid request ID")
			}
		default:
			return nil, nil, -32600, fmt.Errorf("Invalid request ID")
		}
	}
	method, ok := msg["method"].(string)
	if msg["jsonrpc"] != "2.0" || !ok || method == "" {
		return nil, id, -32600, fmt.Errorf("Invalid Request")
	}
	return msg, id, 0, nil
}

func validateParams(msg map[string]interface{}) error {
	method := msg["method"].(string)
	p, exists := msg["params"]
	params, object := p.(map[string]interface{})
	if exists && !object {
		return fmt.Errorf("params must be an object")
	}
	required := map[string][]string{
		"resources/read": {"uri"}, "tools/call": {"name"}, "prompts/get": {"name"},
	}
	for _, key := range required[method] {
		if v, ok := params[key].(string); !ok || v == "" {
			return fmt.Errorf("%s must be a nonempty string", key)
		}
	}
	if method == "initialize" {
		if v, ok := params["protocolVersion"].(string); !ok || v == "" {
			return fmt.Errorf("protocolVersion is required")
		}
		if _, ok := params["capabilities"].(map[string]interface{}); !ok {
			return fmt.Errorf("capabilities must be an object")
		}
		client, ok := params["clientInfo"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("clientInfo must be an object")
		}
		for _, key := range []string{"name", "version"} {
			if _, ok := client[key].(string); !ok {
				return fmt.Errorf("clientInfo.%s is required", key)
			}
		}
	}
	if method == "tools/call" || method == "prompts/get" {
		if args, exists := params["arguments"]; exists {
			if _, ok := args.(map[string]interface{}); !ok {
				return fmt.Errorf("arguments must be an object")
			}
		}
	}
	if method == "tools/call" {
		args, _ := params["arguments"].(map[string]interface{})
		for _, key := range []string{"lane", "role", "reference", "subcommand", "command"} {
			if value, exists := args[key]; exists {
				if s, ok := value.(string); !ok || strings.TrimSpace(s) == "" {
					return fmt.Errorf("%s must be a nonempty string", key)
				}
			}
		}
		for _, pair := range [][2]string{{"lane", "role"}, {"reference", "subcommand"}} {
			a, aok := args[pair[0]].(string)
			b, bok := args[pair[1]].(string)
			if aok && bok && !strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) {
				return fmt.Errorf("conflicting %s and %s", pair[0], pair[1])
			}
		}
	}
	return nil
}

func writePacket(out io.Writer, packet interface{}) error {
	b, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	n, err := out.Write(b)
	if err == nil && n != len(b) {
		return io.ErrShortWrite
	}
	return err
}

func protocolError(out io.Writer, id interface{}, code int, err error) error {
	return writePacket(out, map[string]interface{}{"jsonrpc": "2.0", "id": id, "error": map[string]interface{}{"code": code, "message": err.Error()}})
}
