function formatJSONOrNull(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return null;

  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return null;
  }
}

export function isJSONContentType(contentType: string): boolean {
  const normalized = contentType.toLowerCase();
  return normalized.includes("/json") || normalized.includes("+json");
}

export function escapeHTML(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

export interface SyntaxToken {
  text: string;
  style?: string;
}

export const H = {
  key: "color:var(--lh-key)",
  null_: "color:var(--lh-null);font-style:italic",
  bool: "color:var(--lh-bool)",
  num: "color:var(--lh-num)",
  punct: "color:var(--lh-punct)",
  str: "color:var(--lh-str-json)",
};

const PUNCT_CHARS = new Set([
  ":", ";", ",", "{", "}", "[", "]", "(", ")", "\"", "'", "`",
  "=", "<", ">", "&", "|", "/", "\\", "+", "-", "*",
]);

const TERM_RE =
  /(?:(?:(?:"(?:[^"\\]|\\.)*")|[^\s:=]+)\s*[:=]\s*(?:(?:"(?:[^"\\]|\\.)*")|\S+))|(?:"(?:[^"\\]|\\.)*")|\S+/g;

/**
 * Splits a filter pattern into individual terms, respecting quotes and key-value constructs.
 */
export function splitFilterTerms(pattern: string): string[] {
  if (!pattern || !pattern.trim()) return [];
  const matches = pattern.match(TERM_RE);
  return matches ? matches.map((s) => s.trim()).filter(Boolean) : [];
}

/**
 * Formats selected text appropriately for appending to a filter pattern.
 * If text contains spaces and is not already quoted or a key-value pair, it is quoted.
 */
export function formatFilterTerm(text: string): string {
  const trimmed = text.trim();
  if (!trimmed) return "";
  if (
    (trimmed.startsWith('"') && trimmed.endsWith('"') && trimmed.length >= 2) ||
    (trimmed.startsWith("'") && trimmed.endsWith("'") && trimmed.length >= 2)
  ) {
    return trimmed;
  }
  if (/[:=]/.test(trimmed)) {
    return trimmed;
  }
  if (/\s/.test(trimmed)) {
    return `"${trimmed.replace(/"/g, '\\"')}"`;
  }
  return trimmed;
}

function compileTermRegexSource(term: string): string | null {
  const p = term.trim();
  if (!p) return null;

  let inner = p;
  let hasOuterQuotes = false;
  if (
    inner.startsWith('"') &&
    inner.endsWith('"') &&
    inner.length >= 2 &&
    !inner.slice(1, -1).includes('"')
  ) {
    inner = inner.slice(1, -1);
    hasOuterQuotes = true;
  }

  const rawTokens = inner.split(/(\s+)/);
  const regexParts: string[] = [];

  for (let i = 0; i < rawTokens.length; i++) {
    const tok = rawTokens[i];
    if (/^\s+$/.test(tok)) {
      const prevTok = i > 0 ? rawTokens[i - 1] : "";
      const nextTok = i + 1 < rawTokens.length ? rawTokens[i + 1] : "";
      const prevPunct = prevTok.length > 0 && PUNCT_CHARS.has(prevTok[prevTok.length - 1]);
      const nextPunct = nextTok.length > 0 && PUNCT_CHARS.has(nextTok[0]);
      if (prevPunct || nextPunct) {
        regexParts.push("\\s*");
      } else {
        regexParts.push("\\s+");
      }
    } else {
      let escaped = tok.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      escaped = escaped.replace(/([:=,])/g, "\\s*$1\\s*");
      regexParts.push(escaped);
    }
  }

  const body = regexParts.join("");
  return hasOuterQuotes ? `"?${body}"?` : body;
}

/**
 * Builds a RegExp from a search pattern that matches case-insensitively and
 * tolerates whitespace variations, supporting multiple terms with OR matching
 * for syntax highlighting across tokens.
 */
export function buildFlexiblePatternRegex(pattern: string): RegExp | null {
  const terms = splitFilterTerms(pattern);
  if (terms.length === 0) return null;

  const sources = terms
    .map(compileTermRegexSource)
    .filter((s): s is string => Boolean(s));
  if (sources.length === 0) return null;

  try {
    if (sources.length === 1) {
      return new RegExp(sources[0], "gi");
    }
    return new RegExp(`(?:${sources.join("|")})`, "gi");
  } catch {
    return null;
  }
}

/**
 * Escape HTML and wrap all case-insensitive occurrences of pattern in <mark class="log-search-match">,
 * tolerating whitespace differences and highlighting multiple search terms.
 */
export function highlightSearchText(text: string, pattern?: string): string {
  if (!pattern || !pattern.trim()) return escapeHTML(text);
  const rx = buildFlexiblePatternRegex(pattern);
  if (!rx) return escapeHTML(text);

  let result = "";
  let lastIdx = 0;
  let m: RegExpExecArray | null;

  while ((m = rx.exec(text)) !== null) {
    if (m.index > lastIdx) {
      result += escapeHTML(text.slice(lastIdx, m.index));
    }
    result += `<mark class="log-search-match">${escapeHTML(m[0])}</mark>`;
    lastIdx = m.index + m[0].length;
    if (m[0].length === 0) {
      rx.lastIndex++;
    }
  }
  if (lastIdx < text.length) {
    result += escapeHTML(text.slice(lastIdx));
  }
  return result;
}

