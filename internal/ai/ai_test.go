package ai

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt_Empty(t *testing.T) {
	prompt := BuildSystemPrompt(nil)
	if !strings.Contains(prompt, "senior data engineer") {
		t.Error("expected system prompt to contain 'senior data engineer'")
	}
	if strings.Contains(prompt, "## Connected Database") {
		t.Error("should not contain database section when schema is nil")
	}
}

func TestBuildSystemPrompt_WithSchema(t *testing.T) {
	schema := &SchemaContext{
		DriverType: "postgres",
		Database:   "mydb",
		Tables: map[string][]SchemaColumn{
			"users": {
				{Name: "id", Type: "INT"},
				{Name: "name", Type: "VARCHAR"},
			},
			"orders": {
				{Name: "id", Type: "INT"},
				{Name: "user_id", Type: "INT"},
			},
		},
	}

	prompt := BuildSystemPrompt(schema)

	if !strings.Contains(prompt, "Driver: postgres") {
		t.Error("expected driver type in prompt")
	}
	if !strings.Contains(prompt, "Database: mydb") {
		t.Error("expected database name in prompt")
	}
	if !strings.Contains(prompt, "users:") {
		t.Error("expected users table in prompt")
	}
	if !strings.Contains(prompt, "orders:") {
		t.Error("expected orders table in prompt")
	}
}

func TestBuildSystemPrompt_WithCodeSnippets(t *testing.T) {
	schema := &SchemaContext{
		DriverType: "postgres",
		Tables:     map[string][]SchemaColumn{"t": {{Name: "id", Type: "INT"}}},
		CodeSnippets: []CodeSnippetRef{
			{FilePath: "app.py", Language: "python", Content: "SELECT * FROM users", LineNum: 42},
		},
	}

	prompt := BuildSystemPrompt(schema)
	if !strings.Contains(prompt, "Codebase Context") {
		t.Error("expected Codebase Context section")
	}
	if !strings.Contains(prompt, "app.py") {
		t.Error("expected file path in prompt")
	}
}

func TestBuildSystemPrompt_WithDetectedConnections(t *testing.T) {
	schema := &SchemaContext{
		DriverType: "postgres",
		Tables:     map[string][]SchemaColumn{"t": {{Name: "id", Type: "INT"}}},
		DetectedConnections: []DetectedConnectionRef{
			{Source: ".env", DriverHint: "mysql", Detail: "mysql://user:***@host/db"},
		},
	}

	prompt := BuildSystemPrompt(schema)
	if !strings.Contains(prompt, "Other Databases Detected") {
		t.Error("expected detected connections section")
	}
	if !strings.Contains(prompt, "mysql") {
		t.Error("expected mysql in prompt")
	}
}

func TestExtractSQL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard sql block",
			input:    "Here's the query:\n```sql\nSELECT * FROM users;\n```\nDone.",
			expected: "SELECT * FROM users;",
		},
		{
			name:     "SQL uppercase",
			input:    "```SQL\nSELECT 1;\n```",
			expected: "SELECT 1;",
		},
		{
			name:     "no sql block",
			input:    "Just some text without SQL",
			expected: "",
		},
		{
			name:     "multiline query",
			input:    "```sql\nSELECT\n  id,\n  name\nFROM users\nLIMIT 10;\n```",
			expected: "SELECT\n  id,\n  name\nFROM users\nLIMIT 10;",
		},
		{
			name:     "unclosed block",
			input:    "```sql\nSELECT * FROM users",
			expected: "SELECT * FROM users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSQL(tt.input)
			if got != tt.expected {
				t.Errorf("ExtractSQL() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestAvailableProviders(t *testing.T) {
	providers := AvailableProviders()
	if len(providers) == 0 {
		t.Error("expected at least one registered provider")
	}

	// Check known providers are registered (from init() functions)
	found := map[string]bool{}
	for _, p := range providers {
		found[p] = true
	}
	for _, expected := range []string{"openai", "anthropic", "ollama"} {
		if !found[expected] {
			t.Errorf("expected provider %s to be registered", expected)
		}
	}
}

func TestNewProvider_Unknown(t *testing.T) {
	_, err := New("nonexistent", "", "")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown AI provider") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewProvider_Valid(t *testing.T) {
	p, err := New("openai", "test-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("expected openai, got %s", p.Name())
	}
	if !p.IsConfigured() {
		t.Error("expected IsConfigured=true with API key")
	}
}

func TestProviderDefaults(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		defaultModel string
	}{
		{"openai", "openai", "gpt-4o"},
		{"anthropic", "anthropic", "claude-sonnet-4-20250514"},
		{"ollama", "ollama", "llama3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.provider, "key", "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.DefaultModel() != tt.defaultModel {
				t.Errorf("expected default model %s, got %s", tt.defaultModel, p.DefaultModel())
			}
			if p.MinModel() == "" {
				t.Error("expected non-empty MinModel")
			}
		})
	}
}
