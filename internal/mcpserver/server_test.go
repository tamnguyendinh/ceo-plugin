package mcpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ceo-plugin/internal/aicontext"
	"ceo-plugin/internal/virtualfs"
)

func TestRootsBindingChangeAndTimeout(t *testing.T) {
	s := NewServer(initTestTree(t))
	var out bytes.Buffer
	root := t.TempDir()
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(root)}
	s.rootsSupported = true
	if err := s.requestRoots(&out); err != nil {
		t.Fatal(err)
	}
	reply, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": s.rootsRequest, "result": map[string]interface{}{"roots": []map[string]string{{"uri": u.String()}}}})
	if handled, err := s.handleRootsMessage(reply, &out); !handled || err != nil || s.workspace.Root == "" {
		t.Fatalf("root not bound: %+v %v", s.workspace, err)
	}
	bound := s.workspace.Root
	called := ""
	s.SyncWorkspace = func(p string) aicontext.SyncResult { called = p; return aicontext.SyncResult{Status: "unchanged"} }
	s.syncContext()
	if called != bound {
		t.Fatal("sync not bound to roots")
	}
	if err := s.requestRoots(&out); err != nil {
		t.Fatal(err)
	}
	if s.workspace.Root != "" {
		t.Fatal("stale root survives change")
	}
	u2 := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(t.TempDir())}
	reply, _ = json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": s.rootsRequest, "result": map[string]interface{}{"roots": []map[string]string{{"uri": u.String()}, {"uri": u2.String()}}}})
	_, _ = s.handleRootsMessage(reply, &out)
	if s.workspace.Root != "" || s.workspace.Reason != "workspace_ambiguous" {
		t.Fatal("selected first of multiple roots")
	}
	if err := s.requestRoots(&out); err != nil {
		t.Fatal(err)
	}
	oldID := s.rootsRequest
	s.rootsDeadline = time.Now().Add(-time.Second)
	r := s.syncContext()
	if r.Reason != "workspace_roots_timeout" {
		t.Fatalf("timeout: %+v", r)
	}
	reply, _ = json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": oldID, "result": map[string]interface{}{"roots": []map[string]string{{"uri": u.String()}}}})
	_, _ = s.handleRootsMessage(reply, &out)
	if s.workspace.Root != "" {
		t.Fatal("late root response revived stale binding")
	}
}

func TestRootsAutoSyncOnRootsMessage(t *testing.T) {
	s := NewServer(initTestTree(t))
	var out bytes.Buffer
	root := t.TempDir()
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(root)}
	s.rootsSupported = true

	called := ""
	s.SyncWorkspace = func(p string) aicontext.SyncResult {
		called = p
		return aicontext.SyncResult{Status: "updated"}
	}

	if err := s.requestRoots(&out); err != nil {
		t.Fatal(err)
	}

	reply, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      s.rootsRequest,
		"result": map[string]interface{}{
			"roots": []map[string]string{{"uri": u.String()}},
		},
	})

	handled, err := s.handleRootsMessage(reply, &out)
	if !handled || err != nil {
		t.Fatalf("handleRootsMessage failed: handled=%v, err=%v", handled, err)
	}

	if s.workspace.Root == "" {
		t.Fatal("workspace root was not bound")
	}

	if called != s.workspace.Root {
		t.Fatalf("expected s.SyncWorkspace to be automatically called with %q, but got %q", s.workspace.Root, called)
	}
}

type failedWriter struct{ short bool }

func (w failedWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, fmt.Errorf("output unavailable")
}

