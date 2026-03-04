/**
 * Static SQL analyzer that suggests common query improvements.
 * No AI required — pattern-based detection of anti-patterns.
 */

export interface SqlHint {
  level: "warning" | "info" | "perf";
  message: string;
  suggestion: string;
}

export function analyzeSql(sql: string, driverType?: string): SqlHint[] {
  const hints: SqlHint[] = [];
  const upper = sql.toUpperCase();
  const trimmed = upper.replace(/\s+/g, " ").trim();

  // SELECT * — suggest explicit columns
  if (/SELECT\s+\*\s+FROM/i.test(sql)) {
    hints.push({
      level: "perf",
      message: "SELECT * fetches all columns",
      suggestion: "List only the columns you need to reduce data scanned and improve performance.",
    });
  }

  // Missing LIMIT on SELECT
  if (/^\s*(WITH|SELECT)/i.test(sql) && !/\bLIMIT\b/i.test(sql) && !/\bTOP\b/i.test(sql)) {
    hints.push({
      level: "info",
      message: "No LIMIT clause",
      suggestion: "Add LIMIT to prevent accidentally fetching millions of rows.",
    });
  }

  // SELECT DISTINCT without need (heuristic: no JOIN)
  if (/SELECT\s+DISTINCT\b/i.test(sql) && !/\bJOIN\b/i.test(sql) && !/\bUNION\b/i.test(sql)) {
    hints.push({
      level: "info",
      message: "DISTINCT without JOIN",
      suggestion: "DISTINCT adds sorting overhead. If the source table has unique rows, you can remove it.",
    });
  }

  // OR in WHERE that could be IN()
  if (/WHERE\b.*?\b(\w+)\s*=\s*'[^']*'\s+OR\s+\1\s*=\s*'[^']*'/i.test(sql)) {
    hints.push({
      level: "perf",
      message: "Multiple OR conditions on same column",
      suggestion: "Use IN(...) instead of multiple OR conditions for better readability and optimization.",
    });
  }

  // LIKE with leading wildcard
  if (/LIKE\s+'%/i.test(sql)) {
    hints.push({
      level: "perf",
      message: "LIKE with leading wildcard (%...)",
      suggestion: "Leading wildcards prevent index usage. Consider full-text search or restructuring the query.",
    });
  }

  // Functions on indexed columns in WHERE
  if (/WHERE\b.*?\b(UPPER|LOWER|TRIM|CAST|DATE|EXTRACT|YEAR|MONTH)\s*\(/i.test(sql)) {
    hints.push({
      level: "perf",
      message: "Function applied to column in WHERE clause",
      suggestion: "Wrapping columns in functions prevents index usage. Try computed columns or pre-filtered values.",
    });
  }

  // Implicit cross join (FROM a, b)
  if (/FROM\s+\w+\s*,\s*\w+/i.test(sql) && !/\bJOIN\b/i.test(sql)) {
    hints.push({
      level: "warning",
      message: "Implicit cross join detected (comma-separated tables)",
      suggestion: "Use explicit JOIN syntax for clarity and to avoid accidental cartesian products.",
    });
  }

  // ORDER BY without LIMIT
  if (/\bORDER\s+BY\b/i.test(sql) && !/\bLIMIT\b/i.test(sql) && !/\bTOP\b/i.test(sql)) {
    hints.push({
      level: "perf",
      message: "ORDER BY without LIMIT",
      suggestion: "Sorting all rows is expensive. Add LIMIT if you only need the top/bottom results.",
    });
  }

  // NOT IN with subquery (null-unsafe)
  if (/\bNOT\s+IN\s*\(\s*SELECT\b/i.test(sql)) {
    hints.push({
      level: "warning",
      message: "NOT IN with subquery",
      suggestion: "NOT IN returns no rows if the subquery contains NULL. Use NOT EXISTS or LEFT JOIN ... IS NULL instead.",
    });
  }

  // COUNT(*) with GROUP BY but no HAVING — might want HAVING
  // (Skip this, too noisy)

  // BigQuery-specific hints
  if (driverType === "bigquery") {
    // Missing partition filter
    if (/\bFROM\b/i.test(sql) && !/\bWHERE\b/i.test(sql)) {
      hints.push({
        level: "warning",
        message: "No WHERE clause on BigQuery query",
        suggestion: "BigQuery charges by data scanned. Add partition filters (date ranges) to reduce cost.",
      });
    }

    // Using != instead of <> (both work, but noting BigQuery full scan risk)
    if (/\b_PARTITIONTIME\b/i.test(sql) || /\b_PARTITIONDATE\b/i.test(sql)) {
      // Good — they're using partition columns
    } else if (driverType === "bigquery" && /\bFROM\b/i.test(sql) && !/\bLIMIT\s+\d/i.test(sql)) {
      // Only add if there's no partition hint already
    }

    // Non-backtick-quoted table names
    if (/FROM\s+(?!`)[a-z0-9_-]+\.[a-z0-9_-]+\.[a-z0-9_-]+/i.test(sql)) {
      hints.push({
        level: "info",
        message: "Unquoted BigQuery table reference",
        suggestion: "Use backtick-quoted table names (`project.dataset.table`) to avoid parsing issues with hyphens.",
      });
    }
  }

  // Nested subquery that could be CTE
  const subqueryCount = (sql.match(/\(\s*SELECT\b/gi) || []).length;
  if (subqueryCount >= 2) {
    hints.push({
      level: "info",
      message: `${subqueryCount} nested subqueries detected`,
      suggestion: "Consider using CTEs (WITH ... AS) to improve readability and avoid repeated computation.",
    });
  }

  // Very long query (might benefit from formatting)
  if (sql.length > 500 && !/\n/.test(sql.trim())) {
    hints.push({
      level: "info",
      message: "Query is on a single line",
      suggestion: "Use the Format button to break the query into readable lines.",
    });
  }

  return hints;
}
