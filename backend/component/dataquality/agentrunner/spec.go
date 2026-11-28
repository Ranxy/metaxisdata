package agentrunner

import (
	"context"
	"errors"
	"fmt"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// toolSpec is one static declaration of a model-wake tool. The declaration is
// the single source of the schema the model sees; the wake binds an execution
// body to it.
type toolSpec struct {
	name        string
	description string
	parameters  any
	meta        tool.Meta
}

// readMeta is the metadata of a tool that reads the registry.
func readMeta() tool.Meta {
	return tool.Meta{
		Group:          "quality",
		Scope:          tool.ScopeProductOnly,
		Authz:          tool.AuthzPermission,
		ReadOnly:       true,
		Permission:     string(permission.DataQualityRulesList),
		MaxResultBytes: maxToolResultBytes,
		// Registry reads are independent of each other and of session state.
		ParallelSafe: true,
	}
}

// evidenceMeta is the metadata of a tool that reads a target's evidence through the
// application layer. The permission is rules.evidence, not rules.list: the latter is
// in the member baseline, which grants it to every principal on every target, so it
// cannot express "this agent, these databases".
func evidenceMeta() tool.Meta {
	meta := readMeta()
	meta.Permission = string(permission.DataQualityRulesEvidence)
	return meta
}

// issuesMeta is the problem-ledger read. issues.list is baseline and cannot express
// "this agent, these databases": the target session is fixed, and the lead is bounded
// by requireLeadTarget.
func issuesMeta() tool.Meta {
	meta := readMeta()
	meta.Permission = string(permission.DataQualityIssuesList)
	return meta
}

// repairMeta is the metadata of a tool that changes one check through the application layer. It
// is a publish-class write: the pipeline audits it, and the permission is rules.repair rather
// than rules.update, so the catalog reads as "this actor may not rewrite an arbitrary
// definition".
func repairMeta() tool.Meta {
	return tool.Meta{
		Group:          "quality",
		Scope:          tool.ScopeProductOnly,
		Authz:          tool.AuthzPermission,
		SideEffect:     tool.SideEffectPublish,
		Permission:     string(permission.DataQualityRulesRepair),
		MaxResultBytes: maxToolResultBytes,
	}
}

// proposeMeta is the metadata of a tool that records a proposal or evidence
// through an RPC handler.
func proposeMeta() tool.Meta {
	return tool.Meta{
		Group:          "quality",
		Scope:          tool.ScopeProductOnly,
		Authz:          tool.AuthzPermission,
		SideEffect:     tool.SideEffectPropose,
		Permission:     string(permission.DataQualityPlansUpdate),
		MaxResultBytes: maxToolResultBytes,
	}
}

// suggestMeta is the metadata of a tool that writes a proposal-class record through the
// application layer. The permission is rules.suggest, which is the agent's own drafting right and
// is deliberately outside the member baseline, so the grant that bounds the agent to its databases
// actually applies.
func suggestMeta() tool.Meta {
	meta := proposeMeta()
	meta.Permission = string(permission.DataQualitySuggest)
	return meta
}

// internalMeta is the metadata of a wake-control tool. It belongs to this product
// only and is scoped by session ownership, not by a workspace permission.
func internalMeta() tool.Meta {
	return tool.Meta{
		Group: "agent",
		Scope: tool.ScopeProductOnly,
		Authz: tool.AuthzInternal,
	}
}

// askOptionParameters is the selectable-choice schema shared by an owner ask and a
// subagent's QUESTION.
func askOptionParameters() map[string]any {
	return map[string]any{
		"options": map[string]any{
			"type":        "array",
			"description": "choices the owner can click. Put a recommended choice first and append (Recommended) to its label. Leave empty for a free-text question.",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label":       map[string]any{"type": "string", "description": "short user-facing answer value"},
					"description": map[string]any{"type": "string", "description": "one sentence explaining the tradeoff or impact"},
				},
				"required": []string{"label"},
			},
		},
		"multi_select": map[string]any{"type": "boolean", "description": "whether the owner may select more than one option; defaults to false"},
	}
}