func TestProtocolValidationAndExactID(t *testing.T) {
	s := NewServer(initTestTree(t))
	syncCalls := 0
	s.Sync = func() aicontext.SyncResult { syncCalls++; return aicontext.SyncResult{Status: "updated"} }
	for _, tc := range []struct {
		input string
		code  int
	}{
		{`{bad`, -32700}, {`[]`, -32600}, {`{"jsonrpc":"1.0","id":1,"method":"ping"}`, -32600},
		{`{"jsonrpc":"2.0","id":1}`, -32600}, {`{"jsonrpc":"2.0","id":null,"method":"ping"}`, -32600},
		{`{"jsonrpc":"2.0","id":1,"method":"resources/read"}`, -32602},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_lane_skill","arguments":{"lane":123,"role":"ceo"}}}`, -32602},
	} {
		r, err := sendRequest(s, tc.input)
		if err != nil {
			t.Fatal(err)
		}
		if e, ok := r["error"].(map[string]interface{}); !ok || int(e["code"].(float64)) != tc.code {
			t.Fatalf("%s -> %v", tc.input, r)
		}
	}
	if syncCalls != 0 {
		t.Fatal("invalid requests triggered sync")
	}
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(`{"jsonrpc":"2.0","id":9007199254740993,"method":"ping"}`+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id":9007199254740993`) {
		t.Fatalf("ID rounded: %s", out.String())
	}
	for _, writer := range []failedWriter{{}, {short: true}} {
		if err := s.Serve(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"), writer); err == nil {
			t.Fatal("writer error hidden")
		}
	}
}

func TestSyncFailureKeepsAssetPayload(t *testing.T) {
	s := NewServer(initTestTree(t))
	var diagnostic bytes.Buffer
	s.Diagnostics = &diagnostic
	s.Sync = func() aicontext.SyncResult { return aicontext.SyncResult{Status: "failed", Reason: "fixture denied"} }
	r, err := sendRequest(s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_lane_skill","arguments":{"lane":"ceo"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	result := r["result"].(map[string]interface{})
	content := result["content"].([]interface{})[0].(map[string]interface{})["text"]
	entry, _ := s.tree.GetSkill("ceo")
	if content != entry.Content || diagnostic.Len() == 0 || result["_meta"] == nil {
		t.Fatal("sync failure lost payload/diagnostic")
	}
}

func findAssetsDir(t testing.TB) string {
	t.Helper()
	candidates := []string{
		"../../cmd/engine/assets",
		"cmd/engine/assets",
		"assets",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	t.Fatalf("could not locate assets directory from candidates: %v", candidates)
	return ""
}

func initTestTree(t testing.TB) *virtualfs.VirtualAssetTree {
	t.Helper()
	assetsDir := findAssetsDir(t)
	fsys := os.DirFS(assetsDir)
	tree, err := virtualfs.NewTreeFromFS(fsys)
	if err != nil {
		t.Fatalf("failed to initialize VirtualAssetTree from FS: %v", err)
	}
	return tree
}

func computeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func sendRequest(server *Server, req string) (map[string]interface{}, error) {
	in := strings.NewReader(req + "\n")
	var out bytes.Buffer
	if err := server.Serve(in, &out); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(out.String())
	if trimmed == "" {
		return nil, nil
	}
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w (raw: %s)", err, trimmed)
	}
	return resp, nil
}

// TestMCPServer_Initialize kiểm tra handshake khởi tạo protocol 2024-11-05 và capabilities
func TestMCPServer_Initialize(t *testing.T) {
	tree := initTestTree(t)
	server := NewServer(tree)

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0.0"}}}`
	resp, err := sendRequest(server, req)
	if err != nil {
		t.Fatalf("sendRequest failed: %v", err)
	}

	if resp["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc=2.0, got %v", resp["jsonrpc"])
	}
	if resp["id"] != float64(1) {
		t.Errorf("expected id=1, got %v", resp["id"])
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result object, got %v", resp["result"])
	}

	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocolVersion=2024-11-05, got %v", result["protocolVersion"])
	}

	caps, ok := result["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected capabilities object, got %v", result["capabilities"])
	}
	if _, hasResources := caps["resources"]; !hasResources {
		t.Errorf("missing resources capability")
	}
	if _, hasTools := caps["tools"]; !hasTools {
		t.Errorf("missing tools capability")
	}
	if _, hasPrompts := caps["prompts"]; !hasPrompts {
		t.Errorf("missing prompts capability")
	}

	serverInfo, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected serverInfo object, got %v", result["serverInfo"])
	}
	if serverInfo["name"] != "ceo-plugin" {
		t.Errorf("expected server name ceo-plugin, got %v", serverInfo["name"])
	}
}

