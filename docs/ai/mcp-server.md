---
description: "Give AI coding agents access to AST Metrics through the built-in MCP server: setup for Claude Code, Cursor and other clients."
---

# MCP Server

Let AI agents analyze your codebase. AST Metrics includes a built-in [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server that exposes all metrics through a standard interface.

!!! tip "How it works"
    The `ast-metrics mcp` command starts an MCP server over **stdio**. Your AI client launches it automatically: you never run it manually.

## Available tools

<div class="mcp-tools-grid" markdown>
<div class="mcp-tool" markdown>
### `analyze_project`
Project-level overview: LOC, complexity, maintainability index.
</div>
<div class="mcp-tool" markdown>
### `get_file_metrics`
Detailed metrics for a specific file.
</div>
<div class="mcp-tool" markdown>
### `find_complex_code`
Locate high-complexity hotspots in the codebase.
</div>
<div class="mcp-tool" markdown>
### `find_risky_code`
Find code with high risk scores (complexity × churn).
</div>
<div class="mcp-tool" markdown>
### `get_coupling`
Analyze coupling between components.
</div>
<div class="mcp-tool" markdown>
### `get_dependencies`
List dependency relationships across files.
</div>
<div class="mcp-tool" markdown>
### `get_communities`
The communities the code forms on the dependency graph, with the shared kernel, the cycles, the findings and the actions to take first.
</div>
<div class="mcp-tool" markdown>
### `get_test_quality`
Evaluate test coverage and quality.
</div>
<div class="mcp-tool" markdown>
### `list_components`
Inventory of all classes, functions, and components.
</div>
</div>

## Setup

=== "Claude Desktop"

    Add to your `claude_desktop_config.json`:

    ```json
    {
      "mcpServers": {
        "ast-metrics": {
          "command": "ast-metrics",
          "args": ["mcp", "."]
        }
      }
    }
    ```

=== "Cursor"

    Add to your `.cursor/mcp.json`:

    ```json
    {
      "mcpServers": {
        "ast-metrics": {
          "command": "ast-metrics",
          "args": ["mcp", "."]
        }
      }
    }
    ```

=== "Other clients"

    Any MCP-compatible client can use AST Metrics. The server command is:

    ```
    ast-metrics mcp
    ```

    Transport: **stdio**. Refer to your client's documentation for the configuration format.
