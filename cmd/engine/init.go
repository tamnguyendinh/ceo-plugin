package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"ceo-plugin/internal/aicontext"
)

func runInit(stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "Error resolving current directory: %v\n", err)
		return 1
	}
	commands, err := getCatalog()
	if err != nil {
		fmt.Fprintf(stderr, "Error loading commands catalog: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, err := aicontext.InitWorkspace(ctx, cwd, aicontext.RenderManagedBlock(commands))
	return writeInitResult(stdout, stderr, result, err)
}

func initSummary(result aicontext.InitResult) string {
	var b strings.Builder
	if result.WorkspaceRoot != "" {
		fmt.Fprintf(&b, "  Workspace: %s\n", result.WorkspaceRoot)
	}
	fmt.Fprintf(&b, "  Git Repo:  %s\n", result.GitAction)
	for _, f := range result.Files {
		label := "Context"
		if filepath.Base(f.Path) == ".gitignore" {
			label = "Gitignore"
		}
		fmt.Fprintf(&b, "  %-9s %s (%s)\n", label+":", f.Status, f.Path)
		if f.Error != "" {
			fmt.Fprintf(&b, "    Error: %s\n", f.Error)
		}
		for _, path := range f.Recovery {
			fmt.Fprintf(&b, "    Recovery: %s\n", path)
		}
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(&b, "  Warning: %s\n", warning)
	}
	return b.String()
}

func writeInitText(w io.Writer, text string) error {
	n, err := io.WriteString(w, text)
	if err == nil && n != len(text) {
		err = io.ErrShortWrite
	}
	return err
}

func writeInitResult(stdout, stderr io.Writer, result aicontext.InitResult, initErr error) int {
	summary := initSummary(result)
	if initErr != nil || result.Status != "success" {
		reason := result.Error
		if initErr != nil {
			reason = initErr.Error()
		}
		message := fmt.Sprintf("Initialization failed: status=%s stage=%s: %s\n%s", result.Status, result.Stage, reason, summary)
		if result.Status == "partial" {
			message += "Completed changes were retained; resolve the error before retrying.\n"
		}
		if err := writeInitText(stderr, message); err != nil {
			_ = writeInitText(stdout, message+"Diagnostic output failed: "+err.Error()+"\n")
		}
		return 1
	}
	if len(result.Warnings) > 0 {
		var warnings strings.Builder
		for _, warning := range result.Warnings {
			fmt.Fprintf(&warnings, "Warning: %s\n", warning)
		}
		if err := writeInitText(stderr, warnings.String()); err != nil {
			_ = writeInitText(stdout, "Initialization changes completed, but warning output failed: "+err.Error()+"\n"+summary)
			return 1
		}
	}
	// Warnings already went to stderr; keep the successful table on stdout.
	cleanResult := result
	cleanResult.Warnings = nil
	if err := writeInitText(stdout, "CEO Plugin AI Context initialized successfully:\n"+initSummary(cleanResult)); err != nil {
		_ = writeInitText(stderr, "Initialization changes completed, but result output failed: "+err.Error()+"\n"+summary)
		return 1
	}
	return 0
}
