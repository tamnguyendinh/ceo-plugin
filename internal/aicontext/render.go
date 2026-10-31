package aicontext

import (
	"fmt"
	"strings"

	"ceo-plugin/internal/virtualfs"
)

const (
	StartMarker    = "<!-- ceo-plugin:start -->"
	EndMarker      = "<!-- ceo-plugin:end -->"
	OldStartMarker = "<!-- anvien-ceo-plugin:start -->"
	OldEndMarker   = "<!-- anvien-ceo-plugin:end -->"
)

type LaneSection struct {
	Lane    string
	Heading string
}

var specializedSections = []LaneSection{
	{Lane: "ceo", Heading: "### 2. CEO — Orchestration & Coordination Subcommands"},
	{Lane: "system-architect", Heading: "### 3. System Architect — Architecture & SPEC Subcommands"},
	{Lane: "qa", Heading: "### 4. QA — Verification Protocols & Quality Rules"},
	{Lane: "supervisor", Heading: "### 5. Supervisor — Zero-Trust & Audit Rules"},
	{Lane: "strategic-planner", Heading: "### 6. Strategic Planner — Plan & Ledger Templates"},
}

func RenderManagedBlock(commands []virtualfs.Command) string {
	var b strings.Builder
	b.WriteString(StartMarker + "\n## Command Selection Guide\nUse automatically when the user requests orchestration via the CEO workflow or when coordination is required.\n\n")
	b.WriteString("### How to use ceo-plugin:\nIn Windows:\n\n| Environment | Exact Execution Syntax |\n| :--- | :--- |\n")
	b.WriteString("| PowerShell | & \"$HOME\\.ceo-plugin\\bin\\ceo-plugin.exe\" |\n")
	b.WriteString("| CMD | \"%USERPROFILE%\\.ceo-plugin\\bin\\ceo-plugin.exe\" |\n")
	b.WriteString("| Git Bash | \"$HOME/.ceo-plugin/bin/ceo-plugin.exe\" |\n\n")
	b.WriteString("Lane and reference commands return documentation; they do not create agents or execute the described workflow.\n\n")
	b.WriteString("### Command Help & Discovery\n")
	b.WriteString("Use these commands to inspect available capabilities, lane references, and usage rules directly from your terminal:\n\n")
	b.WriteString("| When you need to... | Use command |\n| :--- | :--- |\n")
	b.WriteString("| View global help and full command catalog | `ceo-plugin help` (or `--help`, `-h`) |\n")
	b.WriteString("| View subcommands and rules for a specific lane | `ceo-plugin help <lane>` (e.g. `ceo-plugin help ceo`, `ceo-plugin help qa`) |\n")
	b.WriteString("| View lane help via flag | `ceo-plugin <lane> --help` |\n")
	b.WriteString("| View workspace initialization guide | `ceo-plugin init --help` (or `ceo-plugin help init`) |\n\n")
	b.WriteString("### Workspace Initialization — `ceo-plugin init`\n")
	b.WriteString("Run explicitly from the project directory when you need to create or update content for `AGENTS.md` and `CLAUDE.md`. Git must be available in PATH; `init` accepts no extra arguments.\n\n")
	b.WriteString("| Environment | Exact Execution Syntax |\n| :--- | :--- |\n")
	b.WriteString("| PowerShell | & \"$HOME\\.ceo-plugin\\bin\\ceo-plugin.exe\" init |\n")
	b.WriteString("| CMD | \"%USERPROFILE%\\.ceo-plugin\\bin\\ceo-plugin.exe\" init |\n")
	b.WriteString("| Git Bash | \"$HOME/.ceo-plugin/bin/ceo-plugin.exe\" init |\n\n")
	b.WriteString("Uses the nearest valid Git workspace root, including worktrees and submodules; if no repository exists, runs `git init` in the current directory. Unsafe roots are rejected.\n")
	b.WriteString("Ensures root `.gitignore` rules cover `/AGENTS.md` and `/CLAUDE.md`, then creates or updates the managed guide in both files while preserving user content outside the managed block. Repeated runs with unchanged content do not rewrite the files.\n")
	b.WriteString("Already tracked context files remain tracked and produce a warning; `.gitignore` changes may appear in `git status`. The command does not stage or commit. Failures return a nonzero exit code and report completed changes if initialization was partial.\n\n")

	// Section 1: Primary Lane Commands
	b.WriteString("### 1. Primary Lane Commands (Loads Core SKILL.md Only)\n")
	b.WriteString("| When you need to... | Use command |\n| :--- | :--- |\n")
	for _, cmd := range commands {
		if cmd.Reference == "" {
			fmt.Fprintf(&b, "| %s | `ceo-plugin %s` |\n", escapeCell(cmd.Description), cmd.Lane)
		}
	}

	// Sections 2-6: Specialized Subcommands Grouped by Lane
	for _, sec := range specializedSections {
		var laneCmds []virtualfs.Command
		for _, cmd := range commands {
			if cmd.Lane == sec.Lane && cmd.Reference != "" {
				laneCmds = append(laneCmds, cmd)
			}
		}
		if len(laneCmds) > 0 {
			b.WriteString("\n" + sec.Heading + "\n")
			b.WriteString("| When you need to... | Use command |\n| :--- | :--- |\n")
			for _, cmd := range laneCmds {
				fmt.Fprintf(&b, "| %s | `ceo-plugin %s %s` |\n", escapeCell(cmd.Description), cmd.Lane, cmd.Reference)
			}
		}
	}

	b.WriteString("\n`architect` shares references with `system-architect`; `mechanical-planner` shares templates with `strategic-planner`.\n")
	b.WriteString("MCP roots initialization, explicit `ceo-plugin init`, and valid lane commands/tools/prompts each create missing AGENTS.md and CLAUDE.md files or update this guide in a resolved workspace, preserving user content outside the managed block. Deleted context files are recreated on the next valid sync. Help commands are read-only. This is not a file watcher and does not force an existing AI session to reload instructions.\n")
	b.WriteString(EndMarker)
	return b.String()
}

func escapeCell(s string) string {
	return strings.NewReplacer("|", "&#124;", "`", "&#96;", "\r", " ", "\n", " ").Replace(s)
}
