package ai

import (
	"context"
	"fmt"
	"strings"
)

// Message represents a single chat message.
type Message struct {
	Role    string `json:"role"`    // "user", "assistant", "system"
	Content string `json:"content"`
}

// QueryFunc executes a query and returns columns, rows (as string slices), row count, and duration.
type QueryFunc func(ctx context.Context, query string, limit int) (columns []string, rows [][]string, rowCount int64, durationMs int64, err error)

// DryRunFunc runs a dry-run/EXPLAIN and returns a summary string.
type DryRunFunc func(ctx context.Context, query string) (summary string, err error)

// SchemaContext is a compact representation of the connected database schema
// that gets injected into the AI system prompt.
type SchemaContext struct {
	DriverType           string                    `json:"driver_type"`
	Database             string                    `json:"database"`
	Tables               map[string][]SchemaColumn `json:"tables"`                  // table_name -> columns
	FullFiles            []CodeSnippetRef          `json:"full_files"`              // full file contents from linked repos
	CodeSnippets         []CodeSnippetRef          `json:"code_snippets"`           // relevant code from user repos
	DetectedConnections  []DetectedConnectionRef   `json:"detected_connections"`    // DB connections found in code
	CodePaths            []string                  `json:"-"`                       // linked repo paths for tool execution
	RunQuery             QueryFunc                 `json:"-"`                       // execute a query against the DB
	DryRun               DryRunFunc                `json:"-"`                       // dry-run/EXPLAIN a query
}

// CodeSnippetRef is a code fragment for AI context.
type CodeSnippetRef struct {
	FilePath string `json:"file_path"`
	Language string `json:"language"`
	Content  string `json:"content"`
	LineNum  int    `json:"line_num"`
}

// DetectedConnectionRef is a DB connection found in the user's code.
type DetectedConnectionRef struct {
	Source     string `json:"source"`
	DriverHint string `json:"driver_hint"`
	Detail     string `json:"detail"`
}

// SchemaColumn is a minimal column descriptor for the AI prompt.
type SchemaColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Provider is the interface for AI/LLM backends.
type Provider interface {
	Name() string
	DefaultModel() string
	MinModel() string // minimum recommended model version
	IsConfigured() bool
	// StreamChat sends messages and calls onChunk for each token. Blocking.
	StreamChat(ctx context.Context, messages []Message, schema *SchemaContext, model string, onChunk func(string)) error
	// Complete is a non-streaming variant. Returns full response.
	Complete(ctx context.Context, messages []Message, schema *SchemaContext, model string) (string, error)
}

// ProviderInfo describes an available AI provider for the UI.
type ProviderInfo struct {
	Name         string `json:"name"`
	DefaultModel string `json:"default_model"`
	MinModel     string `json:"min_model"`
	Configured   bool   `json:"configured"`
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

type ProviderFactory func(apiKey, endpoint string) Provider

var registry = map[string]ProviderFactory{}

// Register adds a provider factory.
func Register(name string, f ProviderFactory) {
	registry[name] = f
}

// New creates a provider instance.
func New(name, apiKey, endpoint string) (Provider, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown AI provider: %s (available: %v)", name, AvailableProviders())
	}
	return f(apiKey, endpoint), nil
}

// AvailableProviders returns registered provider names.
func AvailableProviders() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// System Prompt Builder
// ---------------------------------------------------------------------------

