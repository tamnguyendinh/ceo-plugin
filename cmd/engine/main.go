package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"ceo-plugin/internal/aicontext"
	"ceo-plugin/internal/installstate"
	"ceo-plugin/internal/mcpserver"
	"ceo-plugin/internal/version"
	"ceo-plugin/internal/virtualfs"
)

//go:embed assets/*
var embeddedAssets embed.FS

var (
	treeOnce       sync.Once
	assetTree      *virtualfs.VirtualAssetTree
	assetTreeErr   error
	catalogOnce    sync.Once
	commandCatalog []virtualfs.Command
	catalogErr     error
)

func getCatalog() ([]virtualfs.Command, error) {
	catalogOnce.Do(func() {
		tree, err := getAssetTree()
		if err != nil {
			catalogErr = err
			return
		}
		commandCatalog, catalogErr = virtualfs.BuildCatalog(tree, assetDescriptions)
	})
	return commandCatalog, catalogErr
}

// getAssetTree trả về singleton VirtualAssetTree được nạp một lần duy nhất vào Heap RAM
func getAssetTree() (*virtualfs.VirtualAssetTree, error) {
	treeOnce.Do(func() {
		assetTree, assetTreeErr = virtualfs.NewTreeFromFS(embeddedAssets)
	})
	return assetTree, assetTreeErr
}

// runCLI thực thi logic phân luồng CLI với các luồng I/O được tiêm vào (Dependency Injection)
// Trả về (handled bool, exitCode int)
func runCLI(stdout io.Writer, stderr io.Writer, args []string) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}

	first := strings.TrimSpace(args[0])
	lowerFirst := strings.ToLower(first)
	if len(args) > 2 {
		fmt.Fprintln(stderr, "Expected <lane> [reference], help [<lane>|init], <lane>|init --help, --version, init, or mcp; extra arguments are not allowed.")
		return true, 1
	}
	// Help is read-only and must be dispatched before init or asset sync.
	if isHelpToken(first) {
		if len(args) == 1 {
			return true, runHelp(stdout, stderr, "", true)
		}
		if lowerFirst == "help" {
			return true, runHelp(stdout, stderr, args[1], false)
		}
		fmt.Fprintln(stderr, "Use help <lane> or help init; --help and -h accept no additional arguments.")
		return true, 1
	}
	if len(args) == 2 && isHelpToken(args[1]) {
		return true, runHelp(stdout, stderr, first, false)
	}
	if lowerFirst == "init" {
		if len(args) != 1 {
			fmt.Fprintln(stderr, "Expected init; extra arguments are not allowed.")
			return true, 1
		}
		return true, runInit(stdout, stderr)
	}
	if (lowerFirst == "mcp" || lowerFirst == "--version") && len(args) != 1 {
		fmt.Fprintln(stderr, "Expected <lane> [reference], help, --version, init, or mcp; extra arguments are not allowed.")
		return true, 1
	}
	if lowerFirst == "--version" {
		text := "ceo-plugin " + version.Value + "\n"
		n, err := io.WriteString(stdout, text)
		if err == nil && n != len(text) {
			err = io.ErrShortWrite
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return true, 1
		}
		return true, 0
	}

	// Cờ mcp -> chuyển sang chạy MCP Server
	if lowerFirst == "mcp" {
		return false, 0
	}

	tree, err := getAssetTree()
	if err != nil {
		fmt.Fprintf(stderr, "Lỗi khởi tạo VirtualAssetTree: %v\n", err)
		return true, 1
	}

	// Nhánh 1: Lệnh Tổng (Khi len(args) == 1)
	if len(args) == 1 {
		entry, err := tree.GetSkill(first)
		if err != nil {
			if errors.Is(err, virtualfs.ErrRoleNotFound) {
				fmt.Fprintf(stderr, "Lệnh không hợp lệ: role %q không tồn tại\n\n", first)
			} else {
				fmt.Fprintf(stderr, "Lỗi truy xuất skill cho role %q: %v\n\n", first, err)
			}
			printUsage(stderr)
			return true, 1
		}

		// LUẬT THÉP ANTI-SUMMARIZATION (ZERO CONTENT LOSS): Xuất trực tiếp không chèn thêm bất kỳ banner hay ký tự nào
		return true, outputAsset(stdout, stderr, entry.Content)
	}

	// Nhánh 2: Lệnh Con (Khi len(args) >= 2)
	subName := strings.TrimSpace(args[1])
	entry, err := tree.GetReference(first, subName)
	if err != nil {
		if errors.Is(err, virtualfs.ErrRoleNotFound) {
			fmt.Fprintf(stderr, "Lệnh không hợp lệ: role %q không tồn tại\n\n", first)
		} else {
			fmt.Fprintf(stderr, "Lệnh không hợp lệ: không tìm thấy quy chế/biểu mẫu %q trong vai trò %q\n\n", subName, first)
		}
		printUsage(stderr)
		return true, 1
	}

	// Xuất trực tiếp nội dung quy chế / biểu mẫu nguyên bản
	return true, outputAsset(stdout, stderr, entry.Content)
}

func isHelpToken(token string) bool {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "help", "--help", "-h":
		return true
	}
	return false
}

func runHelp(stdout, stderr io.Writer, target string, global bool) int {
	var err error
	switch {
	case global:
		err = printUsage(stdout)
	case strings.EqualFold(strings.TrimSpace(target), "init"):
		err = printInitUsage(stdout)
	default:
		err = printLaneUsage(stdout, target)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func syncContext(root string) aicontext.SyncResult {
	commands, err := getCatalog()
	if err != nil {
		return aicontext.SyncResult{Status: "failed", Reason: err.Error()}
	}
	r, _ := aicontext.SyncWorkspace(context.Background(), root, aicontext.RenderManagedBlock(commands))
	return r
}

func outputAsset(stdout, stderr io.Writer, content string) int {
	cwd, err := os.Getwd()
	if err == nil {
		ws := aicontext.ResolveWorkspace(cwd, "", "cli")
		r := syncContext(ws.Root)
		if ws.Reason != "" {
			r.Reason = ws.Reason
		}
		r.Warn(stderr)
	} else {
		aicontext.SyncResult{Status: "skipped", Reason: err.Error()}.Warn(stderr)
	}
	n, err := io.WriteString(stdout, content)
	if err == nil && n != len(content) {
		err = io.ErrShortWrite
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() {
	if err := installstate.GuardStartup(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(os.Args) > 1 {
		handled, exitCode := runCLI(os.Stdout, os.Stderr, os.Args[1:])
		if handled {
			if exitCode != 0 {
				os.Exit(exitCode)
			}
			return
		}
	}

	tree, err := getAssetTree()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Lỗi khởi tạo VirtualAssetTree: %v\n", err)
		os.Exit(1)
	}

	// Chạy MCP Stdio Server (khi không có tham số hoặc khi có cờ mcp)
	server := mcpserver.NewServer(tree)
	server.Diagnostics = os.Stderr
	server.SyncWorkspace = syncContext
	if err := server.Serve(os.Stdin, os.Stdout); err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "Lỗi vận hành MCP Server: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
