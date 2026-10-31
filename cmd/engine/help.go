package main

import (
	"fmt"
	"io"
	"strings"

	"ceo-plugin/internal/virtualfs"
)

// Titles are presentation only; commands and descriptions come from the catalog.
var helpLaneTitles = map[string]string{
	"ceo":               "CEO — Orchestration & Coordination",
	"system-architect":  "System Architect — Architecture & SPEC",
	"coder":             "Coder — Implementation",
	"qa":                "QA — Verification Protocols & Quality Rules",
	"supervisor":        "Supervisor — Zero-Trust & Audit Rules",
	"strategic-planner": "Strategic Planner — Plan & Ledger Templates",
}

func helpReferenceLane(lane string) string {
	switch lane {
	case "architect":
		return "system-architect"
	case "mechanical-planner":
		return "strategic-planner"
	}
	return lane
}

func helpCommand(cmd virtualfs.Command) string {
	text := "ceo-plugin " + cmd.Lane
	if cmd.Reference != "" {
		text += " " + cmd.Reference
	}
	return text
}

func appendHelpRows(b *strings.Builder, indent string, rows []virtualfs.Command) {
	width := 0
	for _, cmd := range rows {
		if n := len(helpCommand(cmd)); n > width {
			width = n
		}
	}
	for _, cmd := range rows {
		fmt.Fprintf(b, "%s%-*s  %s\n", indent, width, helpCommand(cmd), cmd.Description)
	}
}

func writeHelp(w io.Writer, text string) error {
	n, err := io.WriteString(w, text)
	if err == nil && n != len(text) {
		return io.ErrShortWrite
	}
	return err
}

func printUsage(w io.Writer) error {
	commands, err := getCatalog()
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("CEO Plugin - documentation from embedded assets\nWindows executable: ~/.ceo-plugin/bin/ceo-plugin.exe\n\n")
	b.WriteString("Help & Discovery:\n")
	b.WriteString("  ceo-plugin help           Show global help (also --help or -h)\n")
	b.WriteString("  ceo-plugin help <lane>    Show help and subcommands for a lane (e.g. ceo-plugin help ceo)\n")
	b.WriteString("  ceo-plugin <lane> --help  Alternative lane help syntax (also help or -h)\n")
	b.WriteString("  ceo-plugin init --help    Show workspace initialization instructions\n\n")
	b.WriteString("Command Selection Guide (Loads Core SKILL.md Only):\n")
	var primary []virtualfs.Command
	for _, cmd := range commands {
		if cmd.Reference == "" {
			primary = append(primary, cmd)
		}
	}
	appendHelpRows(&b, "  ", primary)
	b.WriteString("\nSpecialized Subcommands (Grouped by Lane):\n")
	// The catalog establishes group order and membership, not a second inventory.
	for start := 0; start < len(commands); {
		if commands[start].Reference == "" {
			start++
			continue
		}
		end := start + 1
		for end < len(commands) && commands[end].Lane == commands[start].Lane && commands[end].Reference != "" {
			end++
		}
		lane := commands[start].Lane
		title := helpLaneTitles[lane]
		if title == "" {
			title = lane
		}
		fmt.Fprintf(&b, "\n  %s:\n", title)
		appendHelpRows(&b, "    ", commands[start:end])
		start = end
	}
	b.WriteString("\nNote: 'architect' shares references with 'system-architect'; 'mechanical-planner' shares templates with 'strategic-planner'.\n")
	b.WriteString("These commands return documentation; they do not create agents or execute workflows.\n\n")
	b.WriteString("MCP Server Mode: ceo-plugin mcp (also the default with no arguments)\nVersion: ceo-plugin --version\n")
	b.WriteString("Workspace Initialization: ceo-plugin init (creates a Git repository when needed and updates .gitignore, AGENTS.md, and CLAUDE.md)\n")
	return writeHelp(w, b.String())
}

func printLaneUsage(w io.Writer, target string) error {
	lane := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(target)), "_", "-")
	commands, err := getCatalog()
	if err != nil {
		return err
	}
	var primary []virtualfs.Command
	for _, cmd := range commands {
		if cmd.Lane == lane && cmd.Reference == "" {
			primary = append(primary, cmd)
		}
	}
	if len(primary) == 0 {
		return fmt.Errorf("Role %q does not exist. Valid roles: %s", target, strings.Join(virtualfs.PrimaryRoles, ", "))
	}
	canonical := helpReferenceLane(lane)
	var b strings.Builder
	fmt.Fprintf(&b, "Role: %s (%s)\n\n", lane, helpLaneTitles[canonical])
	if canonical != lane {
		fmt.Fprintf(&b, "References and templates shared with '%s'.\n\n", canonical)
	}
	b.WriteString("Primary Command:\n")
	appendHelpRows(&b, "  ", primary)
	b.WriteString("\nSpecialized Subcommands (References & Rules):\n")
	var references []virtualfs.Command
	for _, cmd := range commands {
		if cmd.Lane == canonical && cmd.Reference != "" {
			cmd.Lane = lane // Show executable syntax using the requested alias.
			references = append(references, cmd)
		}
	}
	if len(references) == 0 {
		fmt.Fprintf(&b, "  (No specialized subcommands for this role. Use 'ceo-plugin %s' to load core instructions.)\n", lane)
	} else {
		appendHelpRows(&b, "  ", references)
	}
	return writeHelp(w, b.String())
}

func printInitUsage(w io.Writer) error {
	return writeHelp(w, `CEO Plugin Workspace Initialization

Usage:
  ceo-plugin init

Description:
  Initializes or updates workspace AI context files (AGENTS.md and CLAUDE.md)
  and ensures proper gitignore protection.

Execution Details:
  - Resolves the nearest valid Git workspace root, including worktrees and submodules.
  - If no repository exists, runs 'git init' in the current working directory.
  - Rejects unsafe workspace roots.
  - Ensures the root .gitignore has effective rules covering /AGENTS.md and /CLAUDE.md.
  - Creates or updates the managed documentation block in AGENTS.md and CLAUDE.md
    while preserving user-authored content outside the markers.
  - Idempotent: repeated runs with unchanged content will not rewrite files.
  - Already tracked files stay tracked and produce a warning; .gitignore does not hide their changes.
  - Does not stage or commit files. Changes to .gitignore may appear in Git status.
  - Failures return a nonzero exit code and report completed changes if initialization was partial.
  - Git must be available in PATH. The init command accepts no additional arguments.

Help only: help init, init help, init --help, or init -h prints this guide without changing the workspace.
`)
}