// askQuestionItemParameters is one question in an ask batch.
func askQuestionItemParameters() map[string]any {
	parameters := askOptionParameters()
	parameters["id"] = map[string]any{"type": "string", "description": "stable short slug for this question, such as timeout; defaults to q1, q2... and is echoed by the answer"}
	parameters["question"] = map[string]any{"type": "string", "description": "the one decision this question puts to the owner"}
	parameters["header"] = map[string]any{"type": "string", "description": "optional short heading, such as Timeout or Confirm"}
	parameters["detail"] = map[string]any{"type": "string", "description": "optional supporting evidence rendered with the question, not in the option labels"}
	parameters["requests"] = map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string"},
		"description": "request names this question decides. A single-question ask may omit it to cover all of the ask's requests; in a batch every question must name a non-empty, disjoint subset covering the ask's requests.",
	}
	return parameters
}

// askQuestionParameters is the questions array shared by ask_owner and forward_request.
func askQuestionParameters() map[string]any {
	return map[string]any{
		"questions": map[string]any{
			"type":        "array",
			"minItems":    1,
			"maxItems":    5,
			"description": "the decisions to put to the owner. Related decisions may share one ask; each question names its own requests when it decides a subset.",
			"items": map[string]any{
				"type":       "object",
				"properties": askQuestionItemParameters(),
				"required":   []string{"question"},
			},
		},
	}
}

// askOwnerParameters is askQuestionParameters plus the optional requests the ask covers.
// The legacy single-question fields stay declared for an in-flight transcript, but the
// batch form is what the schema requires so a schema-obedient model can reach it.
func askOwnerParameters() map[string]any {
	parameters := askQuestionParameters()
	legacy := askQuestionItemParameters()
	for _, name := range []string{"question", "header", "detail", "options", "multi_select"} {
		parameters[name] = legacy[name]
	}
	parameters["requests"] = map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string"},
		"description": "request names this ask covers. Each question in a batch still names the subset it decides.",
	}
	return parameters
}

// answerRequestParameters is the schema of answer_request.
func answerRequestParameters() map[string]any {
	return map[string]any{
		"request": map[string]any{"type": "string", "description": "qualityAgentRequests/... id from list_requests"},
		"answer":  map[string]any{"type": "string", "description": "the answer text; use selected/custom when relaying a structured owner answer"},
		"selected": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "option labels the owner selected, when the question offered choices",
		},
		"custom": map[string]any{"type": "string", "description": "free-form text the owner typed"},
	}
}

