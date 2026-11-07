import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  QualityAgentMessageOrigin,
  QualityAgentMessageRole,
  type QualityAgentSession,
  type QualityAgentSessionMessage,
  QualityAgentSessionState,
  type QualityToolCall,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  formatRowCount,
  formatTargetPath,
  type QualityBadgeVariant,
} from "./dataQuality";

/**
 * The quality agent session vocabulary, as i18n keys and display strings.
 *
 * A session is one target's long-lived conversation: the runner appends a wake
 * message, the model answers and calls tools, and the next wake continues the
 * same transcript. These helpers keep the list, the conversation page and the
 * target card rendering the same state the same way, and keep the proto enum off
 * the screen (a zh-CN page must print 已启用, not `ENABLED`).
 *
 * The keys the switch helpers return are declared in
 * `scripts/check-vue-i18n.mjs`'s DYNAMIC_PREFIXES: the checker cannot follow a
 * key a helper returns, so each family is listed there explicitly.
 */

/** A running session whose lease is live but whose transcript stopped growing. */
export const AGENT_SESSION_STALE_AFTER_MS = 10 * 60 * 1000;

/** A runner whose last tick is older than this is not ticking. */
export const AGENT_RUNNER_STALE_AFTER_MS = 60 * 1000;

const DEFAULT_LOCALE = "en-US";
const WAKE_FINISHED_PREFIX = "wake finished:";
const TOOL_ARGUMENTS_PREVIEW_LENGTH = 120;

// ---- session state ----

/** The label key for a session's durable lifecycle state. */
export function agentSessionStateKey(state: QualityAgentSessionState): string {
  switch (state) {
    case QualityAgentSessionState.ENABLED:
      return "dataQuality.agent.state.enabled";
    case QualityAgentSessionState.PAUSED:
      return "dataQuality.agent.state.paused";
    case QualityAgentSessionState.BLOCKED:
      return "dataQuality.agent.state.blocked";
    case QualityAgentSessionState.ARCHIVED:
      return "dataQuality.agent.state.archived";
    default:
      return "dataQuality.agent.state.unspecified";
  }
}

/** The badge variant for a session's state. Only a blocker is a warning. */
export function agentSessionStateVariant(
  state: QualityAgentSessionState
): QualityBadgeVariant {
  switch (state) {
    case QualityAgentSessionState.ENABLED:
      return "success";
    case QualityAgentSessionState.BLOCKED:
      return "warning";
    case QualityAgentSessionState.PAUSED:
    case QualityAgentSessionState.ARCHIVED:
      return "neutral";
    default:
      return "outline";
  }
}

export interface AgentSessionHeartbeat {
  /** The server derives this from a live lease; it is not the durable state. */
  running: boolean;
  /**
   * Running, but the newest transcript entry is old. A crashed process leaves
   * the lease live for the rest of its duration, so this is the only liveness
   * signal the client has.
   */
  stale: boolean;
}

/** Whether a session is running, and whether that claim looks stale. */
export function agentSessionHeartbeat(
  session: QualityAgentSession,
  now = Date.now()
): AgentSessionHeartbeat {
  const running = session.running;
  const lastActivity = timestampMillis(session.lastActivityTime);
  const stale =
    running &&
    lastActivity !== undefined &&
    now - lastActivity >= AGENT_SESSION_STALE_AFTER_MS;
  return { running, stale };
}

/** Whether a runner's process-local tick is fresh enough to call it alive. */
export function runnerIsTicking(
  lastTick: Timestamp | undefined,
  now = Date.now()
): boolean {
  const tick = timestampMillis(lastTick);
  return tick !== undefined && now - tick < AGENT_RUNNER_STALE_AFTER_MS;
}

function timestampMillis(value: Timestamp | undefined): number | undefined {
  if (value === undefined) {
    return undefined;
  }
  const seconds = Number(value.seconds);
  const nanos = Number(value.nanos ?? 0);
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return undefined;
  }
  return seconds * 1000 + Math.floor(nanos / 1_000_000);
}

// ---- transcript vocabulary ----

