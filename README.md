# Warehouse UI

> Open-source universal database IDE — connect to BigQuery, PostgreSQL, MySQL, SQLite, MongoDB, ClickHouse from one native desktop app. Browse schemas, write SQL or MongoDB queries with Monaco, estimate costs before running, get AI-powered query suggestions, and export results as CSV, JSON, or Excel.

**Keywords**: database IDE, SQL editor, BigQuery GUI, PostgreSQL client, MySQL workbench alternative, MongoDB GUI, database browser, query tool, schema explorer, SQL cost estimator, AI SQL assistant, data analytics tool, open source database client, desktop database app, Wails Go Svelte

---

## Download

Grab the latest release for your platform:

| Platform | Download |
|----------|----------|
| macOS (Intel + Apple Silicon) | [warehouse-ui-macos-universal.dmg](https://github.com/olegnazarov23/warehouse-ui/releases/latest) |
| Windows (64-bit) | [warehouse-ui-windows-amd64-installer.exe](https://github.com/olegnazarov23/warehouse-ui/releases/latest) |

Or build from source (see below).

## Features

- **Multi-database support** — BigQuery, PostgreSQL, MySQL, SQLite, MongoDB, ClickHouse from a single app
- **Native desktop performance** — Built with Go + Wails v2 for near-instant startup
- **Schema browser** — Tree view with databases/datasets, tables/collections, columns, types, and row counts
- **Monaco SQL editor** — Syntax highlighting, schema-aware autocomplete, multi-tab, formatting
- **MongoDB support** — Shell-style queries (find, aggregate, countDocuments, distinct) with automatic document-to-table flattening
- **SSH tunneling** — Connect through bastion hosts with key or password auth, ProxyJump (-J) support, auto-resolves ~/.ssh/config aliases and keys
- **Query explain visualizer** — Visual tree of query execution plans (PostgreSQL, MySQL, SQLite)
- **Chart panel** — Bar, line, and pie charts from query results with auto-detected axes
- **Virtual scrolling** — Handles 10k+ row results without DOM bloat
- **Inline cell editing** — Double-click cells in preview tables to generate UPDATE queries
- **Data diff** — Compare two query results side-by-side with color-coded changes
- **Cost projection** — Dry-run queries on BigQuery/ClickHouse to see estimated cost, rows, and referenced tables before execution
- **AI assistant** — Pluggable LLM (OpenAI, Anthropic, Ollama, or any local model). Schema-aware SQL generation, query explanation, and optimization
- **AI query optimizer** — One-click iterative optimization: AI suggests improvements, each verified by dry-run
- **Multi-chat with memory** — Persistent AI conversations with auto-generated titles (ChatGPT-style)
- **Resizable panels** — Drag to resize sidebar, editor/results split, and AI panel
- **Query history** — Auto-saved with execution stats (duration, cost, bytes, rows)
- **Saved queries** — Organize and share queries with URL slugs
- **Built-in templates** — Starter queries for each database type
- **Export** — CSV, JSON, and Excel (.xlsx) with native file dialogs
- **SQL hints** — Static analyzer suggests common query improvements (missing LIMIT, SELECT *, leading wildcards, etc.)
- **Deep codebase context** — Link local repos and the AI loads full source files (models, services, controllers, helpers) to understand your business logic, entity relationships, and data flows — ask "find offers in campaigns" and it traces through your code to write the right query
- **Auto-detect connections** — Scans .env files and Docker Compose for database connections, offers one-click Test & Add
- **Auto-discovery** — One-click "Discover & Connect" scans all datasets/tables and generates AI sample queries
- **Encrypted credentials** — API keys and connection passwords are AES-256-GCM encrypted at rest
- **Dark theme** — Designed for long query sessions

## Screenshots

_Coming soon_

## Quick Start

### Option 1: Download the app

1. Download from the [Releases page](https://github.com/olegnazarov23/warehouse-ui/releases/latest)
2. Install and launch
3. Add a database connection (BigQuery, PostgreSQL, MySQL, SQLite, or MongoDB)
4. Click the gear icon in the AI panel to configure your AI provider (optional)

### Option 2: Build from source

**Prerequisites:** [Go 1.22+](https://go.dev/dl/), [Wails CLI](https://wails.io/docs/gettingstarted/installation), [Node.js 18+](https://nodejs.org/)

```bash
git clone https://github.com/olegnazarov23/warehouse-ui.git
cd warehouse-ui

# Install frontend deps
cd frontend && npm install && cd ..

# Run in dev mode (hot reload)
wails dev

# Or build a production binary
wails build
./build/bin/warehouse-ui
```

## AI Providers

Configure AI in-app via the gear icon in the AI panel — no .env file needed.

| Provider | Models | Setup |
|----------|--------|-------|
| OpenAI | GPT-4o, GPT-4, o1, etc. | Enter API key in Settings |
| Anthropic | Claude Sonnet, Opus | Enter API key in Settings |
| Ollama | Llama 3, Codestral, Qwen, etc. | Run Ollama locally, set endpoint |
| Local (LM Studio, vLLM, LocalAI, llama.cpp) | Any model | Select "Local", set endpoint URL |

All API keys are encrypted at rest using AES-256-GCM. Keys are validated with a test call before saving.

The AI assistant is schema-aware — it sees your connected database's tables and columns, current SQL in the editor, query results, and any code context from linked repositories.

## Architecture

```
Go (Wails v2)           Svelte 5 + TailwindCSS 4
┌─────────────────┐     ┌─────────────────────────┐
│  app.go         │◄───►│  App.svelte              │
│  ├─ driver/     │     │  ├─ ConnectionPage       │
│  │  ├─ bigquery │     │  ├─ Shell (3-panel)      │
│  │  ├─ postgres │     │  │  ├─ Sidebar           │
│  │  ├─ mysql    │     │  │  │  ├─ SchemaTree     │
│  │  ├─ mongodb  │     │  │  │  ├─ SavedQueries   │
│  │  └─ sqlite   │     │  │  │  ├─ History        │
│  ├─ tunnel/     │     │  │  │  └─ Templates      │
│  │  └─ ssh      │     │  │  ├─ QueryEditor       │
│  ├─ store/      │     │  │  ├─ ResultsPanel      │
│  │  └─ sqlite   │     │  │  └─ DataGrid          │
│  └─ ai/         │     │  └─ AiPanel              │
│     ├─ openai   │     └─────────────────────────┘
│     ├─ anthropic│
│     └─ ollama   │
└─────────────────┘
        │                          │
        └──── Wails Bindings ──────┘
```

**Go backend** handles all database connections, query execution, local storage, AI API calls, code scanning, and export. Every public method on the `App` struct is automatically available to the Svelte frontend via Wails bindings.

**Svelte frontend** is a 3-panel layout: schema sidebar, Monaco editor + results, and an optional AI chat panel. All communication with Go goes through the generated Wails JS bindings.

**Embedded SQLite** (pure Go, no CGO) stores connections, query history, saved queries, AI conversations, and settings locally. All sensitive data (passwords, API keys) is encrypted.

## Database Drivers

| Database | Status | Cost Estimate | Notes |
|----------|--------|---------------|-------|
| BigQuery | Done | Yes ($5/TB) | Service account JSON auth, dry-run, referenced tables |
| PostgreSQL | Done | No | pgx driver, SSL support, EXPLAIN row estimates, SSH tunnel |
| MySQL | Done | No | go-sql-driver, SSL support, EXPLAIN row estimates, SSH tunnel |
| SQLite | Done | No | Pure Go (modernc.org), no CGO needed |
| MongoDB | Done | No | Shell-style queries (find/aggregate/distinct), document flattening, SSH tunnel |
| ClickHouse | Planned | Yes | Coming soon |

## Deep Codebase Context

Link local code repositories when setting up a connection. The AI loads your **full source files** — not just keyword-matched snippets — so it understands your entire application:

1. **Loads full files** prioritized by relevance: models/schemas first, then services/controllers, then helpers/utils, then everything else (up to 300KB budget)
2. **Understands relationships** — traces entity models, business logic, data flows, and how your code interacts with the database
3. **Detects database connections** in `.env` files (connection strings, `DATABASE_URL`, etc.)
4. **Finds Docker databases** in `docker-compose.yml` (postgres, mysql, mongo, redis, clickhouse)

Ask the AI business-level questions like "find offers in campaigns" and it will trace through your model definitions and service logic to generate the correct query.

Detected connections can be tested and added as saved connections with one click.

## Export Formats

- **CSV** — Standard comma-separated values
- **JSON** — Pretty-printed array of objects
- **Excel** — `.xlsx` with headers, auto-fitted columns, and styled header row

All exports use native file dialogs and support up to 100,000 rows.

## SQL Hints (Static Analyzer)

The Messages tab shows automatic suggestions for your query:

- **Performance**: SELECT *, ORDER BY without LIMIT, LIKE with leading wildcard, functions on WHERE columns
- **Warnings**: NOT IN with subquery (null-unsafe), implicit cross joins, missing partition filters (BigQuery)
- **Tips**: Nested subqueries -> CTEs, missing LIMIT, single-line formatting

No AI required — works offline, instant analysis.

## Security

- API keys and connection passwords encrypted at rest (AES-256-GCM)
- API keys validated before saving (test call to provider)
- No data leaves your machine unless you configure a cloud AI provider
- Local AI mode (Ollama, LM Studio, etc.) keeps everything on your device
- Credentials files (.env, service accounts, .pem, .key) are gitignored

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

MIT