// targetToolSpecs is the base tool set of a target work session. Capability
// profiles add the tools a selected capability grants; the base set itself is
// read-, propose- and wake-control only.
func targetToolSpecs() []toolSpec {
	specs := []toolSpec{
		{
			name:        "send_lead_message",
			description: "Send a self-contained report or question to the lead. Use it when you need a decision, a repair failed, a check cannot run, a proposal needs a person, a task the lead assigned is done, or your target changed in a way the lead must know. A QUESTION may offer clickable options with multi_select. Do not send routine all-good reports.",
			parameters: map[string]any{"type": "object", "properties": func() map[string]any {
				parameters := askOptionParameters()
				parameters["message"] = map[string]any{"type": "string", "description": "the report or question; the lead does not see your transcript"}
				parameters["intent"] = map[string]any{"type": "string", "enum": []string{"REPORT", "QUESTION", "ANSWER"}, "description": "what the message is"}
				parameters["case"] = map[string]any{"type": "string", "description": "optional qualityAgentCases id the message is about"}
				parameters["question_key"] = map[string]any{
					"type":        "string",
					"description": "stable short slug for one QUESTION, e.g. timeout or pause-check. Reuse it only when retrying the same question; a different decision needs a different key. Omit when the question is about one case and the case id is enough.",
				}
				return parameters
			}(), "required": []string{"message"}},
			meta: internalMeta(),
		},
		{
			name:        "get_target_summary",
			description: "Read the target's plan state, criticality, rule count, adoption status, pending drafts and uncovered declared properties.",
			parameters:  emptyParameters(),
			meta:        readMeta(),
		},
		{
			name:        "list_recent_executions",
			description: "List the target's recent executions with the check they belong to, their outcome, failure code and message, and row counts.",
			parameters:  emptyParameters(),
			meta:        evidenceMeta(),
		},
		{
			name: "inspect_check",
			description: "Read one check's definition — type, revision, severity and the rest of its config — together with that check's own recent executions and failure evidence. " +
				"Naming no check inspects the newest failing one.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"rule": map[string]any{
					"type":        "string",
					"description": "optional check resource name; omit to inspect the newest failing check",
				},
				"executions": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     20,
					"description": "how many of the check's executions to include, at most 20",
				},
			}},
			meta: evidenceMeta(),
		},
		{
			name:        "list_issues",
			description: "Read this target's issue ledger — the durable problems a FAIL opened (a WARN keeps an existing issue alive but opens none). Use it before telling the lead or owner that a defect is untracked: the issue id is the record. A failing check is a finding, not a work item.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"status": map[string]any{
					"type":        "string",
					"enum":        []string{"OPEN", "ACKNOWLEDGED", "RESOLVED", "ALL"},
					"description": "optional; defaults to OPEN (unresolved; acknowledged included)",
				},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
			}},
			meta: issuesMeta(),
		},
		{
			name: "control_check",
			description: "Pause a check whose own definition cannot run (an unsupported column, a refused bound, a moved structure), so its failures stop while a person decides what to do with it. " +
				"An empty target or a data failure has no such case and cannot be paused this way. " +
				"The check's case becomes escalated. You cannot archive or resume a check.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"case": map[string]any{
					"type":        "string",
					"description": "the qualityAgentCases id from the work list above (a case id, never an execution id)",
				},
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"PAUSE"},
					"description": "the state change to apply",
				},
			}, "required": []string{"case", "action"}},
			meta: repairMeta(),
		},
		{
			name: "run_check",
			description: "Run the check a diagnosed case is about, right now, to reproduce the failure or verify a repair. " +
				"The verdict closes or advances the case.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"case": map[string]any{
					"type":        "string",
					"description": "the qualityAgentCases id from the work list above (a case id, never an execution id)",
				},
			}, "required": []string{"case"}},
			meta: repairMeta(),
		},
		{
			name: "repair_check",
			description: "Apply the repair a diagnosed case names. Omit field and value and the server writes the fix it computes — aligning a check's " +
				"saved column snapshot with the registry — so a check that reports the target's structure changed can run again. " +
				"Name a field and a value only to change what the check accepts: that needs a tune-level delegation and is replayed against " +
				"the recent runs, and it is refused when a failing run would turn into a pass. Either way the next run verifies the repair.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"case": map[string]any{
					"type":        "string",
					"description": "the qualityAgentCases id from the work list above (a case id, never an execution id)",
				},
				"field": map[string]any{
					"type":        "string",
					"description": "optional: the dotted config field the diagnosis named, only for a criterion change",
				},
				"value": map[string]any{
					"type":        "string",
					"description": "optional: the value that field should take; required with a field",
				},
			}, "required": []string{"case"}},
			meta: repairMeta(),
		},
		{
			name: "propose_revision",
			description: "Ask the owner to change one field of a check you may not rewrite yourself: a bound that never binds, " +
				"a threshold, a field a person pinned. The change is validated but not applied, and the case becomes escalated. " +
				"Name the field the diagnosis pointed at.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"case": map[string]any{
					"type":        "string",
					"description": "the qualityAgentCases id from the work list above (a case id, never an execution id)",
				},
				"field": map[string]any{
					"type":        "string",
					"description": "the dotted config field the diagnosis named, e.g. range.min",
				},
				"value": map[string]any{
					"type":        "string",
					"description": "the value the field should take",
				},
				"rationale": map[string]any{
					"type":        "string",
					"description": "why this change, in one or two sentences, for the owner",
				},
			}, "required": []string{"case", "field", "value"}},
			meta: proposeMeta(),
		},
		{
			name: "flag_proposal",
			description: "Record why one of your own drafts should not be adopted on its own: a structural false positive you expect, " +
				"a column that is usually empty, a check that will only ever be noise. A flagged draft is never adopted automatically; " +
				"a person reads the concern and decides.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"draft": map[string]any{
					"type":        "string",
					"description": "the qualitySuggestions resource name or id draft_checks returned",
				},
				"concern": map[string]any{
					"type":        "string",
					"description": "what the draft gets wrong, in one or two sentences",
				},
			}, "required": []string{"draft", "concern"}},
			meta: suggestMeta(),
		},
		{
			name:        "get_memory",
			description: "List the target's confirmed asset facts.",
			parameters:  emptyParameters(),
			meta:        readMeta(),
		},
		{
			name: "record_asset_fact",
			description: "Record one durable fact about the target — what it is, what it holds, how it behaves — in a sentence a " +
				"later wake reads and recognizes. Write it once, in stable wording, and keep the reasoning in your message: a wake's " +
				"analysis is not a fact, and a fact restated in new words on every wake stops being one. Record nothing when a wake " +
				"concluded nothing new.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"statement": map[string]any{
					"type":        "string",
					"description": "one durable sentence about the asset, e.g. the table holds no rows, or created_at is filled by the application on insert",
				},
			}, "required": []string{"statement"}},
			meta: proposeMeta(),
		},
		{
			name: "escalate_case",
			description: "Hand one work item to its owner and stop working on it: a change you may not make, a failure no field change " +
				"fixes, a decision that is not yours. The case becomes escalated and the owner is told, with your reason as the question. " +
				"Use this instead of reporting a blocker: a blocked session stops every target it covers. It acts on a work item only: a " +
				"check that produced a FAIL has none, because a failing check is a finding the issue ledger already reports to your owner — read list_issues and report that issue id instead.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"case": map[string]any{
					"type":        "string",
					"description": "the qualityAgentCases id from the work list above — never an execution or a check id; without a work list entry there is nothing to escalate",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "what the owner has to decide, in one or two sentences",
				},
			}, "required": []string{"case", "reason"}},
			meta: proposeMeta(),
		},
		{
			name: "block_session",
			description: "Park this session on an infrastructure failure only: a spend or wake budget, a transient internal error, a concurrency " +
				"limit. The same code must persist for three consecutive wakes before the session parks. A decision about a check goes to " +
				"escalate_case instead, because a parked session stops every target it covers.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"code":   map[string]any{"type": "string"},
				"reason": map[string]any{"type": "string"},
			}, "required": []string{"code", "reason"}},
			meta: internalMeta(),
		},
		{
			name:        "schedule_wake",
			description: "Ask for an earlier wake when there is concrete unfinished work.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"after_minutes": map[string]any{"type": "integer", "minimum": 1, "maximum": 10080},
				"reason":        map[string]any{"type": "string"},
			}, "required": []string{"after_minutes"}},
			meta: internalMeta(),
		},
	}
	return specs
}