// TestMCPServer_NotificationsInitialized kiểm tra notification không có phản hồi và không gây lỗi
func TestMCPServer_NotificationsInitialized(t *testing.T) {
	tree := initTestTree(t)
	server := NewServer(tree)

	req := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	resp, err := sendRequest(server, req)
	if err != nil {
		t.Fatalf("sendRequest failed: %v", err)
	}
	if resp != nil {
		t.Errorf("expected no response for notifications/initialized, got %v", resp)
	}
}

// TestMCPServer_Ping kiểm tra lệnh ping trả về kết quả rỗng {}
func TestMCPServer_Ping(t *testing.T) {
	tree := initTestTree(t)
	server := NewServer(tree)

	req := `{"jsonrpc":"2.0","id":"ping-1","method":"ping"}`
	resp, err := sendRequest(server, req)
	if err != nil {
		t.Fatalf("sendRequest failed: %v", err)
	}

	if resp["id"] != "ping-1" {
		t.Errorf("expected id=ping-1, got %v", resp["id"])
	}
	result, ok := resp["result"].(map[string]interface{})
	if !ok || len(result) != 0 {
		t.Errorf("expected empty result map, got %v", resp["result"])
	}
}

// TestMCPServer_ResourcesList xác nhận trả về đầy đủ chính xác 39 tài nguyên RAM ram://...
func TestMCPServer_ResourcesList(t *testing.T) {
	tree := initTestTree(t)
	server := NewServer(tree)

	req := `{"jsonrpc":"2.0","id":100,"method":"resources/list"}`
	resp, err := sendRequest(server, req)
	if err != nil {
		t.Fatalf("sendRequest failed: %v", err)
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result object, got %v", resp["result"])
	}

	rawList, ok := result["resources"].([]interface{})
	if !ok {
		t.Fatalf("expected resources array, got %v", result["resources"])
	}

	if len(rawList) != 39 {
		t.Fatalf("expected exactly 39 resources, got %d", len(rawList))
	}

	for i, r := range rawList {
		item, ok := r.(map[string]interface{})
		if !ok {
			t.Fatalf("resource %d is not a map", i)
		}
		uri, _ := item["uri"].(string)
		if !strings.HasPrefix(uri, "ram://") {
			t.Errorf("resource %d URI does not start with ram://: %s", i, uri)
		}
		mimeType, _ := item["mimeType"].(string)
		if mimeType != "text/markdown" {
			t.Errorf("resource %s has unexpected mimeType: %s", uri, mimeType)
		}
		name, _ := item["name"].(string)
		if name == "" {
			t.Errorf("resource %s has empty name", uri)
		}
		desc, _ := item["description"].(string)
		if desc == "" {
			t.Errorf("resource %s has empty description", uri)
		}
	}
}

