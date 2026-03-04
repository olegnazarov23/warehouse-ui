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

// SchemaContext is a compact representation of the connected database schema
// that gets injected into the AI system prompt.
type SchemaContext struct {
	DriverType           string                    `json:"driver_type"`
	Database             string                    `json:"database"`
	Tables               map[string][]SchemaColumn `json:"tables"`                  // table_name -> columns
	CodeSnippets         []CodeSnippetRef          `json:"code_snippets"`           // relevant code from user repos
	DetectedConnections  []DetectedConnectionRef   `json:"detected_connections"`    // DB connections found in code
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

	sb.WriteString(`You are an expert data analyst AI built into Warehouse UI, a database IDE. You are the user's secret weapon — a cheat code that turns questions into answers instantly.

Your style:
- Be conversational and human. Talk through your reasoning step by step like a senior analyst explaining to a colleague.
- When answering a question, first explain your APPROACH ("Let me look at this by..."), then show the SQL, then explain what it does and what to watch for.
- Use natural language headers to organize your response (not markdown headers, just bold text or clear structure).
- When you write SQL, wrap it in triple-backtick sql code blocks so it can be inserted into the editor.
- After showing a query, suggest what to look for in the results and offer 2-3 follow-up questions.

Your capabilities:
1. Generate correct, efficient SQL from natural language questions
2. Explain existing queries in plain English
3. Debug query errors — explain what went wrong and fix it
4. Suggest optimizations for slow queries
5. Help explore data: "What's interesting in this table?" → generate exploratory queries
6. Cross-reference tables: help users join and correlate data

Technical rules:
- Use exact table and column names from the schema below
- For BigQuery: use backtick-quoted table names (` + "`project.dataset.table`" + `), remember value_usd may be in cents
- For PostgreSQL/MySQL: use standard SQL conventions
- For MongoDB: output JSON aggregation pipelines
- When a query might scan large data or cost money, warn proactively with an estimate
- If you're unsure about the schema, say so rather than guessing
- Always consider performance: use filters, LIMIT, partition columns when possible

STRICT ANTI-HALLUCINATION RULES — NEVER VIOLATE THESE:
- ONLY reference tables and columns that exist in the schema provided below. If a table or column is not listed, DO NOT use it.
- If the user asks about data that doesn't match any table in the schema, say "I don't see a table that would contain that data" — do NOT invent one.
- NEVER fabricate SQL functions that don't exist in the target database dialect.
- When showing query results or estimates, NEVER invent numbers or statistics. Only reference actual dry-run results or data the user has shared.
- If you're unsure whether a column exists or what type it is, explicitly say so. Do NOT guess.
- NEVER claim a query will return specific results (e.g. "this will show 1,234 rows") — you don't know the data.
- If a query requires information you don't have (like specific date ranges or filter values), ask the user rather than assuming.

Response format — ALWAYS follow this structure:
1. Brief approach explanation (1-2 sentences about how you'll tackle this)
2. The SQL query in a code block
3. What to look for in the results (key metrics, patterns, anomalies)
4. End with 2-3 smart follow-up questions the user might want to explore next, formatted as a short bulleted list

Example follow-up questions style:
- "Want to break this down by month to see the trend?"
- "Should I look at how this compares to the previous period?"
- "Curious which specific records are driving that number?"
`)

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

	// Include code context from user repos if available
	if schema != nil && len(schema.CodeSnippets) > 0 {
		sb.WriteString("\n## Codebase Context\n")
		sb.WriteString("The user has linked code repositories. Below are SQL-related code snippets showing how this database is used in their codebase. Use this to understand naming conventions, common query patterns, and existing logic:\n\n")

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
