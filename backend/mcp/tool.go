package mcp

import (
	"fmt"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
)

const (
	// defaultPageSize is what a listing returns when the caller does not say. A
	// model's context is the scarce resource here: 25 rows answer the question,
	// a thousand bury it.
	defaultPageSize = 25
	// maxPageSize caps one listing. A larger request is clamped rather than
	// refused, because the caller gets a truthful next-page token either way.
	maxPageSize = 200
)

// toolDefinitions is the tool table. It is a method because each tool closes over
// the configured read services.
func (s *Server) toolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "list_instances",
			Description: "List the database instances this workspace has registered. Start here when you do not yet know which instance a question is about.",
			RPC:         "metaxisdata.v1.InstanceService.ListInstances",
			Permission:  permission.InstancesList,
			Schema: objectSchema(map[string]any{
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}),
			Run: s.runListInstances,
		},
		{
			Name:        "list_databases",
			Description: "List the databases of one instance, each with the guid that identifies it. Use it to find the database a SQL statement or an object belongs to.",
			RPC:         "metaxisdata.v1.DatabaseService.ListDatabases",
			Permission:  permission.DatabasesList,
			Schema: objectSchema(map[string]any{
				"instance":   stringProperty("instance name or instances/<id>"),
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}, "instance"),
			Run: s.runListDatabases,
		},
		{
			Name:        "search_metadata",
			Description: "Find registry objects by name. This is the entry point when you have a name but no reference; it returns each match's guid, type and parent so the next call can address it exactly.",
			RPC:         "metaxisdata.v1.DatabaseService.SearchMetadata",
			Permission:  permission.DatabasesRead,
			Schema: objectSchema(map[string]any{
				"keyword":    stringProperty("the name, or part of it"),
				"meta_type":  stringProperty("restrict to one type, for example TABLE"),
				"instance":   stringProperty("restrict the search to this instance"),
				"database":   stringProperty("restrict the search to this database (needs instance)"),
				"schema":     stringProperty("restrict the search to this schema (needs instance and database)"),
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}, "keyword"),
			Run: s.runSearchMetadata,
		},
		{
			Name:        "list_metadata",
			Description: "List what is directly under an object: the schemas of a database, the tables and views of a schema, the columns of a table. Use get_metadata when you want one object's full shape.",
			RPC:         "metaxisdata.v1.DatabaseService.ListMetadata",
			Permission:  permission.DatabasesRead,
			Schema: objectSchema(map[string]any{
				"parent":     objectRefSchema("the object whose children you want; give a guid, or {instance, database, schema?}"),
				"meta_type":  stringProperty("only list this type, for example TABLE"),
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}, "parent"),
			Run: s.runListMetadata,
		},
		{
			Name:        "get_metadata",
			Description: "Return one object's full structure: columns with their types, indexes, keys and partitions. Ask for this after search_metadata or list_metadata gave you the object.",
			RPC:         "metaxisdata.v1.DatabaseService.GetMetadata",
			Permission:  permission.DatabasesRead,
			Schema: objectSchema(map[string]any{
				"object":    objectRefSchema("the object to read; give a guid, or {instance, database, schema?, name}"),
				"meta_type": stringProperty("the object's type, when the reference is a bare guid"),
			}, "object"),
			Run: s.runGetMetadata,
		},
		{
			Name:        "get_ddl",
			Description: "Return one object's definition as its source engine reports it. Use it when the question is about the SQL that defines a table, view or function.",
			RPC:         "metaxisdata.v1.DatabaseService.GetSchemaString",
			Permission:  permission.DatabasesRead,
			Schema: objectSchema(map[string]any{
				"object":    objectRefSchema("the object to read; give a guid, or {instance, database, schema?, name}"),
				"meta_type": stringProperty("the object's type, when the reference is a bare guid"),
			}, "object"),
			Run: s.runGetDDL,
		},
		{
			Name:        "get_quality_signal",
			Description: "Report whether one object is watched by data quality checks and how healthy it is: worst latest result, last run, quality score, recent pass rate, column coverage and open issues. This answers \"is this table healthy / should I trust it\". A table with no check at all is not monitored, which the answer reports as monitored: false rather than failing; a table whose checks are all paused reports allPaused: true with enabledRuleCount: 0 and no verdict, because nothing is being verified until a check is resumed.",
			RPC:         "metaxisdata.v1.DataQualityService.ListQualityTargets",
			Permission:  permission.DataQualityRulesList,
			Schema: objectSchema(map[string]any{
				"object":    objectRefSchema("the object to read; give a guid, or {instance, database, schema?, name}"),
				"meta_type": stringProperty("the object's type, when the reference is a bare guid"),
			}, "object"),
			Run: s.runGetQualitySignal,
		},
		{
			Name:        "list_quality_targets",
			Description: "List the tables and views data quality checks watch, with their health, quality score, column coverage, latest run and schedule. Use it to find which tables need attention, filtered by a name substring or by their worst latest result. Use get_quality_target to open one of them, and get_quality_signal for one object's trust signal.",
			RPC:         "metaxisdata.v1.DataQualityService.ListQualityTargets",
			Permission:  permission.DataQualityRulesList,
			Schema: objectSchema(map[string]any{
				"search":         stringProperty("a case-insensitive substring of the target's path or guid"),
				"worst_status":   stringProperty("only targets whose worst latest result is PASS, WARN, FAIL, ERROR, SKIPPED or NOT_EVALUATED"),
				"has_open_issue": boolProperty("only targets with an unresolved issue; false means no filter"),
				"page_size":      pageSizeProperty(),
				"page_token":     pageTokenProperty(),
			}),
			Run: s.runListQualityTargets,
		},
		{
			Name:        "get_quality_target",
			Description: "Open one table's data quality workspace: its health summary, every check with its configuration and latest result, its schedule and its unresolved problems. Use get_quality_signal for the one-line verdict, list_quality_targets to find the table, and get_quality_execution for the evidence behind a failed run.",
			RPC:         "metaxisdata.v1.DataQualityService.GetQualityTarget",
			Permission:  permission.DataQualityRulesList,
			Schema: objectSchema(map[string]any{
				"object":    objectRefSchema("the object to read; give a guid, or {instance, database, schema?, name}"),
				"meta_type": stringProperty("the object's type, when the reference is a bare guid"),
			}, "object"),
			Run: s.runGetQualityTarget,
		},
		{
			Name:        "list_quality_issues",
			Description: "List the data quality problem ledger: what is open, acknowledged or resolved, and which problems are muted, with severity, occurrence count and who acknowledged them. Use get_quality_target for one table's checks and health, list_quality_executions for the runs behind an issue, and get_quality_execution for the evidence of one run.",
			RPC:         "metaxisdata.v1.DataQualityService.ListQualityIssues",
			Permission:  permission.DataQualityIssuesList,
			Schema: objectSchema(map[string]any{
				"object":     objectRefSchema("only issues of this object; give a guid, or {instance, database, schema?, name}"),
				"rule":       stringProperty("only issues of this rule, by resource name"),
				"status":     stringProperty("only issues in this state: OPEN, ACKNOWLEDGED or RESOLVED"),
				"severity":   stringProperty("only issues of this severity: WARNING, ERROR or CRITICAL"),
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}),
			Run: s.runListQualityIssues,
		},
		{
			Name:        "list_quality_executions",
			Description: "List recent quality check runs with their verdict, thresholds and evidence flags, optionally narrowed to one object, rule, state, outcome or trigger. Use get_quality_execution for one run's full evidence — the SQL that ran and the failed sample rows.",
			RPC:         "metaxisdata.v1.DataQualityService.ListQualityExecutions",
			Permission:  permission.DataQualityExecutionsList,
			Schema: objectSchema(map[string]any{
				"object":     objectRefSchema("only executions against this object; give a guid, or {instance, database, schema?, name}"),
				"rule":       stringProperty("only executions of this rule, by resource name"),
				"state":      stringProperty("only runs in this lifecycle: PENDING, RUNNING, COMPLETED or ABANDONED"),
				"outcome":    stringProperty("only runs with this verdict: PASS, WARN, FAIL, ERROR, SKIPPED or NOT_EVALUATED"),
				"trigger":    stringProperty("only runs started this way: MANUAL or SCHEDULED"),
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}),
			Run: s.runListQualityExecutions,
		},
		{
			Name:        "get_quality_execution",
			Description: "Return one quality check run's full evidence: the SQL that executed, the failed sample rows, the rows that passed when the check keeps that comparison, a segment check's observed/new/disappeared segments, the thresholds it was judged against, any baseline and the sampling and truncation flags. Use it to explain a verdict yourself; list_quality_executions finds a run.",
			RPC:         "metaxisdata.v1.DataQualityService.GetQualityExecution",
			Permission:  permission.DataQualityExecutionsGet,
			Schema: objectSchema(map[string]any{
				"execution": stringProperty("the execution's resource name, as a listing returned it"),
			}, "execution"),
			Run: s.runGetQualityExecution,
		},
		{
			Name:        "get_quality_impact",
			Description: "Explain one table's quality state by its lineage: the downstream assets its data reaches, and the upstream targets whose checks failed or whose structure changed in the window before now. It is derived from the recorded lineage, so an empty answer means the lineage records no such object, not that nothing consumes it. Use get_quality_target for the checks themselves and get_quality_execution for one run's evidence.",
			RPC:         "metaxisdata.v1.DataQualityService.GetQualityTargetImpact",
			Permission:  permission.DataQualityRulesList,
			Schema: objectSchema(map[string]any{
				"object":       objectRefSchema("the table or view to explain; give a guid, or {instance, database, schema?, name}"),
				"meta_type":    stringProperty("the object's type, when the reference is a bare guid"),
				"depth":        map[string]any{"type": "integer", "description": "how many lineage levels downstream to walk (1-3, default 2)"},
				"window_hours": map[string]any{"type": "integer", "description": "how far back upstream evidence is looked for, in hours (1-168, default 24)"},
			}, "object"),
			Run: s.runGetQualityImpact,
		},
		{
			Name:        "get_quality_consumption_coverage",
			Description: "Measure how much of the data other assets consume is watched: registry assets that another object's lineage reads, how many of them have an enabled check, and the unmonitored ones with the most consumers. Its denominator is only the objects something reads, so it is not the overview's object coverage (which this surface does not expose) and not get_quality_signal's per-object column coverage. Use list_quality_targets or get_quality_signal to look at one of the gaps.",
			RPC:         "metaxisdata.v1.DataQualityService.GetQualityConsumptionCoverage",
			Permission:  permission.DataQualityRulesList,
			Schema: objectSchema(map[string]any{
				// Not pageSizeProperty(): this RPC's ceiling is 100 and its default is
				// 20, so advertising the general 25/200 shape would mislead a caller
				// about whether the gap list it received is complete.
				"page_size": map[string]any{"type": "integer", "description": "how many gaps to return (1-100; zero means the server default of 20, and a larger value is coerced to 100)"},
			}),
			Run: s.runGetQualityConsumptionCoverage,
		},
		{
			Name:        "analyze_sql",
			Description: "Analyze one SQL statement and report, column by column, what it reads and what it writes. Use this for a statement you have in hand; use get_lineage_graph for an object already in the registry. Each scope is analyzed on its own and reported separately.",
			RPC:         "metaxisdata.v1.LineageService.AnalyzeSQL",
			Permission:  permission.LineageGet,
			Schema: objectSchema(map[string]any{
				"sql": stringProperty("the statement to analyze"),
				"scopes": map[string]any{
					"type":        "array",
					"description": "one entry per database the statement should be resolved against; required, because unqualified names cannot say which instance they belong to",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"instance": stringProperty("instance name or instances/<id>"),
							"database": stringProperty("database name or its guid"),
							"schema":   stringProperty("schema name, when the engine has one"),
						},
						"required": []string{"instance", "database"},
					},
				},
				"depth": map[string]any{
					"type":        "integer",
					"description": "also expand each resolved target this many levels (0-10, default 0)",
				},
			}, "sql"),
			Run: s.runAnalyzeSQL,
		},
		{
			Name:        "get_lineage_graph",
			Description: "Return the multi-level lineage around one object: what it is built from, or what it feeds. The graph is column-level, and an object whose definition yields no column-level edge does not appear.",
			RPC:         "metaxisdata.v1.LineageService.GetLineageGraph",
			Permission:  permission.LineageGet,
			Schema: objectSchema(map[string]any{
				"object":    objectRefSchema("the object to start from; give a guid, or {instance, database, schema?, name}"),
				"depth":     map[string]any{"type": "integer", "description": "how many hops to expand (1-10, default 3)"},
				"direction": stringProperty("up (where data comes from), down (what it feeds) or both; default both"),
				"column":    stringProperty("keep only the edges that touch this column"),
			}, "object"),
			Run: s.runGetLineageGraph,
		},
		{
			Name:        "whoami",
			Description: "Report who the caller is and which permissions it holds. Use it before concluding that a question is unanswerable: the permissions say what this client may read.",
			RPC:         "metaxisdata.v1.UserService.GetCurrentUser",
			Schema:      objectSchema(map[string]any{}),
			Run:         s.runWhoami,
		},
	}
}

// --- shared helpers ---

// objectSchema builds a tool's input schema. Every tool takes an object; a caller
// that sends no arguments at all is still well formed.
func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func objectRefSchema(description string) map[string]any {
	return map[string]any{
		"type":        "object",
		"description": description,
		"properties": map[string]any{
			"guid":     stringProperty("a guid from an earlier result; the other fields are then ignored"),
			"instance": stringProperty("instance name or instances/<id>"),
			"database": stringProperty("database name, or its guid"),
			"schema":   stringProperty("schema name, when the engine has one"),
			"name":     stringProperty("the object's name"),
		},
	}
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func boolProperty(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func pageSizeProperty() map[string]any {
	return map[string]any{"type": "integer", "description": fmt.Sprintf("rows to return (default %d, max %d)", defaultPageSize, maxPageSize)}
}

func pageTokenProperty() map[string]any {
	return map[string]any{"type": "string", "description": "continuation token from a previous call"}
}
