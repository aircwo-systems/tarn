import {
  highlightJSON,
  renderTokensWithHighlight,
  type SyntaxToken,
} from "$lib/json-format";

export interface ParsedSpringBootLog {
  pid: string;
  thread: string;
  logger: string;
  message: string;
}

/** Parse Spring Boot/Log4j format: LEVEL PID --- [THREAD] LOGGER : MESSAGE */
export function parseSpringBootLog(raw: string): ParsedSpringBootLog | null {
  const m = raw.match(
    /^\w+\s+(\d+)\s+---\s+\[([^\]]+)\]\s+([\w.$]+)\s*:\s([\s\S]+)$/,
  );
  if (!m) return null;
  return { pid: m[1], thread: m[2].trim(), logger: m[3], message: m[4] };
}

export interface DatadogLog {
  message: string;
  /** Application fields as `key=value` pairs; empty when there are none. */
  extras: string;
  service?: string;
  /** Injected trace ID, decimal or 128-bit hex as the tracer wrote it. */
  traceId?: string;
  status?: number;
  durationMs?: number;
}

// Express/morgan-style access lines: "GET /path 200 12ms".
const REQUEST_LINE = /^([A-Z]+ \S+) (\d{3})(?: (\d+(?:\.\d+)?) ?ms)?$/;

// Tracer and shipper bookkeeping: the row's time, level and stream already show these.
const DATADOG_OMIT = new Set([
  "dd", "dd.trace_id", "dd.span_id", "ddsource", "ddtags", "service", "hostname",
  "host", "env", "version", "level", "status", "timestamp", "message",
]);
const datadogCache = new Map<string, DatadogLog | null>();

/**
 * Datadog-shaped JSON logs (injected `dd` context, or shipper fields such as
 * `ddsource`) reduce to their message plus any application fields.
 */
export function formatDatadogLog(raw: string): DatadogLog | null {
  const cached = datadogCache.get(raw);
  if (cached !== undefined) return cached;
  const result = parseDatadogLog(raw);
  // Rows re-render on every tick; bound the cache rather than re-parsing.
  if (datadogCache.size >= 2000) datadogCache.clear();
  datadogCache.set(raw, result);
  return result;
}

function parseDatadogLog(raw: string): DatadogLog | null {
  if (!raw.trimStart().startsWith("{")) return null;
  let doc: unknown;
  try {
    doc = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!doc || typeof doc !== "object" || Array.isArray(doc)) return null;
  const record = doc as Record<string, unknown>;
  const isDatadog =
    (typeof record.dd === "object" && record.dd !== null) ||
    "ddsource" in record || "ddtags" in record || "dd.trace_id" in record;
  if (!isDatadog || typeof record.message !== "string") return null;
  const extras: string[] = [];
  for (const [key, value] of Object.entries(record)) {
    if (DATADOG_OMIT.has(key) || value === null || value === "") continue;
    if (typeof value === "object" && Object.keys(value).length === 0) continue;
    const text = typeof value === "string" ? value : JSON.stringify(value);
    extras.push(`${key}=${typeof value === "string" && /\s/.test(text) ? JSON.stringify(text) : text}`);
  }
  const result: DatadogLog = { message: record.message, extras: extras.join(" ") };
  const dd = record.dd as Record<string, unknown> | undefined;
  const service = typeof dd?.service === "string" ? dd.service : record.service;
  if (typeof service === "string" && service) result.service = service;
  const traceId = dd?.trace_id ?? record["dd.trace_id"];
  if ((typeof traceId === "string" || typeof traceId === "number") && String(traceId)) result.traceId = String(traceId);
  const request = REQUEST_LINE.exec(record.message);
  if (request) {
    result.message = request[1];
    result.status = Number(request[2]);
    if (request[3] !== undefined) result.durationMs = Number(request[3]);
  } else {
    const http = record.http as Record<string, unknown> | undefined;
    const code = Number(http?.status_code);
    if (Number.isInteger(code) && code >= 100 && code < 600) result.status = code;
  }
  return result;
}

/**
 * Eight hex digits of a trace ID's low 64 bits, which is what Tarn correlates
 * on, so decimal and 128-bit hex forms of one trace read the same.
 */
