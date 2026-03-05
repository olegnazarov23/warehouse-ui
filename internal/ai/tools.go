package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"warehouse-ui/internal/logger"
)

// ToolActivity wraps a tool progress message in a marker tag.
// The frontend detects these and shows them as temporary status,
// then strips them from the final response.
func ToolActivity(msg string) string {
	return fmt.Sprintf("<tool-activity>%s</tool-activity>", msg)
}

// ToolActivityMsg creates a verbose progress message for a tool call.
func ToolActivityMsg(toolName string, input map[string]any) string {
	switch toolName {
	case "search_files":
		pattern, _ := input["pattern"].(string)
		return ToolActivity(fmt.Sprintf("🔍 Searching files: `%s`", pattern))
	case "read_file":
		path, _ := input["path"].(string)
		return ToolActivity(fmt.Sprintf("📄 Reading: `%s`", path))
	case "grep_code":
		pattern, _ := input["pattern"].(string)
		fileGlob, _ := input["file_glob"].(string)
		if fileGlob != "" {
			return ToolActivity(fmt.Sprintf("🔎 Grep: `%s` in `%s`", pattern, fileGlob))
		}
		return ToolActivity(fmt.Sprintf("🔎 Grep: `%s`", pattern))
	case "run_query":
		query, _ := input["query"].(string)
		if len(query) > 80 {
			query = query[:80] + "..."
		}
		return ToolActivity(fmt.Sprintf("▶️ Running: `%s`", query))
	case "explain_query":
		query, _ := input["query"].(string)
		if len(query) > 80 {
			query = query[:80] + "..."
		}
		return ToolActivity(fmt.Sprintf("📊 Explaining: `%s`", query))
	default:
		return ToolActivity(fmt.Sprintf("⚙️ %s", toolName))
	}
}

// CodeTool defines a tool the AI can call.
type CodeTool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// CodeTools returns the tool definitions for codebase exploration.
func CodeTools() []CodeTool {
	return []CodeTool{
		{
			Name:        "search_files",
			Description: "Search for files by name pattern in the linked code repository. Use glob patterns like '*.py', '*model*', 'libs/**/*.py'. Returns matching file paths.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "Glob pattern to match file names/paths (e.g. '*model*', '**/*.py', 'libs/models/*.py')",
					},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "read_file",
			Description: "Read the full contents of a file from the linked code repository. Use this after search_files to read interesting files.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Relative file path within the repository (e.g. 'libs/models/offer.py')",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "grep_code",
			Description: "Search for a regex pattern in file contents across the linked code repository. Returns matching lines with file path and line numbers. Use to find where a class, function, variable, or concept is used.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{
						"type":        "string",
						"description": "Regex pattern to search for in file contents (e.g. 'class Campaign', 'TriggeredEmail', 'offer_id')",
					},
					"file_glob": map[string]any{
						"type":        "string",
						"description": "Optional glob to filter which files to search (e.g. '*.py', 'libs/**/*.py'). Searches all code files if empty.",
					},
				},
				"required": []string{"pattern"},
			},
		},
	}
}

