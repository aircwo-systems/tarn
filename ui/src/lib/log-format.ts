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

/** Recursively unescape JSON string values that are themselves JSON. */
export function deepUnescapeJSON(val: unknown): unknown {
  if (typeof val === "string") {
    const t = val.trim();
    if (t.startsWith("{") || t.startsWith("[")) {
      try {
        return deepUnescapeJSON(JSON.parse(t));
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
  return looksLikeJSON(text)
    ? highlightJSON(text, searchPattern)
    : highlightJavaToString(text, searchPattern);
}