// conversationToolSpecs is the front-office tool set: reads over the agent's scope
// and queue, plus the two wake controls. It deliberately cannot touch a single
// target's plan or checks; that work belongs to the target sessions.
func conversationToolSpecs() []toolSpec {
	return []toolSpec{
		{
			name:        "get_scope_summary",
			description: "Read the agent's scope health: databases, discovered/monitored/dormant targets and the queue split.",
			parameters:  emptyParameters(),
			meta:        readMeta(),
		},
		{
			name:        "list_scope_targets",
			description: "List targets in the agent's scope with their queue state, mode and priority.",
			parameters:  emptyParameters(),
			meta:        readMeta(),
		},
		{
			name:        "get_target_summary",
			description: "Read one target's plan state, criticality, rule count, adoption status, pending drafts and uncovered declared properties.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"target": map[string]any{"type": "string", "description": "subagent name, target GUID or session resource name"},
			}, "required": []string{"target"}},
			meta: readMeta(),
		},
		{
			name:        "list_recent_executions",
			description: "List one target's recent executions with their check, outcome, failure code and row counts.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"target": map[string]any{"type": "string", "description": "subagent name, target GUID or session resource name"},
			}, "required": []string{"target"}},
			meta: evidenceMeta(),
		},
		{
			name:        "inspect_check",
			description: "Read one check's definition and its own recent executions for a target. Naming no check inspects the newest failing one.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"target":     map[string]any{"type": "string", "description": "subagent name, target GUID or session resource name"},
				"rule":       map[string]any{"type": "string", "description": "optional check resource name"},
				"executions": map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
			}, "required": []string{"target"}},
			meta: evidenceMeta(),
		},
		{
			name:        "get_memory",
			description: "List one target's confirmed asset facts.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"target": map[string]any{"type": "string", "description": "subagent name, target GUID or session resource name"},
			}, "required": []string{"target"}},
			meta: readMeta(),
		},
		{
			name:        "list_open_cases",
			description: "List a target's open work items.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"target": map[string]any{"type": "string", "description": "subagent name, target GUID or session resource name"},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
			}, "required": []string{"target"}},
			meta: readMeta(),
		},
		{
			name:        "list_issues",
			description: "Read one target's issue ledger — the durable problems a FAIL opened (a WARN keeps an existing issue alive but opens none). Use it before saying a defect is untracked: the issue id is the record, and a failing check is not a work item.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"target": map[string]any{"type": "string", "description": "subagent name, session resource name, or a target GUID that is one of this agent's enrollments"},
				"status": map[string]any{
					"type":        "string",
					"enum":        []string{"OPEN", "ACKNOWLEDGED", "RESOLVED", "ALL"},
					"description": "optional; defaults to OPEN (unresolved; acknowledged included)",
				},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
			}, "required": []string{"target"}},
			meta: issuesMeta(),
		},
		{
			name:        "list_subagents",
			description: "List the fixed subagents this lead coordinates. Each row carries the name, target, state, mode, next wake and last deterministic review.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "optional name or target GUID substring"},
				"state": map[string]any{"type": "string", "description": "optional state filter"},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
			}},
			meta: internalMeta(),
		},
		{
			name:        "get_subagent",
			description: "Read one subagent's status and up to five open work items.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"agent": map[string]any{"type": "string", "description": "subagent name, target GUID or session resource name"},
			}, "required": []string{"agent"}},
			meta: internalMeta(),
		},
		{
			name:        "send_subagent_message",
			description: "Send an instruction, assignment or answer to one subagent. Delivery is accepted into its inbox; the answer arrives later as a message, not as this result.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"agent":   map[string]any{"type": "string", "description": "subagent name, target GUID or session resource name"},
				"message": map[string]any{"type": "string"},
				"intent":  map[string]any{"type": "string", "enum": []string{"MESSAGE", "ASSIGN", "ANSWER", "QUESTION"}},
			}, "required": []string{"agent", "message"}},
			meta: internalMeta(),
		},
		{
			name:        "list_requests",
			description: "List the subagent requests waiting for the lead. Default is OPEN and FORWARDED. A forwarded request names its ask; OPEN has no ask yet.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"status": map[string]any{"type": "string", "enum": []string{"OPEN", "FORWARDED", "CLOSED"}},
				"target": map[string]any{"type": "string"},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			}},
			meta: internalMeta(),
		},
		{
			name:        "list_asks",
			description: "List the questions this agent has put to the owner. Default OPEN; use it to find a stale question before cancel_ask. CLOSED means resolved, dismissed or cancelled; ALL includes the live ones.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"status": map[string]any{"type": "string", "enum": []string{"OPEN", "ANSWERED", "CLOSED", "CANCELLED", "ALL"}},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			}},
			meta: internalMeta(),
		},
		{
			name:        "answer_request",
			description: "Answer one subagent request yourself. The answer is delivered to the origin subagent and the request becomes ANSWERED.",
			parameters:  map[string]any{"type": "object", "properties": answerRequestParameters(), "required": []string{"request"}},
			meta:        internalMeta(),
		},
		{
			name:        "forward_request",
			description: "Forward one or more subagent requests to the owner as one ask with one or more questions. Use questions for the batch form; the legacy question form is still accepted for a single decision. A question may offer clickable options, and each question names the requests it decides. The owner's answer comes back as a delivered message.",
			parameters:  map[string]any{"type": "object", "properties": askOwnerParameters(), "required": []string{"requests", "questions"}},
			meta:        internalMeta(),
		},
		{
			name:        "cancel_ask",
			description: "Withdraw an open question to the owner when a later answer makes it obsolete. The requests behind it return to you so you can answer them or put them in a new ask. Only question and anomaly asks can be withdrawn; approval asks belong to the adoption queue.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"ask":     map[string]any{"type": "string", "description": "the ask resource name or id, from list_requests or your earlier ask result"},
				"message": map[string]any{"type": "string", "description": "what to tell the owner about the withdrawal; write it in the owner's language"},
			}, "required": []string{"ask", "message"}},
			meta: internalMeta(),
		},
		{
			name:        "reply_owner",
			description: "Tell the owner something on a wake that was not an owner question (a follow-up or a result). Do not use it for routine chatter.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"message": map[string]any{"type": "string"},
			}, "required": []string{"message"}},
			meta: internalMeta(),
		},
		{
			name:        "ask_owner",
			description: "Ask the owner one or more questions, optionally with clickable options and covering several requests. Use questions for the batch form; the legacy question form is still accepted for a single decision. This writes an owner-visible message and rings the doorbell; end the wake after it.",
			parameters:  map[string]any{"type": "object", "properties": askOwnerParameters(), "required": []string{"questions"}},
			meta:        internalMeta(),
		},
		{
			name:        "schedule_wake",
			description: "Ask for an earlier wake when there is concrete unfinished work.",
			parameters: map[string]any{"type": "object", "properties": map[string]any{
				"after_minutes": map[string]any{"type": "integer", "minimum": 1, "maximum": 10080},
				"reason":        map[string]any{"type": "string"},
			}, "required": []string{"after_minutes"}},
			meta: internalMeta(),
		},
	}
}