export function shortTraceId(id: string): string {
  let low: bigint;
  if (/^[0-9a-f]{32}$/i.test(id)) low = BigInt(`0x${id.slice(16)}`);
  else if (/^\d{1,20}$/.test(id)) low = BigInt(id) & 0xffff_ffff_ffff_ffffn;
  else return "";
  return low === 0n ? "" : low.toString(16).padStart(16, "0").slice(-8);
}

export function hasJavaToString(str: string): boolean {
  return /[A-Z][a-zA-Z0-9]*\([a-z]/.test(str);
}

export function looksLikeJSON(str: string): boolean {
  const t = str.trim();
  return t.startsWith("{") || t.startsWith("[") || t.startsWith('\\{') || t.startsWith('\\"');
}

export function isComplexMessage(msg: string): boolean {
  return msg.length > 300 || hasJavaToString(msg) || looksLikeJSON(msg);
}

/**
 * Format Java toString output (Lombok/etc) into an indented, readable tree.
 * Converts `ClassName(key=value, ...)` and `{key=value}` map literals.
 */
export function formatJavaToString(str: string): string {
  let indent = 0;
  let result = "";
  for (let i = 0; i < str.length; i++) {
    const ch = str[i];
    const next = i + 1 < str.length ? str[i + 1] : "";
    if ((ch === "(" && next === ")") || (ch === "{" && next === "}")) {
      result += ch + next;
      i++; // skip the paired closing char
    } else if (ch === "(" || ch === "{") {
      result += ch + "\n";
      indent++;
      result += "  ".repeat(indent);
    } else if (ch === ")" || ch === "}") {
      indent = Math.max(0, indent - 1);
      result += "\n" + "  ".repeat(indent) + ch;
    } else if (ch === "," && next === " ") {
      result += ",\n" + "  ".repeat(indent);
      i++; // skip the trailing space
    } else if (ch === "=") {
      result += ": ";
    } else {
      result += ch;
    }
  }
  return result.trim();
}

/** Expand JSON strings and JSON payloads appended to log messages. */
export function deepUnescapeJSON(val: unknown): unknown {
  if (typeof val === "string") {
    const t = val.trim();
    if (t.startsWith("{") || t.startsWith("[")) {
      try {
        return deepUnescapeJSON(JSON.parse(t));
      } catch {
        /* look for JSON after a text prefix */
      }
    }
    const embedded = t.match(/^([\s\S]+?)\s+(\{[\s\S]*\}|\[[\s\S]*\])$/);
    if (embedded) {
      try {
        return { text: embedded[1], payload: deepUnescapeJSON(JSON.parse(embedded[2])) };
      } catch {
        return val;
      }
    }
    return val;
  }
  if (Array.isArray(val)) return val.map(deepUnescapeJSON);
  if (val && typeof val === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(val)) out[k] = deepUnescapeJSON(v);
    return out;
  }
  return val;
}