/** The label key for one transcript entry's role. */
export function agentMessageRoleKey(role: QualityAgentMessageRole): string {
  switch (role) {
    case QualityAgentMessageRole.SYSTEM:
      return "dataQuality.agent.role.system";
    case QualityAgentMessageRole.USER:
      return "dataQuality.agent.role.user";
    case QualityAgentMessageRole.ASSISTANT:
      return "dataQuality.agent.role.assistant";
    case QualityAgentMessageRole.TOOL:
      return "dataQuality.agent.role.tool";
    case QualityAgentMessageRole.ENGINE:
      return "dataQuality.agent.role.engine";
    case QualityAgentMessageRole.COMPACTION:
      return "dataQuality.agent.role.compaction";
    default:
      return "dataQuality.agent.role.unspecified";
  }
}

/** The label key for what produced a user or engine message. */
export function agentMessageOriginKey(
  origin: QualityAgentMessageOrigin
): string {
  switch (origin) {
    case QualityAgentMessageOrigin.WAKE:
      return "dataQuality.agent.origin.wake";
    case QualityAgentMessageOrigin.EVENT:
      return "dataQuality.agent.origin.event";
    case QualityAgentMessageOrigin.ASK:
      return "dataQuality.agent.origin.ask";
    case QualityAgentMessageOrigin.RUNNER:
      return "dataQuality.agent.origin.runner";
    case QualityAgentMessageOrigin.MODEL:
      return "dataQuality.agent.origin.model";
    case QualityAgentMessageOrigin.COMPACTION:
      return "dataQuality.agent.origin.compaction";
    default:
      return "dataQuality.agent.origin.unspecified";
  }
}

/** One wake's slice of the transcript, with the header data the page shows. */
export interface AgentWakeGroup {
  /** The wake number; zero holds the session's system message. */
  wake: number;
  /** The group's messages, oldest first. */
  messages: QualityAgentSessionMessage[];
  /** The first message's timestamp, which is when the wake began its record. */
  startTime?: Timestamp;
  /** The origin of the wake's user message; UNSPECIFIED when it has none. */
  origin: QualityAgentMessageOrigin;
  /**
   * The wake's own conclusion: the text of the last runner note, with the
   * runner's `wake finished: ` prefix stripped. Empty when the wake recorded none.
   */
  summary: string;
}

/**
 * Group a transcript into wakes, oldest wake first.
 *
 * The transcript is append-only but a caller merges an older page and a live
 * page, so the input is ordered defensively by `seq` before grouping. A wake
 * with no user message (the system-message group 0, or a compaction checkpoint
 * recorded between wakes) still forms a group; the header falls back to the
 * group's first message.
 */
export function groupMessagesByWake(
  messages: readonly QualityAgentSessionMessage[]
): AgentWakeGroup[] {
  const ordered = [...messages].sort(compareSeq);
  const groups = new Map<number, AgentWakeGroup>();
  for (const message of ordered) {
    let group = groups.get(message.wake);
    if (!group) {
      group = {
        wake: message.wake,
        messages: [],
        startTime: message.createTime,
        origin: QualityAgentMessageOrigin.UNSPECIFIED,
        summary: "",
      };
      groups.set(message.wake, group);
    }
    group.messages.push(message);
  }
  return [...groups.values()]
    .sort((left, right) => left.wake - right.wake)
    .map((group) => ({
      ...group,
      origin:
        group.messages.find(
          (message) => message.role === QualityAgentMessageRole.USER
        )?.origin ?? QualityAgentMessageOrigin.UNSPECIFIED,
      summary: wakeSummary(group.messages),
    }));
}

/** The text of the last runner note, which is how a wake records its outcome. */
function wakeSummary(messages: QualityAgentSessionMessage[]): string {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index];
    if (message.role !== QualityAgentMessageRole.ENGINE) {
      continue;
    }
    const text = message.content.trim();
    if (!text) {
      continue;
    }
    return text.startsWith(WAKE_FINISHED_PREFIX)
      ? text.slice(WAKE_FINISHED_PREFIX.length).trim()
      : text;
  }
  return "";
}

/**
 * Merge transcript pages without duplicating a message.
 *
 * The initial load returns the newest page, "load earlier" prepends an older
 * page and the live poll appends everything after the watermark; a message can
 * therefore arrive in two of them. Entries are deduplicated by `seq` and kept in
 * ascending order so wake grouping never depends on arrival order.
 */
export function mergeTranscriptMessages(
  existing: readonly QualityAgentSessionMessage[],
  incoming: readonly QualityAgentSessionMessage[]
): QualityAgentSessionMessage[] {
  const bySeq = new Map<string, QualityAgentSessionMessage>();
  for (const message of [...existing, ...incoming]) {
    bySeq.set(message.seq.toString(), message);
  }
  return [...bySeq.values()].sort(compareSeq);
}

