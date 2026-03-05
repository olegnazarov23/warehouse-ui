#!/bin/bash
# End-to-end CLI test suite for warehouse-ui
# Tests all CLI commands against an in-memory SQLite database.
# Usage: ./test_cli_e2e.sh [path-to-binary]

set -euo pipefail

BIN="${1:-./build/bin/warehouse-ui}"
DB="/tmp/warehouse-ui-e2e-test.db"
PASS=0
FAIL=0
TESTS=()

cleanup() {
    rm -f "$DB"
}
trap cleanup EXIT

# --- Helpers ---

pass() {
    PASS=$((PASS + 1))
    TESTS+=("PASS: $1")
    echo "  ✓ $1"
}

fail() {
    FAIL=$((FAIL + 1))
    TESTS+=("FAIL: $1 — $2")
    echo "  ✗ $1: $2"
}

run() {
    "$BIN" "$@" 2>/dev/null
}

assert_json_field() {
    local json="$1" field="$2" expected="$3" test_name="$4"
    local actual
    actual=$(echo "$json" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('$field',''))" 2>/dev/null || echo "PARSE_ERROR")
    if [ "$actual" = "$expected" ]; then
        pass "$test_name"
    else
        fail "$test_name" "expected '$expected', got '$actual'"
    fi
}

assert_contains() {
    local output="$1" expected="$2" test_name="$3"
    if echo "$output" | grep -q "$expected"; then
        pass "$test_name"
    else
        fail "$test_name" "output does not contain '$expected'"
    fi
}

assert_exit_code() {
    local expected="$1" test_name="$2"
    shift 2
    set +e
    "$BIN" "$@" >/dev/null 2>&1
    local code=$?
    set -e
    if [ "$code" -eq "$expected" ]; then
        pass "$test_name"
    else
        fail "$test_name" "expected exit code $expected, got $code"
    fi
}

# --- Tests ---

echo ""
echo "=== Warehouse UI CLI E2E Tests ==="
echo "Binary: $BIN"
echo ""

# 1. Version
echo "--- version ---"
OUT=$(run version)
assert_json_field "$OUT" "name" "warehouse-ui" "version: name field"
assert_json_field "$OUT" "version" "$(echo "$OUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['version'])")" "version: has version field"

# 2. Connect to SQLite
echo "--- connect ---"
OUT=$(run connect --type sqlite --database "$DB" --name "e2e-test")
assert_json_field "$OUT" "connected" "True" "connect: connected=true"
assert_json_field "$OUT" "driver_type" "sqlite" "connect: driver_type=sqlite"

# 3. Status
echo "--- status ---"
OUT=$(run status)
assert_json_field "$OUT" "connected" "True" "status: connected after connect"

# 4. Query — create table
echo "--- query: create table ---"
OUT=$(run query "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT, age INTEGER)")
assert_json_field "$OUT" "row_count" "0" "query: CREATE TABLE returns 0 rows"

# 5. Query — insert data
echo "--- query: insert data ---"
run query "INSERT INTO users VALUES (1, 'Alice', 'alice@test.com', 30)" >/dev/null
run query "INSERT INTO users VALUES (2, 'Bob', 'bob@test.com', 25)" >/dev/null
run query "INSERT INTO users VALUES (3, 'Charlie', 'charlie@test.com', 35)" >/dev/null
pass "query: INSERT 3 rows"

# 6. Query — select
echo "--- query: select ---"
OUT=$(run query "SELECT * FROM users ORDER BY id")
assert_json_field "$OUT" "row_count" "3" "query: SELECT returns 3 rows"
assert_contains "$OUT" "Alice" "query: result contains Alice"

# 7. Query with --limit
echo "--- query: --limit ---"
OUT=$(run query --limit 1 "SELECT * FROM users")
assert_json_field "$OUT" "row_count" "1" "query: --limit 1 returns 1 row"

# 8. Query with --file
echo "--- query: --file ---"
echo "SELECT count(*) as cnt FROM users" > /tmp/warehouse-ui-test.sql
OUT=$(run query --file /tmp/warehouse-ui-test.sql)
assert_contains "$OUT" "cnt" "query: --file works"
rm -f /tmp/warehouse-ui-test.sql

