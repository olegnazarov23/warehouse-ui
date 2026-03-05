export namespace ai {
	
	export class ProviderInfo {
	    name: string;
	    default_model: string;
	    min_model: string;
	    configured: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.default_model = source["default_model"];
	        this.min_model = source["min_model"];
	        this.configured = source["configured"];
	    }
	}

}

export namespace driver {
	
	export class Column {
	    name: string;
	    type: string;
	    nullable: boolean;
	    description?: string;
	    is_primary?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Column(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.nullable = source["nullable"];
	        this.description = source["description"];
	        this.is_primary = source["is_primary"];
	    }
	}
	export class ConnectionConfig {
	    id: string;
	    type: string;
	    name: string;
	    host: string;
	    database: string;
	    username: string;
	    password: string;
	    ssl_mode: string;
	    options: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.database = source["database"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.ssl_mode = source["ssl_mode"];
	        this.options = source["options"];
	    }
	}
	export class DryRunResult {
	    valid: boolean;
	    estimated_bytes: number;
	    estimated_cost_usd: number;
	    estimated_rows: number;
	    statement_type?: string;
	    error?: string;
	    warnings?: string[];
	    referenced_tables?: string[];
	
	    static createFrom(source: any = {}) {
	        return new DryRunResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.valid = source["valid"];
	        this.estimated_bytes = source["estimated_bytes"];
	        this.estimated_cost_usd = source["estimated_cost_usd"];
	        this.estimated_rows = source["estimated_rows"];
	        this.statement_type = source["statement_type"];
	        this.error = source["error"];
	        this.warnings = source["warnings"];
	        this.referenced_tables = source["referenced_tables"];
	    }
	}
	export class ExplainNode {
	    operation: string;
	    details?: string;
	    table?: string;
	    estimated_rows?: number;
	    cost?: number;
	    children?: ExplainNode[];
	