// TestMCPServer_ResourcesRead_CanonicalAndAliases kiểm tra đọc tài nguyên khớp 100% SHA-256 bit-by-bit
func TestMCPServer_ResourcesRead_CanonicalAndAliases(t *testing.T) {
	tree := initTestTree(t)
	server := NewServer(tree)
	assetsDir := findAssetsDir(t)

	testCases := []struct {
		name             string
		uri              string
		expectedDiskFile string
		expectedBytes    int
	}{
		{
			name:             "CEO SKILL canonical (13,294 bytes)",
			uri:              "ram://ceo/SKILL.md",
			expectedDiskFile: filepath.Join(assetsDir, "ceo", "SKILL.md"),
			expectedBytes:    13294,
		},
		{
			name:             "System Architect Mode 1 reference (11,762 bytes)",
			uri:              "ram://system-architect/references/mode-1-spec-authoring-and-architecture.md",
			expectedDiskFile: filepath.Join(assetsDir, "system-architect", "references", "mode-1-spec-authoring-and-architecture.md"),
			expectedBytes:    11762,
		},
		{
			name:             "System Architect Mode 0 reference (6,653 bytes)",
			uri:              "ram://system-architect/references/mode-0-interactive-architecture-discussion.md",
			expectedDiskFile: filepath.Join(assetsDir, "system-architect", "references", "mode-0-interactive-architecture-discussion.md"),
			expectedBytes:    6653,
		},
		{
			name:             "Architect alias inherited reference",
			uri:              "ram://architect/references/mode-1-spec-authoring-and-architecture.md",
			expectedDiskFile: filepath.Join(assetsDir, "system-architect", "references", "mode-1-spec-authoring-and-architecture.md"),
			expectedBytes:    11762,
		},
		{
			name:             "Strategic Planner template (15,479 bytes)",
			uri:              "ram://strategic-planner/templates/plan.template.md",
			expectedDiskFile: filepath.Join(assetsDir, "planner", "templates", "plan.template.md"),
			expectedBytes:    15479,
		},
		{
			name:             "Mechanical Planner alias template",
			uri:              "ram://mechanical-planner/templates/plan.template.md",
			expectedDiskFile: filepath.Join(assetsDir, "planner", "templates", "plan.template.md"),
			expectedBytes:    15479,
		},
		{
			name:             "QA backend-qa-protocol reference",
			uri:              "ram://qa/references/backend-qa-protocol.md",
			expectedDiskFile: filepath.Join(assetsDir, "qa", "references", "backend-qa-protocol.md"),
			expectedBytes:    4275,
		},
		{
			name:             "Supervisor evidence-and-source-inspection reference",
			uri:              "ram://supervisor/references/evidence-and-source-inspection.md",
			expectedDiskFile: filepath.Join(assetsDir, "supervisor", "references", "evidence-and-source-inspection.md"),
			expectedBytes:    2736,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			diskBytes, err := os.ReadFile(tc.expectedDiskFile)
			if err != nil {
				t.Fatalf("failed to read expected disk file %s: %v", tc.expectedDiskFile, err)
			}
			if tc.expectedBytes > 0 && len(diskBytes) != tc.expectedBytes {
				t.Fatalf("disk file size mismatch: expected %d, got %d", tc.expectedBytes, len(diskBytes))
			}
			expectedHash := computeSHA256(diskBytes)

			req := fmt.Sprintf(`{"jsonrpc":"2.0","id":200,"method":"resources/read","params":{"uri":"%s"}}`, tc.uri)
			resp, err := sendRequest(server, req)
			if err != nil {
				t.Fatalf("sendRequest failed: %v", err)
			}

			if resp["error"] != nil {
				t.Fatalf("unexpected error response for URI %s: %v", tc.uri, resp["error"])
			}

			result, ok := resp["result"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected result object, got %v", resp["result"])
			}

			contents, ok := result["contents"].([]interface{})
			if !ok || len(contents) == 0 {
				t.Fatalf("expected non-empty contents array, got %v", result["contents"])
			}

			firstContent, ok := contents[0].(map[string]interface{})
			if !ok {
				t.Fatalf("content item is not a map")
			}

			if firstContent["mimeType"] != "text/markdown" {
				t.Errorf("expected mimeType=text/markdown, got %v", firstContent["mimeType"])
			}

			textContent, ok := firstContent["text"].(string)
			if !ok {
				t.Fatalf("expected text content string, got %v", firstContent["text"])
			}

			actualHash := computeSHA256([]byte(textContent))
			if actualHash != expectedHash {
				t.Fatalf("SHA-256 mismatch for URI %s:\nexpected: %s\nactual:   %s", tc.uri, expectedHash, actualHash)
			}
		})
	}
}