// Spec is one agent product: its name, prompt, tool whitelist and turn budget.
// The mode selects which spec a session runs, so monitor and triage differ only
// in declaration, never in engine code.
type Spec struct {
	// Name identifies the product in metrics and logs.
	Name string
	// Feature labels this product's model calls in metrics.
	Feature string
	// Prompt is the system prompt the runtime renders.
	Prompt *agent.Prompt
	// Tools is the base whitelist; capability profiles add feature tools and the
	// registry validates the composed set at wake time.
	Tools []toolSpec
	// MaxTurns bounds one wake's model calls.
	MaxTurns int
}

// The specs are process-level declarations: one prompt, one tool whitelist and one
// turn budget per product, resolved once.
var (
	// conversationProduct is the agent's front office: the owner's conversation,
	// woken only by a question or a scheduled follow-up.
	conversationProduct = &Spec{
		Name:     "quality-assistant",
		Feature:  "dq_agent",
		Prompt:   conversationPrompt(),
		Tools:    conversationToolSpecs(),
		MaxTurns: 16,
	}
	// monitorProduct is the long-lived monitoring product: one target, woken by
	// events, questions and a daily cadence.
	monitorProduct = &Spec{
		Name:     "quality-monitor",
		Feature:  "dq_agent",
		Prompt:   qualityPrompt(),
		Tools:    targetToolSpecs(),
		MaxTurns: modelMaxTurns,
	}
	// triageProduct is the coverage product: it inspects an uncovered target,
	// drafts checks and records what it concluded.
	triageProduct = &Spec{
		Name:     "quality-triage",
		Feature:  "dq_agent",
		Prompt:   qualityPrompt(),
		Tools:    targetToolSpecs(),
		MaxTurns: triageMaxTurns,
	}
)