	    static createFrom(source: any = {}) {
	        return new ExplainNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.operation = source["operation"];
	        this.details = source["details"];
	        this.table = source["table"];
	        this.estimated_rows = source["estimated_rows"];
	        this.cost = source["cost"];
	        this.children = this.convertValues(source["children"], ExplainNode);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExplainResult {
	    plan: ExplainNode;
	    raw_text: string;
	
	    static createFrom(source: any = {}) {
	        return new ExplainResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plan = this.convertValues(source["plan"], ExplainNode);
	        this.raw_text = source["raw_text"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class QueryResult {
	    columns: string[];
	    column_types?: string[];
	    rows: any[][];
	    row_count: number;
	    total_rows: number;
	    duration_ms: number;
	    bytes_processed?: number;
	    bytes_billed?: number;
	    cost_usd?: number;
	    cache_hit?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new QueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = source["columns"];
	        this.column_types = source["column_types"];
	        this.rows = source["rows"];
	        this.row_count = source["row_count"];
	        this.total_rows = source["total_rows"];
	        this.duration_ms = source["duration_ms"];
	        this.bytes_processed = source["bytes_processed"];
	        this.bytes_billed = source["bytes_billed"];
	        this.cost_usd = source["cost_usd"];
	        this.cache_hit = source["cache_hit"];
	    }
	}
	export class TableInfo {
	    name: string;
	    type: string;
	    row_count: number;
	    size_bytes: number;
	    columns?: Column[];
	
	    static createFrom(source: any = {}) {
	        return new TableInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.row_count = source["row_count"];
	        this.size_bytes = source["size_bytes"];
	        this.columns = this.convertValues(source["columns"], Column);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class AiSettings {
	    provider: string;
	    model: string;
	    api_key: string;
	    endpoint: string;
	
	    static createFrom(source: any = {}) {
	        return new AiSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.api_key = source["api_key"];
	        this.endpoint = source["endpoint"];
	    }
	}
	export class DetectedConnection {
	    source: string;
	    driver_hint: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new DetectedConnection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.driver_hint = source["driver_hint"];
	        this.detail = source["detail"];
	    }
	}
	export class CodeSnippet {
	    file_path: string;
	    language: string;
	    content: string;
	    line_num: number;
	
	    static createFrom(source: any = {}) {
	        return new CodeSnippet(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file_path = source["file_path"];
	        this.language = source["language"];
	        this.content = source["content"];
	        this.line_num = source["line_num"];
	    }
	}
	export class CodeContext {
	    paths: string[];
	    snippets: CodeSnippet[];
	    full_files: CodeSnippet[];
	    detected_connections: DetectedConnection[];
	    summary: string;
	
	    static createFrom(source: any = {}) {
	        return new CodeContext(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.paths = source["paths"];
	        this.snippets = this.convertValues(source["snippets"], CodeSnippet);
	        this.full_files = this.convertValues(source["full_files"], CodeSnippet);
	        this.detected_connections = this.convertValues(source["detected_connections"], DetectedConnection);
	        this.summary = source["summary"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ConnectionStatus {
	    connected: boolean;
	    id: string;
	    name: string;
	    driver_type: string;
	    database: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.driver_type = source["driver_type"];
	        this.database = source["database"];
	    }
	}
	export class DatabaseInfo {
	    name: string;
	    tables: driver.TableInfo[];
	
	    static createFrom(source: any = {}) {
	        return new DatabaseInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.tables = this.convertValues(source["tables"], driver.TableInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class DiscoveryResult {
	    databases: DatabaseInfo[];
	    total_tables: number;
	    total_columns: number;
	
	    static createFrom(source: any = {}) {
	        return new DiscoveryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.databases = this.convertValues(source["databases"], DatabaseInfo);
	        this.total_tables = source["total_tables"];
	        this.total_columns = source["total_columns"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OptimizeResult {
	    original_sql: string;
	    optimized_sql: string;
	    explanation: string;
	    iterations: number;
	    original_dry_run?: driver.DryRunResult;
	    optimized_dry_run?: driver.DryRunResult;
	    improvements: string[];
	
	    static createFrom(source: any = {}) {
	        return new OptimizeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.original_sql = source["original_sql"];
	        this.optimized_sql = source["optimized_sql"];
	        this.explanation = source["explanation"];
	        this.iterations = source["iterations"];
	        this.original_dry_run = this.convertValues(source["original_dry_run"], driver.DryRunResult);
	        this.optimized_dry_run = this.convertValues(source["optimized_dry_run"], driver.DryRunResult);
	        this.improvements = source["improvements"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SampleQuery {
	    title: string;
	    description: string;
	    sql: string;
	    category: string;
	
	    static createFrom(source: any = {}) {
	        return new SampleQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.description = source["description"];
	        this.sql = source["sql"];
	        this.category = source["category"];
	    }
	}

}

export namespace store {
	
	export class ChatMessage {
	    id: string;
	    conversation_id: string;
	    role: string;
	    content: string;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.conversation_id = source["conversation_id"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.created_at = source["created_at"];
	    }
	}
	export class Conversation {
	    id: string;
	    connection_id: string;
	    title: string;
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new Conversation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.connection_id = source["connection_id"];
	        this.title = source["title"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class HistoryEntry {
	    id: string;
	    connection_id: string;
	    connection_name: string;
	    driver_type: string;
	    sql: string;
	    status: string;
	    error?: string;
	    bytes_processed?: number;
	    cost_usd?: number;
	    duration_ms?: number;
	    row_count?: number;
	    cache_hit: boolean;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.connection_id = source["connection_id"];
	        this.connection_name = source["connection_name"];
	        this.driver_type = source["driver_type"];
	        this.sql = source["sql"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.bytes_processed = source["bytes_processed"];
	        this.cost_usd = source["cost_usd"];
	        this.duration_ms = source["duration_ms"];
	        this.row_count = source["row_count"];
	        this.cache_hit = source["cache_hit"];
	        this.created_at = source["created_at"];
	    }
	}
	export class HistoryStats {
	    total_queries: number;
	    total_cost_usd: number;
	    total_bytes_read: number;
	    avg_duration_ms: number;
	    queries_this_week: number;
	
	    static createFrom(source: any = {}) {
	        return new HistoryStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_queries = source["total_queries"];
	        this.total_cost_usd = source["total_cost_usd"];
	        this.total_bytes_read = source["total_bytes_read"];
	        this.avg_duration_ms = source["avg_duration_ms"];
	        this.queries_this_week = source["queries_this_week"];
	    }
	}
	export class QueryTemplate {
	    id: string;
	    name: string;
	    description: string;
	    sql: string;
	    driver_type: string;
	    category: string;
	    difficulty: string;
	
	    static createFrom(source: any = {}) {
	        return new QueryTemplate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.sql = source["sql"];
	        this.driver_type = source["driver_type"];
	        this.category = source["category"];
	        this.difficulty = source["difficulty"];
	    }
	}
	export class SavedConnection {
	    id: string;
	    name: string;
	    driver_type: string;
	    config_json: string;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new SavedConnection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.driver_type = source["driver_type"];
	        this.config_json = source["config_json"];
	        this.created_at = source["created_at"];
	    }
	}
	export class SavedQuery {
	    id: string;
	    name: string;
	    description: string;
	    sql: string;
	    tags: string;
	    slug: string;
	    connection_id: string;
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new SavedQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.sql = source["sql"];
	        this.tags = source["tags"];
	        this.slug = source["slug"];
	        this.connection_id = source["connection_id"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}

}

