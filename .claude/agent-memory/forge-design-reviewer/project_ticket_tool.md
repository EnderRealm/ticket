---
name: tk project architecture
description: tk is a Go CLI/MCP/TUI ticket manager with markdown+YAML storage; four layers sharing pkg/ticket core library
type: project
---

tk is a Go binary with four layers: pkg/ticket/ (core), cmd/ (cobra CLI), internal/tui/ (bubbletea), internal/mcp/ (MCP server). Tickets are markdown files with YAML frontmatter in .tickets/. MCP tests use in-process harness via go-sdk NewInMemoryTransports. The codebase uses pflag via cobra but has never used StringSlice or StringArray flags -- all multi-value inputs use comma-separated String flags.

**Why:** Understanding the architecture is essential for accurate design reviews.
**How to apply:** When reviewing designs, verify claims against the four-layer architecture and existing patterns (e.g., flag types, error handling patterns, JSON struct conventions).
