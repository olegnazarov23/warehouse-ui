package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func init() {
	Register(MongoDB, func() Driver { return &MongoDBDriver{} })
}

// MongoDBDriver implements Driver for MongoDB.
type MongoDBDriver struct {
	client   *mongo.Client
	database *mongo.Database
	dbName   string
	cancel   context.CancelFunc
}

func (d *MongoDBDriver) Type() DriverType         { return MongoDB }
func (d *MongoDBDriver) SupportsCostEstimate() bool { return false }
func (d *MongoDBDriver) QueryLanguage() string     { return "mongodb" }

func (d *MongoDBDriver) Connect(ctx context.Context, cfg ConnectionConfig) error {
	uri := cfg.Options["connection_string"]
	if uri == "" {
		// Build URI from host only — credentials set via SetAuth to avoid encoding issues
		host := cfg.Host
		if host == "" {
			host = "localhost:27017"
		}
		if !strings.Contains(host, ":") {
			host += ":27017"
		}
		uri = fmt.Sprintf("mongodb://%s/", host)
	}

	clientOpts := options.Client().ApplyURI(uri)
	clientOpts.SetConnectTimeout(10 * time.Second)
	clientOpts.SetServerSelectionTimeout(5 * time.Second)

	// Set auth via API to avoid URI-encoding issues with special characters
	if cfg.Username != "" && cfg.Options["connection_string"] == "" {
		authSource := cfg.Options["auth_source"]
		if authSource == "" {
			// Default auth source to database name if set, otherwise "admin"
			if cfg.Database != "" {
				authSource = cfg.Database
			} else {
				authSource = "admin"
			}
		}
		clientOpts.SetAuth(options.Credential{
			Username:   cfg.Username,
			Password:   cfg.Password,
			AuthSource: authSource,
		})
	}

	// Set replica set if configured
	if rs := cfg.Options["replica_set"]; rs != "" {
		clientOpts.SetReplicaSet(rs)
	}

	// Use direct connection by default to prevent the driver from discovering
	// replica set members with internal hostnames unreachable from this machine
	// (common with SSH tunnels). Skip if replicaSet is set (needs topology discovery).
	hasReplicaSet := strings.Contains(uri, "replicaSet=") || cfg.Options["replica_set"] != ""
	if !hasReplicaSet && cfg.Options["direct_connection"] != "false" {
		clientOpts.SetDirect(true)
	}

	client, err := mongo.Connect(clientOpts)
	if err != nil {
		return fmt.Errorf("mongodb connect: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return fmt.Errorf("mongodb ping: %w", err)
	}

	dbName := cfg.Database
	if dbName == "" {
		dbName = "test"
	}

	d.client = client
	d.database = client.Database(dbName)
	d.dbName = dbName
	return nil
}

func (d *MongoDBDriver) Disconnect() error {
	if d.client != nil {
		ch := make(chan error, 1)
		go func() { ch <- d.client.Disconnect(context.Background()) }()
		select {
		case err := <-ch:
			return err
		case <-time.After(5 * time.Second):
			d.client = nil
			return nil
		}
	}
	return nil
}

func (d *MongoDBDriver) Ping(ctx context.Context) error {
	if d.client == nil {
		return fmt.Errorf("not connected")
	}
	ch := make(chan error, 1)
	go func() { ch <- d.client.Ping(ctx, nil) }()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		return fmt.Errorf("MongoDB ping timed out — check connection and credentials")
	}
}

func (d *MongoDBDriver) ListDatabases(ctx context.Context) ([]string, error) {
	if d.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	names, err := d.client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	return names, nil
}

func (d *MongoDBDriver) ListTables(ctx context.Context, database string) ([]TableInfo, error) {
	if d.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	db := d.database
	if database != "" && database != d.dbName {
		db = d.client.Database(database)
	}

	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	sort.Strings(names)

	var tables []TableInfo
	for _, name := range names {
		t := TableInfo{Name: name, Type: "collection"}
		// Try to get estimated document count
		count, err := db.Collection(name).EstimatedDocumentCount(ctx)
		if err == nil {
			t.RowCount = count
		}
		tables = append(tables, t)
	}
	return tables, nil
}