// BuildSystemPrompt creates a schema-aware system prompt for the AI.
func BuildSystemPrompt(schema *SchemaContext) string {
	var sb strings.Builder

	sb.WriteString(`You are a senior data engineer built into Warehouse UI, a database IDE. You think independently, make reasonable assumptions from the schema, and deliver answers — not questions.

Style:
- SQL first, then a brief explanation only if the query logic isn't obvious.
- Wrap SQL in triple-backtick sql code blocks.
- Keep responses short. No filler, no preamble.
- No follow-up questions. No "let me know if..." or "would you like...". Just deliver the answer.
- Use markdown formatting for readability: **bold** for key terms, bullet points for lists, and headers (##) to separate sections when the response has multiple parts.
- Use a few emojis sparingly to make responses scannable: 🔍 for lookups/searches, 📊 for data insights, ⚠️ for warnings/caveats, ✅ for confirmations, 💡 for tips. Don't overdo it — one or two per response is enough.

Mindset:
- You can read the schema. Use it to infer what data means — column names, types, and table relationships tell you most of what you need.
- Make smart defaults: use reasonable date ranges, sensible GROUP BYs, and appropriate aggregations based on what the data looks like.
- If a request is vague ("show me something interesting"), pick the most useful analysis based on the schema and go.
- Only ask the user something if the query would be destructive (DELETE/DROP) or if the schema genuinely has no table matching what they asked about.
- You have access to the user's codebase files below as a primer. Use them to understand business concepts, entity relationships, data flows, and how the database is actually used.
- You also have tools to explore the codebase deeper: search_files (find files by name), grep_code (search file contents), and read_file (read full files). USE THESE TOOLS proactively when a user asks about domain concepts, entity relationships, or business logic. Don't guess — search the code first, read the relevant files, then write the query.
- When a user asks about a domain concept (e.g. "offers", "campaigns"), use grep_code to find where it's defined, read_file to understand the model, then trace relationships through the code before writing the query.
- You have data analyst tools: run_query (execute SQL and see results — use this to verify your queries, explore data, or do multi-step analysis), explain_query (dry-run/EXPLAIN to check cost and performance before running expensive queries).
- Be a proactive data analyst: when you write a query, consider running it with run_query to verify it works. If the user asks to optimize, use explain_query first, then iterate. Suggest follow-up queries that would give deeper insights based on the results.

Rules:
- ONLY use tables and columns from the schema below. Never invent tables, columns, or functions.
- Use the correct dialect for the connected database (BigQuery: backtick-quoted names, PostgreSQL/MySQL: standard SQL).
- Always add LIMIT unless the user asks for everything. Prefer filters and partition columns.
- If a query could be expensive, add a one-line cost warning.
`)

	// MongoDB-specific instructions
	if schema != nil && schema.DriverType == "mongodb" {
		sb.WriteString(`
MongoDB-specific rules:
- Write queries in collection.method() syntax: collection.find({}), collection.aggregate([...])
- Wrap queries in triple-backtick sql code blocks (the UI will parse them).
- Supported methods: find, aggregate, countDocuments, distinct
- find() accepts filter and optional projection: collection.find({"status": "active"}, {"name": 1, "email": 1})
- aggregate() accepts a pipeline array: collection.aggregate([{"$match": ...}, {"$group": ...}])
- Use $match early in pipelines to reduce data scanned.
- Use $project to limit fields returned.
- For date ranges use ISODate-style strings: {"created_at": {"$gte": "2024-01-01T00:00:00Z"}}
`)
	}

	if schema != nil && len(schema.Tables) > 0 {
		sb.WriteString("\n## Connected Database\n")
		sb.WriteString(fmt.Sprintf("Driver: %s\n", schema.DriverType))
		if schema.Database != "" {
			sb.WriteString(fmt.Sprintf("Database: %s\n", schema.Database))
		}
		sb.WriteString("\n## Schema\n```\n")

		for table, cols := range schema.Tables {
			colDescs := make([]string, 0, len(cols))
			for _, c := range cols {
				colDescs = append(colDescs, fmt.Sprintf("%s %s", c.Name, c.Type))
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n", table, strings.Join(colDescs, ", ")))
		}
		sb.WriteString("```\n")
	}

	// Include full codebase files if available (preferred over snippets)
	if schema != nil && len(schema.FullFiles) > 0 {
		sb.WriteString("\n## Codebase\n")
		sb.WriteString("The user has linked their code repository. Below are the source files. Use these to understand data models, entity relationships, business logic, services, and how data flows through the application.\n\n")

		for _, f := range schema.FullFiles {
			sb.WriteString(fmt.Sprintf("**%s**:\n```%s\n%s\n```\n\n", f.FilePath, f.Language, f.Content))
		}
	} else if schema != nil && len(schema.CodeSnippets) > 0 {
		// Fallback to snippets if full files not available
		sb.WriteString("\n## Codebase Context\n")
		sb.WriteString("The user has linked code repositories. Below are code snippets showing how this database is used in their codebase:\n\n")

		for _, s := range schema.CodeSnippets {
			sb.WriteString(fmt.Sprintf("**%s** (line %d):\n```%s\n%s\n```\n\n", s.FilePath, s.LineNum, s.Language, s.Content))
		}
	}

	// Include detected database connections from code
	if schema != nil && len(schema.DetectedConnections) > 0 {
		sb.WriteString("\n## Other Databases Detected in Code\n")
		sb.WriteString("The code scanner found references to other databases in the codebase. These may be useful for cross-referencing data. When relevant, suggest the user connect to these additional databases:\n\n")

		for _, dc := range schema.DetectedConnections {
			if dc.DriverHint != "" {
				sb.WriteString(fmt.Sprintf("- **%s** (%s): `%s`\n", dc.DriverHint, dc.Source, dc.Detail))
			} else {
				sb.WriteString(fmt.Sprintf("- **%s**: `%s`\n", dc.Source, dc.Detail))
			}
		}
		sb.WriteString("\nIf the user asks a question that could benefit from data in one of these other databases, suggest they add that connection too.\n")
	}

	return sb.String()
}