// DataTools returns tool definitions for database analysis.
func DataTools() []CodeTool {
	return []CodeTool{
		{
			Name:        "run_query",
			Description: "Execute a SQL query against the connected database and return the results. Use this to verify queries, explore data, do multi-step analysis, or check if your query is correct before presenting it. Returns columns and rows as a formatted table. Always use LIMIT to keep results small.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The SQL query to execute. Always include LIMIT (max 50 rows for tool calls).",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "explain_query",
			Description: "Run EXPLAIN/dry-run on a query to check its execution plan, estimated cost, rows, and referenced tables without actually executing it. Use this before running expensive queries or when optimizing query performance.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The SQL query to analyze.",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

// ExecuteTool runs a tool and returns the result as a string.
func ExecuteTool(name string, input map[string]any, codePaths []string) (string, error) {
	logger.Info("ai tool: %s input=%v", name, input)

	switch name {
	case "search_files":
		if len(codePaths) == 0 {
			return "", fmt.Errorf("no code repositories linked")
		}
		pattern, _ := input["pattern"].(string)
		if pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}
		return toolSearchFiles(pattern, codePaths)
	case "read_file":
		if len(codePaths) == 0 {
			return "", fmt.Errorf("no code repositories linked")
		}
		path, _ := input["path"].(string)
		if path == "" {
			return "", fmt.Errorf("path is required")
		}
		return toolReadFile(path, codePaths)
	case "grep_code":
		if len(codePaths) == 0 {
			return "", fmt.Errorf("no code repositories linked")
		}
		pattern, _ := input["pattern"].(string)
		if pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}
		fileGlob, _ := input["file_glob"].(string)
		return toolGrepCode(pattern, fileGlob, codePaths)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// ExecuteDataTool runs a data analysis tool using the provided query functions.
func ExecuteDataTool(ctx context.Context, name string, input map[string]any, schema *SchemaContext) (string, error) {
	logger.Info("ai data tool: %s input=%v", name, input)

	switch name {
	case "run_query":
		if schema.RunQuery == nil {
			return "", fmt.Errorf("query execution not available")
		}
		query, _ := input["query"].(string)
		if query == "" {
			return "", fmt.Errorf("query is required")
		}
		return toolRunQuery(ctx, query, schema)
	case "explain_query":
		if schema.DryRun == nil {
			return "", fmt.Errorf("explain/dry-run not available")
		}
		query, _ := input["query"].(string)
		if query == "" {
			return "", fmt.Errorf("query is required")
		}
		return toolExplainQuery(ctx, query, schema)
	default:
		return "", fmt.Errorf("unknown data tool: %s", name)
	}
}

// IsDataTool returns true if the tool name is a data analysis tool.
func IsDataTool(name string) bool {
	return name == "run_query" || name == "explain_query"
}

// toolRunQuery executes a query and formats results as a text table.
func toolRunQuery(ctx context.Context, query string, schema *SchemaContext) (string, error) {
	// Force a reasonable limit for tool calls
	const maxRows = 50
	columns, rows, rowCount, durationMs, err := schema.RunQuery(ctx, query, maxRows)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Query executed in %dms, %d rows returned:\n\n", durationMs, rowCount))

	if len(columns) == 0 {
		sb.WriteString("(no columns returned)\n")
		return sb.String(), nil
	}

	// Calculate column widths
	widths := make([]int, len(columns))
	for i, col := range columns {
		widths[i] = len(col)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				if len(cell) > 60 {
					widths[i] = 60
				} else {
					widths[i] = len(cell)
				}
			}
		}
	}

	// Header
	for i, col := range columns {
		if i > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(fmt.Sprintf("%-*s", widths[i], col))
	}
	sb.WriteString("\n")

	// Separator
	for i, w := range widths {
		if i > 0 {
			sb.WriteString("-+-")
		}
		sb.WriteString(strings.Repeat("-", w))
	}
	sb.WriteString("\n")

	// Rows
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				sb.WriteString(" | ")
			}
			display := cell
			if len(display) > 60 {
				display = display[:57] + "..."
			}
			if i < len(widths) {
				sb.WriteString(fmt.Sprintf("%-*s", widths[i], display))
			} else {
				sb.WriteString(display)
			}
		}
		sb.WriteString("\n")
	}

	if rowCount > int64(len(rows)) {
		sb.WriteString(fmt.Sprintf("\n(%d more rows not shown)\n", rowCount-int64(len(rows))))
	}

	return sb.String(), nil
}

// toolExplainQuery runs dry-run/EXPLAIN and returns the analysis.
func toolExplainQuery(ctx context.Context, query string, schema *SchemaContext) (string, error) {
	summary, err := schema.DryRun(ctx, query)
	if err != nil {
		return "", err
	}
	return summary, nil
}