/**
 * Render a line's syntax tokens to HTML, preserving syntax styles while
 * highlighting search pattern matches across or within tokens.
 */
export function renderTokensWithHighlight(
  tokens: SyntaxToken[],
  pattern?: string,
): string {
  if (!pattern || !pattern.trim()) {
    let out = "";
    for (const token of tokens) {
      if (!token.text) continue;
      const escaped = escapeHTML(token.text);
      out += token.style ? `<span style="${token.style}">${escaped}</span>` : escaped;
    }
    return out;
  }

  const fullLine = tokens.map((t) => t.text).join("");
  const rx = buildFlexiblePatternRegex(pattern);
  if (!rx) {
    let out = "";
    for (const token of tokens) {
      if (!token.text) continue;
      const escaped = escapeHTML(token.text);
      out += token.style ? `<span style="${token.style}">${escaped}</span>` : escaped;
    }
    return out;
  }

  const matchRanges: [number, number][] = [];
  let m: RegExpExecArray | null;
  while ((m = rx.exec(fullLine)) !== null) {
    matchRanges.push([m.index, m.index + m[0].length]);
    if (m[0].length === 0) {
      rx.lastIndex++;
    }
  }

  if (matchRanges.length === 0) {
    let out = "";
    for (const token of tokens) {
      if (!token.text) continue;
      const escaped = escapeHTML(token.text);
      out += token.style ? `<span style="${token.style}">${escaped}</span>` : escaped;
    }
    return out;
  }

  let out = "";
  let linePos = 0;

  for (const token of tokens) {
    if (!token.text) continue;
    const tokenStart = linePos;
    const tokenEnd = linePos + token.text.length;
    linePos = tokenEnd;

    let tokenHtml = "";
    let offset = 0;

    for (const [mStart, mEnd] of matchRanges) {
      if (mEnd <= tokenStart || mStart >= tokenEnd) continue;
      const startInToken = Math.max(0, mStart - tokenStart);
      const endInToken = Math.min(token.text.length, mEnd - tokenStart);

      if (startInToken > offset) {
        tokenHtml += escapeHTML(token.text.slice(offset, startInToken));
      }
      tokenHtml += `<mark class="log-search-match">${escapeHTML(token.text.slice(startInToken, endInToken))}</mark>`;
      offset = endInToken;
    }

    if (offset < token.text.length) {
      tokenHtml += escapeHTML(token.text.slice(offset));
    }

    out += token.style ? `<span style="${token.style}">${tokenHtml}</span>` : tokenHtml;
  }

  return out;
}

function tokenizeJSONValue(
  raw: string,
  tokens: SyntaxToken[],
): void {
  const hasComma = raw.endsWith(",");
  const value = hasComma ? raw.slice(0, -1) : raw;

  if (value === "null") {
    tokens.push({ text: value, style: H.null_ });
  } else if (value === "true" || value === "false") {
    tokens.push({ text: value, style: H.bool });
  } else if (/^-?\d+(\.\d+)?([Ee][+-]?\d+)?$/.test(value)) {
    tokens.push({ text: value, style: H.num });
  } else if (value === "{" || value === "[") {
    tokens.push({ text: value, style: H.punct });
  } else if (value.startsWith('"') && value.endsWith('"')) {
    tokens.push({ text: value, style: H.str });
  } else {
    tokens.push({ text: value });
  }

  if (hasComma) {
    tokens.push({ text: ",", style: H.punct });
  }
}

function tokenizeJSONLine(line: string): SyntaxToken[] {
  const match = line.match(/^(\s*)([\s\S]*)$/);
  const indent = match?.[1] ?? "";
  const content = match?.[2] ?? "";

  const tokens: SyntaxToken[] = [];
  if (indent) {
    tokens.push({ text: indent });
  }
  if (!content) {
    return tokens;
  }

  if (/^[{}\[\]],?$/.test(content)) {
    tokens.push({ text: content, style: H.punct });
    return tokens;
  }

  const keyValue = content.match(/^("(?:[^"\\]|\\.)*")(\s*:\s*)([\s\S]*)$/);
  if (keyValue) {
    tokens.push({ text: keyValue[1], style: H.key });
    tokens.push({ text: keyValue[2], style: H.punct });
    tokenizeJSONValue(keyValue[3], tokens);
    return tokens;
  }

  tokenizeJSONValue(content, tokens);
  return tokens;
}

export function highlightJSON(text: string, searchPattern?: string): string {
  return text
    .split("\n")
    .map((line) => {
      const tokens = tokenizeJSONLine(line);
      return renderTokensWithHighlight(tokens, searchPattern);
    })
    .join("\n");
}

export function formatJSONForViewer(
  value: string,
  searchPattern?: string,
): { formatted: string; formattedHtml: string } | null {
  const formatted = formatJSONOrNull(value);
  if (formatted === null) return null;

  return {
    formatted,
    formattedHtml: highlightJSON(formatted, searchPattern),
  };
}