func (d *MongoDBDriver) GetTableSchema(ctx context.Context, database, collection string) (*TableInfo, error) {
	if d.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	db := d.database
	if database != "" && database != d.dbName {
		db = d.client.Database(database)
	}

	coll := db.Collection(collection)

	// Sample up to 100 documents to infer schema
	pipeline := bson.A{bson.D{{Key: "$sample", Value: bson.D{{Key: "size", Value: 100}}}}}
	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("sample documents: %w", err)
	}
	defer cursor.Close(ctx)

	fieldTypes := map[string]string{} // field name → BSON type name
	for cursor.Next(ctx) {
		var doc bson.D
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		for _, elem := range doc {
			if _, exists := fieldTypes[elem.Key]; !exists {
				fieldTypes[elem.Key] = bsonTypeName(elem.Value)
			}
		}
	}

	info := &TableInfo{Name: collection, Type: "collection"}

	// Sort field names, but put _id first
	var fieldNames []string
	for name := range fieldTypes {
		if name != "_id" {
			fieldNames = append(fieldNames, name)
		}
	}
	sort.Strings(fieldNames)
	if _, ok := fieldTypes["_id"]; ok {
		fieldNames = append([]string{"_id"}, fieldNames...)
	}

	for _, name := range fieldNames {
		info.Columns = append(info.Columns, Column{
			Name:      name,
			Type:      fieldTypes[name],
			Nullable:  name != "_id",
			IsPrimary: name == "_id",
		})
	}
	return info, nil
}

func (d *MongoDBDriver) PreviewTable(ctx context.Context, database, collection string, limit int) (*QueryResult, error) {
	if limit <= 0 {
		limit = 100
	}
	return d.Execute(ctx, fmt.Sprintf("%s.find({})", collection), limit)
}

func (d *MongoDBDriver) Execute(ctx context.Context, query string, limit int) (*QueryResult, error) {
	if d.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	queryCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	defer func() { d.cancel = nil }()

	collName, method, args, err := ParseMongoQuery(query)
	if err != nil {
		return nil, err
	}

	coll := d.database.Collection(collName)
	start := time.Now()

	switch method {
	case "find":
		return d.execFind(queryCtx, coll, args, limit, start)
	case "aggregate":
		return d.execAggregate(queryCtx, coll, args, limit, start)
	case "countDocuments", "count":
		return d.execCount(queryCtx, coll, args, start)
	case "distinct":
		return d.execDistinct(queryCtx, coll, args, start)
	default:
		return nil, fmt.Errorf("unsupported method: %s (supported: find, aggregate, countDocuments, distinct)", method)
	}
}

func (d *MongoDBDriver) execFind(ctx context.Context, coll *mongo.Collection, args string, limit int, start time.Time) (*QueryResult, error) {
	filter, projection := parseFindArgs(args)

	opts := options.Find()
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	if projection != nil {
		opts.SetProjection(projection)
	}

	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find: %w", err)
	}
	defer cursor.Close(ctx)

	return cursorToResult(ctx, cursor, limit, start)
}

func (d *MongoDBDriver) execAggregate(ctx context.Context, coll *mongo.Collection, args string, limit int, start time.Time) (*QueryResult, error) {
	var pipeline bson.A
	if err := bson.UnmarshalExtJSON([]byte(args), false, &pipeline); err != nil {
		return nil, fmt.Errorf("invalid aggregation pipeline: %w", err)
	}

	// Add $limit stage if not already present
	hasLimit := false
	for _, stage := range pipeline {
		if doc, ok := stage.(bson.D); ok {
			for _, elem := range doc {
				if elem.Key == "$limit" {
					hasLimit = true
				}
			}
		}
	}
	if !hasLimit && limit > 0 {
		pipeline = append(pipeline, bson.D{{Key: "$limit", Value: limit}})
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("aggregate: %w", err)
	}
	defer cursor.Close(ctx)

	return cursorToResult(ctx, cursor, limit, start)
}

