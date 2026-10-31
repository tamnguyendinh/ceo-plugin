package mcpserver

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"ceo-plugin/internal/aicontext"
	"ceo-plugin/internal/version"
	"ceo-plugin/internal/virtualfs"
)

// Server đại diện cho MCP Stdio JSON-RPC Server
type Server struct {
	tree           *virtualfs.VirtualAssetTree
	mu             sync.Mutex // Đảm bảo đồng bộ hóa khi ghi phản hồi ra STDOUT
	Sync           func() aicontext.SyncResult
	Diagnostics    io.Writer
	SyncWorkspace  func(string) aicontext.SyncResult
	rootsSupported bool
	rootsRequest   string
	rootsSequence  uint64
	rootsDeadline  time.Time
	workspace      aicontext.Workspace
}

// NewServer khởi tạo instance mới của MCP Server gắn với VirtualAssetTree
func NewServer(tree *virtualfs.VirtualAssetTree) *Server {
	return &Server{
		tree: tree,
	}
}

// Serve khởi chạy vòng lặp xử lý giao thức MCP trên luồng I/O được tiêm vào.
// Khi luồng 'in' nhận io.EOF, hàm trả về nil để tiến trình thoát sạch sẽ (Exit Code 0).
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := strings.TrimSpace(string(line))
			if len(trimmed) > 0 {
				if consumed, err := s.handleRootsMessage([]byte(trimmed), out); consumed {
					if err != nil {
						return err
					}
					continue
				}
				msg, id, code, decodeErr := decodeMessage([]byte(trimmed))
				if decodeErr != nil {
					if err := protocolError(out, id, code, decodeErr); err != nil {
						return err
					}
					continue
				}
				if _, request := msg["id"]; !request {
					if msg["method"] == "notifications/initialized" || msg["method"] == "notifications/roots/list_changed" {
						if err := s.requestRoots(out); err != nil {
							return err
						}
					}
					continue
				}
				if err := validateParams(msg); err != nil {
					if err := protocolError(out, id, -32602, err); err != nil {
						return err
					}
					continue
				}
				if err := s.handleMessage(msg, out); err != nil {
					return err
				}
			}
		}

		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// Run là hàm tiện ích khởi chạy server mặc định với os.Stdin và os.Stdout
func Run(tree *virtualfs.VirtualAssetTree) error {
	s := NewServer(tree)
	s.Diagnostics = os.Stderr
	return s.Serve(os.Stdin, os.Stdout)
}