/** The newest `seq` in a transcript, or zero for an empty one. */
export function lastTranscriptSeq(
  messages: readonly QualityAgentSessionMessage[]
): bigint {
  let last = 0n;
  for (const message of messages) {
    if (message.seq > last) {
      last = message.seq;
    }
  }
  return last;
}

/** The tool result answering one tool call, when the transcript has it. */
export function toolResultForCall(
  messages: readonly QualityAgentSessionMessage[],
  toolCallId: string
): QualityAgentSessionMessage | undefined {
  if (!toolCallId) {
    return undefined;
  }
  return messages.find(
    (message) =>
      message.role === QualityAgentMessageRole.TOOL &&
      message.toolCallId === toolCallId
  );
}

/**
 * Tool results whose call is missing from the transcript.
 *
 * A crash can leave a result without its assistant row (an interrupted wake), and
 * a provider may omit the call id; those rows are shown on their own instead of
 * being dropped.
 */
export function orphanToolResults(
  messages: readonly QualityAgentSessionMessage[]
): QualityAgentSessionMessage[] {
  const called = new Set<string>();
  for (const message of messages) {
    for (const call of message.toolCalls) {
      called.add(call.id);
    }
  }
  return messages.filter(
    (message) =>
      message.role === QualityAgentMessageRole.TOOL &&
      !called.has(message.toolCallId)
  );
}

/**
 * A tool call's arguments as one readable line: JSON objects become
 * `key: value` pairs, anything else stays as it was written. Empty for a call
 * without arguments.
 */
export function toolCallArgumentsPreview(
  call: QualityToolCall,
  maxLength = TOOL_ARGUMENTS_PREVIEW_LENGTH
): string {
  const raw = call.arguments.trim();
  if (!raw || raw === "{}") {
    return "";
  }
  return truncateText(formatArguments(raw), maxLength);
}

function formatArguments(raw: string): string {
  try {
    const parsed: unknown = JSON.parse(raw);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      const entries = Object.entries(parsed as Record<string, unknown>);
      if (entries.length > 0) {
        return entries
          .map(([key, value]) => `${key}: ${formatArgumentValue(value)}`)
          .join(", ");
      }
    }
  } catch {
    // Not JSON: a provider may pass a plain string, which is shown as written.
  }
  return raw;
}

function formatArgumentValue(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }
  const serialized = JSON.stringify(value);
  return serialized === undefined ? String(value) : serialized;
}

/** Rune-safe truncation, so a cut never splits a character. */
export function truncateText(text: string, maxLength: number): string {
  const characters = Array.from(text);
  if (characters.length <= maxLength) {
    return text;
  }
  return `${characters.slice(0, Math.max(0, maxLength - 1)).join("")}…`;
}

// ---- tokens ----

/**
 * A token count in the compact form the run panel has room for: `1.2K`,
 * `12.3M`. Below a thousand the exact number is shown, because the reader is
 * looking at a small, meaningful figure.
 */
export function formatTokens(
  value: bigint | number,
  locale = DEFAULT_LOCALE
): string {
  const count = Number(value);
  if (!Number.isFinite(count)) {
    return formatRowCount(0, locale);
  }
  if (Math.abs(count) < 1000) {
    return formatRowCount(count, locale);
  }
  return new Intl.NumberFormat(locale, {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(count);
}

// ---- target display ----

/** A session's target name: the object's own name, or its GUID as a fallback. */
export function agentSessionTargetName(session: QualityAgentSession): string {
  return session.target?.path?.object || session.target?.metaGuid || "-";
}

/** A session's target path for a subtitle: `schema.orders` or `db.schema.orders`. */
export function agentSessionTargetPath(session: QualityAgentSession): string {
  return formatTargetPath(session.target?.path);
}

/** Whether a session matches a free-text search over its target and database. */
export function agentSessionMatchesSearch(
  session: QualityAgentSession,
  query: string
): boolean {
  const term = query.trim().toLowerCase();
  if (!term) {
    return true;
  }
  const path = session.target?.path;
  const haystack = [
    path?.instance,
    path?.database,
    path?.schema,
    path?.object,
    session.target?.metaGuid,
    session.database,
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  return haystack.includes(term);
}

function compareSeq(
  left: QualityAgentSessionMessage,
  right: QualityAgentSessionMessage
): number {
  if (left.seq === right.seq) {
    return 0;
  }
  return left.seq < right.seq ? -1 : 1;
}
