# Warehouse UI

> Open-source universal database IDE — connect to BigQuery, PostgreSQL, MySQL, SQLite, ClickHouse from one native desktop app. Browse schemas, write SQL with Monaco, estimate costs before running, get AI-powered query suggestions, and export results as CSV, JSON, or Excel.

**Keywords**: database IDE, SQL editor, BigQuery GUI, PostgreSQL client, MySQL workbench alternative, database browser, query tool, schema explorer, SQL cost estimator, AI SQL assistant, data analytics tool, open source database client, desktop database app, Wails Go Svelte

---

## Download

Grab the latest release for your platform:

| Platform | Download |
|----------|----------|
| macOS (Intel + Apple Silicon) | [warehouse-ui-macos-universal.dmg](https://github.com/olegnazarov23/warehouse-ui/releases/latest) |
| Windows (64-bit) | [warehouse-ui-windows-amd64-installer.exe](https://github.com/olegnazarov23/warehouse-ui/releases/latest) |

Or build from source (see below).

## Features

- **Multi-database support** — BigQuery, PostgreSQL, MySQL, SQLite, ClickHouse from a single app
- **Native desktop performance** — Built with Go + Wails v2 for near-instant startup
- **Schema browser** — Tree view with databases/datasets, tables, columns, types, and row counts
- **Monaco SQL editor** — Syntax highlighting, autocomplete, multi-tab, formatting (Ctrl+Shift+F)
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
- **Code repo scanning** — Link local codebases so AI learns your query patterns, ORM models, and migrations
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
3. Add a database connection (BigQuery, PostgreSQL, MySQL, or SQLite)
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
│  │  └─ sqlite   │     │  │  │  ├─ SavedQueries   │
│  ├─ store/      │     │  │  │  ├─ History        │
│  │  └─ sqlite   │     │  │  │  └─ Templates      │
│  └─ ai/         │     │  │  ├─ QueryEditor       │
│     ├─ openai   │     │  │  ├─ ResultsPanel      │
│     ├─ anthropic│     │  │  └─ DataGrid          │
│     └─ ollama   │     │  └─ AiPanel              │
└─────────────────┘     └─────────────────────────┘
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
| PostgreSQL | Done | No | pgx driver, SSL support, EXPLAIN row estimates |
| MySQL | Done | No | go-sql-driver, SSL support, EXPLAIN row estimates |
| SQLite | Done | No | Pure Go (modernc.org), no CGO needed |
| ClickHouse | Planned | Yes | Coming soon |
| MongoDB | Planned | No | JSON query mode |

## Code Repository Scanning

Link local code repositories when setting up a connection. The scanner:

1. **Extracts SQL patterns** from Go, Python, TypeScript, Java, Ruby, and other code files
2. **Detects database connections** in `.env` files (connection strings, `DATABASE_URL`, etc.)
3. **Finds Docker databases** in `docker-compose.yml` (postgres, mysql, mongo, redis, clickhouse)
4. **Injects context into AI** — the assistant sees your actual query patterns, ORM models, and migrations

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