// handleMessage xử lý từng bản tin JSON-RPC và ghi phản hồi ra out một cách thread-safe
func (s *Server) handleMessage(rawMap map[string]interface{}, out io.Writer) error {
	method, _ := rawMap["method"].(string)
	id := rawMap["id"]

	// Với thông báo JSON-RPC (notification không có id), tuyệt đối không gửi phản hồi
	if id == nil {
		if method == "notifications/initialized" {
			// MCP initialized notification: ghi nhận và tiếp tục
			return nil
		}
		return nil
	}

	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
	}

	switch method {
	case "initialize":
		params := rawMap["params"].(map[string]interface{})
		caps := params["capabilities"].(map[string]interface{})
		_, s.rootsSupported = caps["roots"].(map[string]interface{})
		resp["result"] = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"resources": map[string]bool{
					"subscribe":   false,
					"listChanged": false,
				},
				"tools": map[string]bool{
					"listChanged": false,
				},
				"prompts": map[string]bool{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]string{
				"name":    "ceo-plugin",
				"version": version.Value,
			},
		}

	case "ping":
		resp["result"] = map[string]interface{}{}

	case "resources/list":
		if s.tree == nil {
			resp["error"] = map[string]interface{}{
				"code":    -32603,
				"message": "VirtualAssetTree is nil",
			}
		} else {
			entries := s.tree.ListEntries()
			resources := make([]map[string]interface{}, 0, len(entries))
			for _, entry := range entries {
				resName := strings.TrimPrefix(entry.URI, "ram://")
				desc := fmt.Sprintf("%s (%s for %s)", entry.Name, entry.Kind, entry.Role)
				resources = append(resources, map[string]interface{}{
					"uri":         entry.URI,
					"name":        resName,
					"description": desc,
					"mimeType":    "text/markdown",
				})
			}
			resp["result"] = map[string]interface{}{
				"resources": resources,
			}
		}

	case "resources/read":
		params, _ := rawMap["params"].(map[string]interface{})
		uri, _ := params["uri"].(string)
		if s.tree == nil {
			resp["error"] = map[string]interface{}{
				"code":    -32603,
				"message": "VirtualAssetTree is nil",
			}
		} else {
			entry, err := s.tree.FindByURI(uri)
			if err != nil {
				resp["error"] = map[string]interface{}{
					"code":    -32002,
					"message": fmt.Sprintf("Resource not found: %s", uri),
				}
			} else {
				resp["result"] = map[string]interface{}{
					"contents": []map[string]interface{}{
						{
							"uri":      entry.URI,
							"mimeType": "text/markdown",
							"text":     entry.Content,
						},
					},
				}
			}
		}

	case "tools/list":
		resp["result"] = map[string]interface{}{
			"tools": []map[string]interface{}{
				{
					"name":        "get_lane_skill",
					"description": "Retrieve the core operating principles (SKILL.md) for a specific CEO plugin lane.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"lane": map[string]interface{}{
								"type":        "string",
								"description": "Lane/role name: ceo, system-architect, architect, coder, qa, supervisor, mechanical-planner, strategic-planner",
							},
						},
						"required": []string{"lane"},
					},
				},
				{
					"name":        "get_lane_reference",
					"description": "Retrieve a specific reference rule, guideline, or template for a role lane.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"lane": map[string]interface{}{
								"type":        "string",
								"description": "Lane/role name: ceo, system-architect, architect, coder, qa, supervisor, mechanical-planner, strategic-planner",
							},
							"reference": map[string]interface{}{
								"type":        "string",
								"description": "Specific reference or template name (e.g. timer-rules, mode-1-spec-authoring-and-architecture, plan-template)",
							},
						},
						"required": []string{"lane", "reference"},
					},
				},
				{
					"name":        "ceo_plugin",
					"description": "Required first step before using CEO Plugin: call ceo_plugin with arguments {} and read the entire result before confirming readiness. Returns only verbatim instructions.md, the initial operating guide; does not load or invoke CEO SKILL.",
					"inputSchema": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
					},
				},
				{
					"name":        "ceo_command",
					"description": "Read documentation using <lane> [reference]. Returns one file; does not execute the described workflow.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"command": map[string]interface{}{
								"type":        "string",
								"description": "Subcommand to execute: ceo, system-architect, architect, coder, qa, supervisor, mechanical-planner, strategic-planner",
							},
						},
						"required": []string{"command"},
					},
				},
			},
		}

	case "tools/call":
		if s.tree == nil {
			return protocolError(out, id, -32603, fmt.Errorf("VirtualAssetTree is nil"))
		}
		params, _ := rawMap["params"].(map[string]interface{})
		toolName, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]interface{})
		if args == nil {
			args = make(map[string]interface{})
		}

		var text string
		var err error

		if s.tree == nil {
			err = fmt.Errorf("VirtualAssetTree is nil")
		} else {
			switch toolName {
			case "get_lane_skill":
				lane, _ := args["lane"].(string)
				if lane == "" {
					lane, _ = args["role"].(string)
				}
				var entry *virtualfs.AssetEntry
				entry, err = s.tree.GetSkill(lane)
				if err == nil {
					text = entry.Content
				}
			case "get_lane_reference":
				lane, _ := args["lane"].(string)
				if lane == "" {
					lane, _ = args["role"].(string)
				}
				ref, _ := args["reference"].(string)
				if ref == "" {
					ref, _ = args["subcommand"].(string)
				}
				var entry *virtualfs.AssetEntry
				entry, err = s.tree.GetReference(lane, ref)
				if err == nil {
					text = entry.Content
				}
			case "ceo_plugin", "anvien_ceo_plugin":
				text = string(aicontext.EmbeddedInstructionsContent())
			case "ceo_command", "anvien_command":
				cmdArg, _ := args["command"].(string)
				parts := strings.Fields(cmdArg)
				if len(parts) > 2 {
					err = fmt.Errorf("extra command arguments")
				} else if len(parts) == 2 {
					var entry *virtualfs.AssetEntry
					entry, err = s.tree.GetReference(parts[0], parts[1])
					if err == nil {
						text = entry.Content
					}
				} else if len(parts) == 1 {
					var entry *virtualfs.AssetEntry
					entry, err = s.tree.GetSkill(parts[0])
					if err == nil {
						text = entry.Content
					}
				} else {
					err = fmt.Errorf("lệnh rỗng")
				}
			default:
				var entry *virtualfs.AssetEntry
				entry, err = s.tree.GetSkill(toolName)
				if err == nil {
					text = entry.Content
				}
			}
		}

		if err != nil {
			resp["error"] = map[string]interface{}{
				"code":    -32602,
				"message": fmt.Sprintf("Lỗi thực thi tool: %v", err),
			}
		} else {
			syncResult := s.syncContext()
			resp["result"] = map[string]interface{}{
				"_meta": map[string]interface{}{"ceo-plugin/context-sync": syncResult},
				"content": []map[string]string{
					{
						"type": "text",
						"text": text,
					},
				},
			}
		}

	case "prompts/list":
		prompts := make([]map[string]string, 0, len(virtualfs.PrimaryRoles))
		for _, role := range virtualfs.PrimaryRoles {
			prompts = append(prompts, map[string]string{"name": role, "description": "Load the " + role + " SKILL instructions; does not create an agent."})
		}
		resp["result"] = map[string]interface{}{"prompts": prompts}

	case "prompts/get":
		params, _ := rawMap["params"].(map[string]interface{})
		name, _ := params["name"].(string)

		if s.tree == nil {
			resp["error"] = map[string]interface{}{
				"code":    -32603,
				"message": "VirtualAssetTree is nil",
			}
		} else {
			entry, err := s.tree.GetSkill(name)
			if err != nil {
				resp["error"] = map[string]interface{}{
					"code":    -32602,
					"message": fmt.Sprintf("Lỗi nạp prompt: %v", err),
				}
			} else {
				syncResult := s.syncContext()
				resp["result"] = map[string]interface{}{
					"_meta": map[string]interface{}{"ceo-plugin/context-sync": syncResult},
					"messages": []map[string]interface{}{
						{
							"role": "user",
							"content": map[string]string{
								"type": "text",
								"text": entry.Content,
							},
						},
					},
				}
			}
		}

	default:
		resp["error"] = map[string]interface{}{
			"code":    -32601,
			"message": "Method not found",
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return writePacket(out, resp)
}

func (s *Server) syncContext() aicontext.SyncResult {
	s.expireRootsRequest()
	r := aicontext.SyncResult{Status: "skipped", Reason: "workspace_unresolved"}
	if s.Sync != nil {
		r = s.Sync()
	} else if s.SyncWorkspace != nil && s.workspace.Root != "" {
		r = s.SyncWorkspace(s.workspace.Root)
	} else if s.workspace.Reason != "" {
		r.Reason = s.workspace.Reason
	}
	r.Warn(s.Diagnostics)
	return r
}