func (d *MongoDBDriver) execCount(ctx context.Context, coll *mongo.Collection, args string, start time.Time) (*QueryResult, error) {
	var filter bson.D
	if args != "" {
		if err := bson.UnmarshalExtJSON([]byte(args), false, &filter); err != nil {
			return nil, fmt.Errorf("invalid filter: %w", err)
		}
	}

	count, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}

	return &QueryResult{
		Columns:    []string{"count"},
		Rows:       [][]interface{}{{count}},
		RowCount:   1,
		TotalRows:  1,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

func (d *MongoDBDriver) execDistinct(ctx context.Context, coll *mongo.Collection, args string, start time.Time) (*QueryResult, error) {
	// Parse: "fieldName", {filter}  or just  "fieldName"
	fieldName, filter := parseDistinctArgs(args)

	dr := coll.Distinct(ctx, fieldName, filter)
	if err := dr.Err(); err != nil {
		return nil, fmt.Errorf("distinct: %w", err)
	}

	var values []interface{}
	if err := dr.Decode(&values); err != nil {
		return nil, fmt.Errorf("distinct decode: %w", err)
	}

	rows := make([][]interface{}, len(values))
	for i, v := range values {
		rows[i] = []interface{}{fmt.Sprintf("%v", v)}
	}

	return &QueryResult{
		Columns:    []string{fieldName},
		Rows:       rows,
		RowCount:   int64(len(rows)),
		TotalRows:  int64(len(rows)),
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

func (d *MongoDBDriver) DryRun(_ context.Context, query string) (*DryRunResult, error) {
	_, method, _, err := ParseMongoQuery(query)
	if err != nil {
		return &DryRunResult{Valid: false, Error: err.Error()}, nil
	}
	stType := strings.ToUpper(method)
	if method == "find" {
		stType = "FIND"
	}
	return &DryRunResult{Valid: true, StatementType: stType}, nil
}

func (d *MongoDBDriver) Cancel() error {
	if d.cancel != nil {
		d.cancel()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var queryPattern = regexp.MustCompile(`^(\w+)\.(find|aggregate|countDocuments|count|distinct)\s*\(([\s\S]*)\)\s*$`)

// ParseMongoQuery parses "collection.method(args)" syntax.
func ParseMongoQuery(q string) (collection, method, args string, err error) {
	q = strings.TrimSpace(q)
	m := queryPattern.FindStringSubmatch(q)
	if m == nil {
		return "", "", "", fmt.Errorf("invalid query format. Expected: collection.method({...})\nExamples:\n  users.find({\"age\": {\"$gt\": 25}})\n  orders.aggregate([{\"$group\": {\"_id\": \"$status\", \"count\": {\"$sum\": 1}}}])\n  products.countDocuments({\"active\": true})\n  users.distinct(\"status\", {})")
	}
	return m[1], m[2], strings.TrimSpace(m[3]), nil
}

// parseFindArgs parses the arguments to find(): filter and optional projection.
func parseFindArgs(args string) (bson.D, bson.D) {
	if args == "" {
		return bson.D{}, nil
	}

	// Split into filter and projection by finding the comma between two top-level JSON objects
	parts := splitJSONArgs(args)

	var filter bson.D
	if len(parts) > 0 {
		if err := bson.UnmarshalExtJSON([]byte(parts[0]), false, &filter); err != nil {
			filter = bson.D{}
		}
	}

	var projection bson.D
	if len(parts) > 1 {
		if err := bson.UnmarshalExtJSON([]byte(parts[1]), false, &projection); err != nil {
			projection = nil
		}
	}

	return filter, projection
}

// parseDistinctArgs parses distinct("field", {filter}) arguments.
func parseDistinctArgs(args string) (string, bson.D) {
	args = strings.TrimSpace(args)
	// Find the field name (quoted string)
	var fieldName string
	rest := args

	if len(args) > 0 && args[0] == '"' {
		end := strings.Index(args[1:], "\"")
		if end >= 0 {
			fieldName = args[1 : end+1]
			rest = strings.TrimSpace(args[end+2:])
		}
	}

	// Skip comma
	rest = strings.TrimLeft(rest, ", ")

	var filter bson.D
	if rest != "" {
		bson.UnmarshalExtJSON([]byte(rest), false, &filter)
	}

	return fieldName, filter
}

// splitJSONArgs splits comma-separated JSON arguments at the top level.
func splitJSONArgs(s string) []string {
	var parts []string
	depth := 0
	start := 0
	inString := false
	escaped := false

	for i, ch := range s {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == '{' || ch == '[' {
			depth++
		} else if ch == '}' || ch == ']' {
			depth--
		} else if ch == ',' && depth == 0 {
			part := strings.TrimSpace(s[start:i])
			if part != "" {
				parts = append(parts, part)
			}
			start = i + 1
		}
	}
	if last := strings.TrimSpace(s[start:]); last != "" {
		parts = append(parts, last)
	}
	return parts
}

// cursorToResult reads a MongoDB cursor into a flat QueryResult.
func cursorToResult(ctx context.Context, cursor *mongo.Cursor, limit int, start time.Time) (*QueryResult, error) {
	var docs []bson.D
	count := 0
	for cursor.Next(ctx) {
		if limit > 0 && count >= limit {
			break
		}
		var doc bson.D
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		docs = append(docs, doc)
		count++
	}

	columns, rows := FlattenDocuments(docs)

	return &QueryResult{
		Columns:    columns,
		Rows:       rows,
		RowCount:   int64(len(rows)),
		TotalRows:  int64(len(rows)),
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

// FlattenDocuments converts a slice of BSON documents into columns and rows.
// Nested documents are flattened one level (parent.child); deeper structures are JSON-stringified.
func FlattenDocuments(docs []bson.D) ([]string, [][]interface{}) {
	if len(docs) == 0 {
		return nil, nil
	}

	// Collect all column names across all documents, preserving order
	colIndex := map[string]int{}
	var columns []string

	addColumn := func(name string) {
		if _, exists := colIndex[name]; !exists {
			colIndex[name] = len(columns)
			columns = append(columns, name)
		}
	}

	for _, doc := range docs {
		for _, elem := range doc {
			// Flatten one level of nested documents
			if nested, ok := elem.Value.(bson.D); ok {
				for _, sub := range nested {
					addColumn(elem.Key + "." + sub.Key)
				}
			} else {
				addColumn(elem.Key)
			}
		}
	}

	// Build rows
	rows := make([][]interface{}, len(docs))
	for i, doc := range docs {
		row := make([]interface{}, len(columns))
		for _, elem := range doc {
			if nested, ok := elem.Value.(bson.D); ok {
				for _, sub := range nested {
					key := elem.Key + "." + sub.Key
					if idx, ok := colIndex[key]; ok {
						row[idx] = formatMongoValue(sub.Value)
					}
				}
			} else {
				if idx, ok := colIndex[elem.Key]; ok {
					row[idx] = formatMongoValue(elem.Value)
				}
			}
		}
		rows[i] = row
	}

	return columns, rows
}

// formatMongoValue converts a BSON value to a JSON-safe display value.
func formatMongoValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case bson.D:
		b, _ := json.Marshal(bsonDToMap(val))
		return string(b)
	case bson.A:
		b, _ := json.Marshal(val)
		return string(b)
	case bson.ObjectID:
		return val.Hex()
	case time.Time:
		return val.Format(time.RFC3339)
	case []byte:
		return string(val)
	default:
		return val
	}
}

func bsonDToMap(d bson.D) map[string]interface{} {
	m := make(map[string]interface{}, len(d))
	for _, elem := range d {
		m[elem.Key] = elem.Value
	}
	return m
}

// bsonTypeName returns a human-readable type name for a BSON value.
func bsonTypeName(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case string:
		return "string"
	case int32:
		return "int"
	case int64:
		return "long"
	case float64:
		return "double"
	case bool:
		return "bool"
	case bson.ObjectID:
		return "objectId"
	case time.Time:
		return "date"
	case bson.D:
		return "object"
	case bson.A:
		return "array"
	case []byte:
		return "binData"
	default:
		return "unknown"
	}
}