export function computeFormattedMessage(content: string): string | null {
  const invokeError = content.match(/^Invoke Error[ \t]+([\s\S]+)$/);
  if (invokeError) {
    try {
      const parsed = JSON.parse(invokeError[1]);
      if (parsed && typeof parsed === "object") {
        return `Invoke Error\n${JSON.stringify(deepUnescapeJSON(parsed), null, 2)}`;
      }
    } catch {
      /* not JSON */
    }
  }
  // JSON first — with recursive unescape of stringified nested JSON
  try {
    const parsed = JSON.parse(content);
    return JSON.stringify(deepUnescapeJSON(parsed), null, 2);
  } catch {
    /* not JSON */
  }
  // Try unescaping backslash-quoted JSON (common in log output: {\"key\":\"value\"})
  if (content.includes('\\"')) {
    const cleaned = content.replace(/\\"/g, '"');
    try {
      const parsed = JSON.parse(cleaned);
      return JSON.stringify(deepUnescapeJSON(parsed), null, 2);
    } catch {
      /* still not JSON */
    }
  }
  // Java toString
  if (hasJavaToString(content)) {
    return formatJavaToString(content);
  }
  return null;
}

// ── Inline JSON formatting for compact view ─────────────────────────
export function tryFormatInlineJSON(msg: string): {
  isJSON: boolean;
  formatted: string;
} {
  const trimmed = msg.trim();
  if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
    try {
      const parsed = JSON.parse(trimmed);
      const unescaped = deepUnescapeJSON(parsed);
      return { isJSON: true, formatted: JSON.stringify(unescaped, null, 2) };
    } catch {
      /* not JSON */
    }
  }
  // Try unescaping backslash-quoted JSON
  if (trimmed.includes('\\"')) {
    const cleaned = trimmed.replace(/\\"/g, '"');
    try {
      const parsed = JSON.parse(cleaned);
      const unescaped = deepUnescapeJSON(parsed);
      return { isJSON: true, formatted: JSON.stringify(unescaped, null, 2) };
    } catch {
      /* still not JSON */
    }
  }
  return { isJSON: false, formatted: msg };
}

// ── Syntax highlighting ─────────────────────────────────────────────

const H = {
  key: "color:var(--lh-key)",
  cls: "color:var(--lh-cls)",
  null_: "color:var(--lh-null);font-style:italic",
  bool: "color:var(--lh-bool)",
  num: "color:var(--lh-num)",
  str: "color:var(--lh-str)",
  punct: "color:var(--lh-punct)",
};

function tokenizeJtsValue(
  rawVal: string,
  tokens: SyntaxToken[],
): void {
  const hasComma = rawVal.endsWith(",");
  const v = hasComma ? rawVal.slice(0, -1) : rawVal;

  if (v === "null") {
    tokens.push({ text: v, style: H.null_ });
  } else if (v === "true" || v === "false") {
    tokens.push({ text: v, style: H.bool });
  } else if (/^-?\d+(\.\d+)?([Ee][+-]?\d+)?[fFdDlL]?$/.test(v)) {
    tokens.push({ text: v, style: H.num });
  } else {
    const cm = v.match(/^([A-Z][a-zA-Z0-9$_]*)([({].*)$/);
    if (cm) {
      tokens.push({ text: cm[1], style: H.cls });
      tokens.push({ text: cm[2], style: H.punct });
    } else if (v === "{" || v === "[") {
      tokens.push({ text: v, style: H.punct });
    } else {
      tokens.push({ text: v, style: H.str });
    }
  }

  if (hasComma) {
    tokens.push({ text: ",", style: H.punct });
  }
}

function tokenizeJavaToStringLine(line: string): SyntaxToken[] {
  const m = line.match(/^(\s*)([\s\S]*)$/);
  const indent = m?.[1] ?? "";
  const content = m?.[2] ?? "";
  const tokens: SyntaxToken[] = [];
  if (indent) tokens.push({ text: indent });
  if (!content) return tokens;

  // Closing bracket lines
  if (/^[)\]}]+,?$/.test(content)) {
    tokens.push({ text: content, style: H.punct });
    return tokens;
  }

  // key: value
  const kv = content.match(/^([a-z_$][a-zA-Z0-9_$]*)(: )([\s\S]*)$/);
  if (kv) {
    tokens.push({ text: kv[1], style: H.key });
    tokens.push({ text: kv[2], style: H.punct });
    tokenizeJtsValue(kv[3], tokens);
    return tokens;
  }

  // ClassName( or ClassName{
  const cls = content.match(/^([A-Z][a-zA-Z0-9$_]*)([({,]?.*)$/);
  if (cls) {
    tokens.push({ text: cls[1], style: H.cls });
    if (cls[2]) tokens.push({ text: cls[2], style: H.punct });
    return tokens;
  }

  if (content === "{" || content === "[") {
    tokens.push({ text: content, style: H.punct });
    return tokens;
  }

  tokens.push({ text: content });
  return tokens;
}

export function highlightJavaToString(text: string, searchPattern?: string): string {
  return text
    .split("\n")
    .map((line) => {
      const tokens = tokenizeJavaToStringLine(line);
      return renderTokensWithHighlight(tokens, searchPattern);
    })
    .join("\n");
}

export function highlightFormatted(text: string, searchPattern?: string): string {
  return looksLikeJSON(text) || text.startsWith("Invoke Error\n")
    ? highlightJSON(text, searchPattern)
    : highlightJavaToString(text, searchPattern);
}