// TestMCPServer_ResourcesRead_NotFound kiểm tra trả về mã lỗi -32002 khi URI không tồn tại
func TestMCPServer_ResourcesRead_NotFound(t *testing.T) {
	tree := initTestTree(t)
	server := NewServer(tree)

	req := `{"jsonrpc":"2.0","id":300,"method":"resources/read","params":{"uri":"ram://invalid/path/file.md"}}`
	resp, err := sendRequest(server, req)
	if err != nil {
		t.Fatalf("sendRequest failed: %v", err)
	}

	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object, got %v", resp)
	}

	code, _ := errObj["code"].(float64)
	if code != -32002 {
		t.Errorf("expected error code -32002, got %v", code)
	}

	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "Resource not found") {
		t.Errorf("expected message to contain 'Resource not found', got %s", msg)
	}
}

// TestMCPServer_ToolsList kiểm tra danh sách 4 công cụ đăng ký
func TestMCPServer_ToolsList(t *testing.T) {
	tree := initTestTree(t)
	server := NewServer(tree)

	req := `{"jsonrpc":"2.0","id":400,"method":"tools/list"}`
	resp, err := sendRequest(server, req)
	if err != nil {
		t.Fatalf("sendRequest failed: %v", err)
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected result object, got %v", resp["result"])
	}

	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatalf("expected tools array, got %v", result["tools"])
	}

	expectedTools := map[string]bool{
		"get_lane_skill":     false,
		"get_lane_reference": false,
		"ceo_plugin":         false,
		"ceo_command":        false,
	}

	for _, tool := range tools {
		tMap, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := tMap["name"].(string)
		if name == "ceo_plugin" {
			description, _ := tMap["description"].(string)
			for _, required := range []string{"Required first step", "ceo_plugin with arguments {}", "read the entire result before confirming readiness", "only verbatim instructions.md"} {
				if !strings.Contains(description, required) {
					t.Errorf("bootstrap description missing %q", required)
				}
			}
		}
		if _, exists := expectedTools[name]; exists {
			expectedTools[name] = true
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing tool in tools/list: %s", name)
		}
	}
}