# 9. Schema — list tables
echo "--- schema ---"
OUT=$(run schema list-tables)
assert_contains "$OUT" "users" "schema: list-tables shows users table"

# 10. Schema — describe
OUT=$(run schema describe users)
assert_contains "$OUT" "name" "schema: describe shows column name"
assert_contains "$OUT" "email" "schema: describe shows email column"

# 11. Dry run
echo "--- dry-run ---"
OUT=$(run dry-run "SELECT * FROM users")
assert_json_field "$OUT" "valid" "True" "dry-run: valid=true"

# 12. Connections list
echo "--- connections ---"
OUT=$(run connections)
assert_contains "$OUT" "e2e-test" "connections: shows saved connection"

# 13. History
echo "--- history ---"
OUT=$(run history --limit 5)
assert_contains "$OUT" "SELECT" "history: shows recent queries"

# 14. --format table
echo "--- --format table ---"
OUT=$(run query --format table "SELECT * FROM users ORDER BY id")
assert_contains "$OUT" "Alice" "format table: query output has Alice"
assert_contains "$OUT" "rows" "format table: shows row count"

OUT=$(run connections --format table)
assert_contains "$OUT" "e2e-test" "format table: connections shows name"

OUT=$(run history --format table --limit 3)
assert_contains "$OUT" "SELECT" "format table: history shows queries"

OUT=$(run schema list-tables --format table)
assert_contains "$OUT" "users" "format table: schema list-tables shows users"

# 15. DATABASE_URL env var
echo "--- DATABASE_URL ---"
run disconnect >/dev/null 2>&1
OUT=$(DATABASE_URL="sqlite://$DB" run query "SELECT count(*) as cnt FROM users")
assert_contains "$OUT" "cnt" "DATABASE_URL: auto-connect works"

# 16. MCP server (basic handshake)
echo "--- mcp ---"
MCP_OUT=$(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
{"jsonrpc":"2.0","id":3,"method":"ping","params":{}}' | DATABASE_URL="sqlite://$DB" "$BIN" mcp 2>/dev/null)
assert_contains "$MCP_OUT" "warehouse-ui" "mcp: initialize returns server info"
assert_contains "$MCP_OUT" "execute_query" "mcp: tools/list includes execute_query"
assert_contains "$MCP_OUT" "describe_table" "mcp: tools/list includes describe_table"

# 17. MCP tool calls
echo "--- mcp tool calls ---"
MCP_OUT=$(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"execute_query","arguments":{"sql":"SELECT * FROM users ORDER BY id","limit":10}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_tables","arguments":{}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"describe_table","arguments":{"table":"users"}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"status","arguments":{}}}' | DATABASE_URL="sqlite://$DB" "$BIN" mcp 2>/dev/null)
assert_contains "$MCP_OUT" "Alice" "mcp: execute_query returns data"
assert_contains "$MCP_OUT" "users" "mcp: list_tables shows users"
assert_contains "$MCP_OUT" "email" "mcp: describe_table shows columns"
assert_contains "$MCP_OUT" "connected" "mcp: status returns connected"

# 18. Error cases
echo "--- error cases ---"
assert_exit_code 1 "error: query without connection" query "SELECT 1"
assert_exit_code 1 "error: unknown command" badcommand

# 19. Disconnect
echo "--- disconnect ---"
run connect --type sqlite --database "$DB" >/dev/null
OUT=$(run disconnect)
assert_json_field "$OUT" "status" "disconnected" "disconnect: status=disconnected"

OUT=$(run status)
assert_json_field "$OUT" "connected" "False" "status: disconnected after disconnect"

# --- Summary ---
echo ""
echo "==================================="
echo "Results: $PASS passed, $FAIL failed"
echo "==================================="

if [ "$FAIL" -gt 0 ]; then
    echo ""
    echo "Failed tests:"
    for t in "${TESTS[@]}"; do
        if [[ "$t" == FAIL* ]]; then
            echo "  $t"
        fi
    done
    exit 1
fi

echo ""
echo "All tests passed!"
exit 0
