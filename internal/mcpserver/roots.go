package mcpserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"ceo-plugin/internal/aicontext"
)

func (s *Server) requestRoots(out io.Writer) error {
	s.workspace = aicontext.Workspace{Reason: "workspace_unresolved"}
	if !s.rootsSupported {
		return nil
	}
	s.rootsSequence++
	s.rootsRequest = fmt.Sprintf("ceo-plugin/roots/%d", s.rootsSequence)
	s.rootsDeadline = time.Now().Add(5 * time.Second)
	return writePacket(out, map[string]interface{}{"jsonrpc": "2.0", "id": s.rootsRequest, "method": "roots/list"})
}

// Responses to server-initiated requests are not client requests: never send
// an error response back to a response. A changed list immediately invalidates binding.
func (s *Server) handleRootsMessage(raw []byte, out io.Writer) (bool, error) {
	s.expireRootsRequest()
	var msg map[string]json.RawMessage
	if json.Unmarshal(raw, &msg) != nil || msg == nil {
		return false, nil
	}
	if _, hasMethod := msg["method"]; hasMethod {
		return false, nil
	}
	_, result := msg["result"]
	_, failure := msg["error"]
	if !result && !failure {
		return false, nil
	}
	var id, protocol string
	_ = json.Unmarshal(msg["id"], &id)
	_ = json.Unmarshal(msg["jsonrpc"], &protocol)
	if id == "" || id != s.rootsRequest || protocol != "2.0" {
		return true, nil
	}
	s.rootsRequest = ""
	s.workspace = aicontext.Workspace{Reason: "workspace_unresolved"}
	if failure {
		return true, nil
	}
	var body struct {
		Roots []struct {
			URI string `json:"uri"`
		} `json:"roots"`
	}
	if err := json.Unmarshal(msg["result"], &body); err != nil {
		s.workspace.Reason = "invalid_roots_response"
		return true, nil
	}
	unique := map[string]string{}
	for _, root := range body.Roots {
		u, err := url.Parse(root.URI)
		if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || u.RawQuery != "" || u.Fragment != "" {
			s.workspace.Reason = "invalid_workspace_uri"
			return true, nil
		}
		path := strings.TrimPrefix(u.Path, "/")
		if len(path) < 3 || path[1] != ':' || path[2] != '/' {
			s.workspace.Reason = "invalid_workspace_uri"
			return true, nil
		}
		ws := aicontext.ResolveWorkspace("", filepath.FromSlash(path), "mcp")
		if ws.Root == "" {
			s.workspace = ws
			return true, nil
		}
		unique[strings.ToLower(ws.Root)] = ws.Root
	}
	if len(unique) > 1 {
		s.workspace.Reason = "workspace_ambiguous"
		return true, nil
	}
	for _, root := range unique {
		s.workspace = aicontext.Workspace{Root: root}
		// Both roots binding and runtime tool/prompt calls may create context
		// files, but only after a valid workspace has been resolved.
		if s.SyncWorkspace != nil {
			r := s.SyncWorkspace(root)
			r.Warn(s.Diagnostics)
		}
	}
	return true, nil
}

func (s *Server) expireRootsRequest() {
	if s.rootsRequest != "" && time.Now().After(s.rootsDeadline) {
		s.rootsRequest = ""
		s.workspace = aicontext.Workspace{Reason: "workspace_roots_timeout"}
	}
}