// TestMCPServer_ToolsCall kiểm tra gọi các công cụ và trả về nội dung 100% SHA-256 match
func TestMCPServer_ToolsCall(t *testing.T) {
	tree := initTestTree(t)
	server := NewServer(tree)
	assetsDir := findAssetsDir(t)

	// 1. Gọi get_lane_skill cho CEO
	t.Run("get_lane_skill ceo", func(t *testing.T) {
		diskBytes, err := os.ReadFile(filepath.Join(assetsDir, "ceo", "SKILL.md"))
		if err != nil {
			t.Fatalf("failed to read disk file: %v", err)
		}
		expectedHash := computeSHA256(diskBytes)

		req := `{"jsonrpc":"2.0","id":501,"method":"tools/call","params":{"name":"get_lane_skill","arguments":{"lane":"ceo"}}}`
		resp, err := sendRequest(server, req)
		if err != nil {
			t.Fatalf("sendRequest failed: %v", err)
		}

		result, ok := resp["result"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected result object, got %v", resp["result"])
		}
		contents, _ := result["content"].([]interface{})
		if len(contents) == 0 {
			t.Fatalf("empty content in tool result")
		}
		item := contents[0].(map[string]interface{})
		text := item["text"].(string)

		if computeSHA256([]byte(text)) != expectedHash {
			t.Errorf("SHA-256 mismatch for get_lane_skill ceo")
		}
	})

	// 2. Gọi get_lane_reference cho strategic-planner plan-template
	t.Run("get_lane_reference strategic-planner plan-template", func(t *testing.T) {
		diskBytes, err := os.ReadFile(filepath.Join(assetsDir, "planner", "templates", "plan.template.md"))
		if err != nil {
			t.Fatalf("failed to read disk file: %v", err)
		}
		expectedHash := computeSHA256(diskBytes)

		req := `{"jsonrpc":"2.0","id":502,"method":"tools/call","params":{"name":"get_lane_reference","arguments":{"lane":"strategic-planner","reference":"plan-template"}}}`
		resp, err := sendRequest(server, req)
		if err != nil {
			t.Fatalf("sendRequest failed: %v", err)
		}

		result, ok := resp["result"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected result object, got %v", resp["result"])
		}
		contents, _ := result["content"].([]interface{})
		item := contents[0].(map[string]interface{})
		text := item["text"].(string)

		if computeSHA256([]byte(text)) != expectedHash {
			t.Errorf("SHA-256 mismatch for get_lane_reference")
		}
	})

	// 3. Bootstrap returns instructions only, including through its legacy alias.
	for _, name := range []string{"ceo_plugin", "anvien_ceo_plugin"} {
		for _, emptyTree := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s instructions emptyTree=%v", name, emptyTree), func(t *testing.T) {
				bootstrapTree := tree
				if emptyTree {
					// No CEO skill exists: bootstrap must not fetch or depend on it.
					bootstrapTree = virtualfs.NewVirtualAssetTree()
				}
				bootstrap := NewServer(bootstrapTree)
				syncCalls := 0
				bootstrap.Sync = func() aicontext.SyncResult {
					syncCalls++
					return aicontext.SyncResult{Status: "updated"}
				}
				want, err := os.ReadFile(filepath.Join("..", "aicontext", "instructions.md"))
				if err != nil {
					t.Fatal(err)
				}
				req := fmt.Sprintf(`{"jsonrpc":"2.0","id":503,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, name)
				resp, err := sendRequest(bootstrap, req)
				if err != nil {
					t.Fatal(err)
				}
				result, ok := resp["result"].(map[string]interface{})
				if !ok || resp["error"] != nil {
					t.Fatalf("expected successful bootstrap result: %v", resp)
				}
				contents, _ := result["content"].([]interface{})
				if len(contents) != 1 {
					t.Fatalf("expected exactly one text block, got %d", len(contents))
				}
				item := contents[0].(map[string]interface{})
				if item["type"] != "text" || item["text"] != string(want) {
					t.Fatal("bootstrap must return verbatim instructions.md without added content")
				}
				meta := result["_meta"].(map[string]interface{})
				contextSync := meta["ceo-plugin/context-sync"].(map[string]interface{})
				if syncCalls != 1 || contextSync["status"] != "updated" {
					t.Fatalf("context sync changed: calls=%d metadata=%v", syncCalls, contextSync)
				}
			})
		}
	}

	// 4. Gọi ceo_command với subcommand phân cấp
	t.Run("ceo_command hierarchical", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":504,"method":"tools/call","params":{"name":"ceo_command","arguments":{"command":"qa backend-qa-protocol"}}}`
		resp, err := sendRequest(server, req)
		if err != nil {
			t.Fatalf("sendRequest failed: %v", err)
		}
		result, ok := resp["result"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected result object, got %v", resp["result"])
		}
		contents, _ := result["content"].([]interface{})
		item := contents[0].(map[string]interface{})
		text := item["text"].(string)
		if len(text) != 4275 {
			t.Errorf("expected 4275 bytes for qa backend-qa-protocol, got %d", len(text))
		}
	})

	// 5. Gọi tool không hợp lệ trả về lỗi
	t.Run("invalid tool call error", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":505,"method":"tools/call","params":{"name":"get_lane_skill","arguments":{"lane":"unknown-lane"}}}`
		resp, err := sendRequest(server, req)
		if err != nil {
			t.Fatalf("sendRequest failed: %v", err)
		}
		if resp["error"] == nil {
			t.Errorf("expected error for unknown lane tool call, got nil")
		}
	})
}

