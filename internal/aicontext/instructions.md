# CEO Plugin — MCP Server Instructions & Operating Guide

Whenever you enter or work in a repository, immediately check both AGENTS.md and CLAUDE.md in the workspace root.

Each file must contain the CEO Plugin instructions enclosed between <!-- ceo-plugin:start --> and <!-- ceo-plugin:end -->.

If either file is missing, either marker is missing, or the enclosed section is empty, you MUST run the bootstrap command in your terminal:

powershell
& "$HOME\.ceo-plugin\bin\ceo-plugin.exe" init

CMD
"%USERPROFILE%\.ceo-plugin\bin\ceo-plugin.exe" init

Git Bash
"$HOME/.ceo-plugin/bin/ceo-plugin.exe" init

What this command does:
- Detects the nearest valid Git repository (runs git init if none exists).
- Ensures the root .gitignore protects /AGENTS.md and /CLAUDE.md.
- Generates or updates the managed AI context block while preserving all existing user content.
- Enables all AI agents to access the complete command catalog and operating guidelines.

## 🛠️ MCP TOOLS DIRECTORY & USAGE RULES

This MCP server exposes 4 specialized lazy-loaded tools:

| MCP Tool Name | Purpose | When to Use | Key Arguments |
| :--- | :--- | :--- | :--- |
| `get_lane_skill` | Retrieves the primary operating principles (SKILL.md) for a specific role. | When assuming a role (e.g. CEO, Architect, QA) or onboarding an agent. | `lane`: `ceo`, `system-architect`, `architect`, `coder`, `qa`, `supervisor`, `mechanical-planner`, `strategic-planner` |
| `get_lane_reference` | Retrieves a specific reference guideline, protocol, or template. | When needing exact rules (e.g. backend QA protocol, mode 0/1/2 architect rules, timer rules). | `lane`: role name<br>`reference`: reference name (e.g. `timer-rules`, `backend-qa-protocol`, `plan-template`) |
| `ceo_command` | Universal shortcut to fetch any document using `<lane> [reference]`. | Quick retrieval of skills or subcommands in a single tool call. | `command`: e.g. `"qa e2e-data-flow"`, `"ceo standby-rules"`, `"coder"` |
| `ceo_plugin` | Returns only this initial operating guide (`instructions.md`) verbatim. | Required first step before using CEO Plugin; read the entire result before confirming readiness. | `{}` *(No arguments)* |

> [!IMPORTANT]
> **Documentation Only:** These MCP tools return documentation, operating rules, and templates. Calling them does not execute code, alter git state, or launch external agents. You (the AI) must read the returned rules and execute the recommended workflow.

## 💻 CLI EXECUTION SYNTAX & HELP DISCOVERY

When working via the command line or terminal, execute ceo-plugin using the appropriate shell syntax:

### Shell Execution Syntax (Windows)
| Environment | Exact Execution Syntax |
| :--- | :--- |
| **PowerShell** | `& "$HOME\.ceo-plugin\bin\ceo-plugin.exe" <command>` |
| **CMD** | `"%USERPROFILE%\.ceo-plugin\bin\ceo-plugin.exe" <command>` |
| **Git Bash** | `"$HOME/.ceo-plugin/bin/ceo-plugin.exe" <command>` |

### Command Discovery & Help
Use the built-in help commands to inspect available capabilities directly:
- `ceo-plugin help` (or `--help`, `-h`): View the global catalog and all available lanes.
- `ceo-plugin help <lane>` (or `ceo-plugin <lane> --help`): View specific subcommands and rules for a single lane (e.g. `ceo-plugin help ceo`, `ceo-plugin help qa`).
- `ceo-plugin init --help`: View workspace initialization details.