// monitorSpec returns the monitoring product.
func monitorSpec() *Spec { return monitorProduct }

// triageSpec returns the coverage product.
func triageSpec() *Spec { return triageProduct }

// specForSession selects the product a session runs. A conversation session has no
// target; target sessions pick monitor or triage from their mode.
func specForSession(session *store.QualityAgentSessionMessage) *Spec {
	if isLeadSession(session) {
		return conversationProduct
	}
	return specFor(session.Mode)
}

// specFor selects the product a target session runs.
func specFor(mode storepb.QualityAgentSessionMode) *Spec {
	if mode == storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE {
		return triageSpec()
	}
	return monitorSpec()
}

// ValidateSpecs checks the product declarations at startup: every spec's tools
// register, the whitelist resolves, and the engine has an execution branch for
// each name. The server calls it before constructing the runner, so a renamed or
// misspelled tool fails the boot instead of reaching the model.
func ValidateSpecs() error {
	if err := validateCapabilityProfiles(); err != nil {
		return err
	}
	for _, spec := range []*Spec{monitorProduct, triageProduct, conversationProduct} {
		declarations := append([]toolSpec(nil), spec.Tools...)
		for _, profile := range capabilityProfiles() {
			declarations = append(declarations, profile.Tools(spec)...)
		}
		registry := tool.NewRegistry()
		seen := make(map[string]bool, len(declarations))
		for _, declaration := range declarations {
			if seen[declaration.name] {
				return fmt.Errorf("spec %s declares tool %q twice", spec.Name, declaration.name)
			}
			seen[declaration.name] = true
			declarationTool := tool.Tool{
				Name:        declaration.name,
				Description: declaration.description,
				Parameters:  declaration.parameters,
				Meta:        declaration.meta,
				Execute: func(context.Context, tool.Call, *tool.Invocation) (tool.Result, error) {
					return tool.Result{}, errors.New("the declaration is not executable")
				},
			}
			if err := registry.Register(declarationTool); err != nil {
				return fmt.Errorf("spec %s: %w", spec.Name, err)
			}
			if !handlesTool(declaration.name) {
				return fmt.Errorf("spec %s declares tool %q but the engine has no execution branch", spec.Name, declaration.name)
			}
			if err := validateToolPermission(declaration); err != nil {
				return fmt.Errorf("spec %s: %w", spec.Name, err)
			}
		}
		if _, err := registry.Resolve(toolSpecNames(declarations)); err != nil {
			return fmt.Errorf("spec %s: %w", spec.Name, err)
		}
	}
	return nil
}