// TestMCPServer_Prompts kiểm tra prompts/list và prompts/get
func TestMCPServer_Prompts(t *testing.T) {
	tree := initTestTree(t)
	server := NewServer(tree)

	t.Run("prompts/list", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":601,"method":"prompts/list"}`
		resp, err := sendRequest(server, req)
		if err != nil {
			t.Fatalf("sendRequest failed: %v", err)
		}
		result, ok := resp["result"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected result object, got %v", resp["result"])
		}
		prompts, ok := result["prompts"].([]interface{})
		if !ok || len(prompts) != 8 {
			t.Fatalf("expected 8 prompts, got %d", len(prompts))
		}
	})

	t.Run("prompts/get", func(t *testing.T) {
		req := `{"jsonrpc":"2.0","id":602,"method":"prompts/get","params":{"name":"coder"}}`
		resp, err := sendRequest(server, req)
		if err != nil {
			t.Fatalf("sendRequest failed: %v", err)
		}
		result, ok := resp["result"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected result object, got %v", resp["result"])
		}
		messages, ok := result["messages"].([]interface{})
		if !ok || len(messages) == 0 {
			t.Fatalf("expected messages array, got %v", result["messages"])
		}
		firstMsg := messages[0].(map[string]interface{})
		content := firstMsg["content"].(map[string]interface{})
		text := content["text"].(string)
		if len(text) != 16647 {
			t.Errorf("expected 16647 bytes for coder prompt, got %d", len(text))
		}
	})
}

// TestMCPServer_CleanExitOnEOF mô phỏng đóng pipe Stdio và kiểm tra Serve() thoát sạch sẽ với nil < 50ms
func TestMCPServer_CleanExitOnEOF(t *testing.T) {
	tree := initTestTree(t)
	server := NewServer(tree)

	pr, pw := io.Pipe()
	var out bytes.Buffer

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- server.Serve(pr, &out)
	}()

	// Gửi một lệnh ping trước
	_, _ = pw.Write([]byte(`{"jsonrpc":"2.0","id":700,"method":"ping"}` + "\n"))

	// Đóng pipe mô phỏng EOF từ phía client
	_ = pw.Close()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("expected nil error on EOF, got: %v", err)
		}
		if elapsed > 100*time.Millisecond {
			t.Fatalf("expected clean exit under 100ms, took %v", elapsed)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for Serve to cleanly exit on EOF")
	}

	// Xác nhận phản hồi ping đã được ghi ra trước khi thoát
	var resp map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse ping response before EOF: %v", err)
	}
	if resp["id"] != float64(700) {
		t.Errorf("expected response for id 700, got %v", resp["id"])
	}
}

// TestMCPServer_ConcurrencyAndRace gửi nhiều request đồng thời, chạy với cờ -race để kiểm tra an toàn đa luồng
func TestMCPServer_ConcurrencyAndRace(t *testing.T) {
	tree := initTestTree(t)
	server := NewServer(tree)

	const numWorkers = 20
	const requestsPerWorker = 25
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < requestsPerWorker; j++ {
				reqID := workerID*1000 + j
				var req string
				switch j % 4 {
				case 0:
					req = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"resources/read","params":{"uri":"ram://ceo/SKILL.md"}}`, reqID)
				case 1:
					req = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"get_lane_skill","arguments":{"lane":"coder"}}}`, reqID)
				case 2:
					req = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"resources/list"}`, reqID)
				case 3:
					req = fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"ping"}`, reqID)
				}

				resp, err := sendRequest(server, req)
				if err != nil {
					t.Errorf("worker %d req %d failed: %v", workerID, j, err)
					return
				}
				if resp["error"] != nil {
					t.Errorf("worker %d req %d returned error: %v", workerID, j, resp["error"])
				}
			}
		}(i)
	}

	wg.Wait()
}