// toolSearchFiles finds files matching a glob pattern within linked repos.
func toolSearchFiles(pattern string, codePaths []string) (string, error) {
	var matches []string
	const maxResults = 50

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, "__pycache__": true,
		"dist": true, "build": true, "target": true, ".next": true,
		"venv": true, ".venv": true,
	}

	for _, root := range codePaths {
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || len(matches) >= maxResults {
				return nil
			}
			if info.IsDir() {
				if skipDirs[info.Name()] || strings.HasPrefix(info.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}

			relPath, _ := filepath.Rel(root, path)
			matched, _ := filepath.Match(pattern, info.Name())
			if !matched {
				matched, _ = filepath.Match(pattern, relPath)
			}
			if !matched && strings.Contains(pattern, "/") {
				matched = strings.Contains(strings.ToLower(relPath), strings.ToLower(strings.ReplaceAll(pattern, "*", "")))
			}
			if !matched {
				lowerName := strings.ToLower(info.Name())
				lowerPattern := strings.ToLower(strings.ReplaceAll(pattern, "*", ""))
				if lowerPattern != "" && strings.Contains(lowerName, lowerPattern) {
					matched = true
				}
			}
			if matched {
				matches = append(matches, relPath)
			}
			return nil
		})
	}

	if len(matches) == 0 {
		return "No files found matching pattern: " + pattern, nil
	}

	result := fmt.Sprintf("Found %d files:\n", len(matches))
	for _, m := range matches {
		result += m + "\n"
	}
	return result, nil
}

// toolReadFile reads a file's contents, validating it's within linked repos.
func toolReadFile(path string, codePaths []string) (string, error) {
	path = filepath.Clean(path)
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("path traversal not allowed")
	}

	for _, root := range codePaths {
		fullPath := filepath.Join(root, path)

		absRoot, _ := filepath.Abs(root)
		absPath, _ := filepath.Abs(fullPath)
		if !strings.HasPrefix(absPath, absRoot) {
			continue
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		content := string(data)
		if len(content) > 100*1024 {
			content = content[:100*1024] + "\n... (truncated at 100KB)"
		}

		return fmt.Sprintf("File: %s (%d bytes)\n\n%s", path, len(data), content), nil
	}

	return "", fmt.Errorf("file not found: %s", path)
}

// toolGrepCode searches file contents for a regex pattern.
func toolGrepCode(pattern, fileGlob string, codePaths []string) (string, error) {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	codeExts := map[string]bool{
		".go": true, ".py": true, ".ts": true, ".tsx": true,
		".js": true, ".jsx": true, ".rb": true, ".java": true,
		".cs": true, ".rs": true, ".php": true, ".sql": true,
		".yaml": true, ".yml": true, ".json": true, ".toml": true,
		".graphql": true, ".prisma": true, ".proto": true,
	}

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, "__pycache__": true,
		"dist": true, "build": true, "target": true, ".next": true,
		"venv": true, ".venv": true,
	}

	type match struct {
		file    string
		lineNum int
		line    string
	}

	var matches []match
	const maxMatches = 50
	const contextLines = 2

	for _, root := range codePaths {
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || len(matches) >= maxMatches {
				return nil
			}
			if info.IsDir() {
				if skipDirs[info.Name()] || strings.HasPrefix(info.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}

			ext := strings.ToLower(filepath.Ext(info.Name()))
			if !codeExts[ext] {
				return nil
			}

			if fileGlob != "" {
				matched, _ := filepath.Match(fileGlob, info.Name())
				if !matched {
					return nil
				}
			}

			if info.Size() > 100*1024 {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			relPath, _ := filepath.Rel(root, path)
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				if len(matches) >= maxMatches {
					break
				}
				if re.MatchString(line) {
					start := i - contextLines
					if start < 0 {
						start = 0
					}
					end := i + contextLines + 1
					if end > len(lines) {
						end = len(lines)
					}
					context := strings.Join(lines[start:end], "\n")
					matches = append(matches, match{
						file:    relPath,
						lineNum: i + 1,
						line:    context,
					})
				}
			}
			return nil
		})
	}

	if len(matches) == 0 {
		return "No matches found for pattern: " + pattern, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d matches for '%s':\n\n", len(matches), pattern))
	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("**%s:%d**:\n```\n%s\n```\n\n", m.file, m.lineNum, m.line))
	}
	return sb.String(), nil
}