// validateToolPermission checks that a tool's declared permission is one the agent
// principal can actually hold. A tool gated by a permission no role grants would fail at
// call time for every agent, which is a declaration bug worth a boot error rather than a
// wake-time surprise.
//
// "Holdable" means the member baseline or the quality agent role grants it. The baseline
// counts because Manager.CheckPermission short-circuits on it for every principal, which is
// also why a baseline permission cannot scope a read to one database.
func validateToolPermission(declaration toolSpec) error {
	if declaration.meta.Authz != tool.AuthzPermission {
		return nil
	}
	perm := permission.Permission(declaration.meta.Permission)
	if perm == "" {
		return fmt.Errorf("tool %q declares no permission", declaration.name)
	}
	for _, roleID := range []string{store.WorkspaceMemberRole, store.QualityAgentRole} {
		if role := store.GetPredefinedRole(roleID); role != nil && role.Permissions[perm] {
			return nil
		}
	}
	return fmt.Errorf("tool %q declares %q, which neither the member baseline nor the quality agent role grants",
		declaration.name, perm)
}

// toolSpecNames lists a declaration slice's tool names in order.
func toolSpecNames(declarations []toolSpec) []string {
	names := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		names = append(names, declaration.name)
	}
	return names
}

// specNames lists a spec's tool names in declaration order.
func specNames(declarations []toolSpec) []string {
	names := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		names = append(names, declaration.name)
	}
	return names
}
