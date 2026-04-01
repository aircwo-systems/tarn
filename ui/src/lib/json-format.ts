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

function escapeHTML(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

const H = {
  key: "color:var(--lh-key)",
  null_: "color:var(--lh-null);font-style:italic",
  bool: "color:var(--lh-bool)",
  num: "color:var(--lh-num)",
  punct: "color:var(--lh-punct)",
  str: "color:var(--lh-str-json)",
};

function jsonValue(raw: string): string {
  const hasComma = raw.endsWith(",");
  const value = hasComma ? raw.slice(0, -1) : raw;
  const comma = hasComma ? `<span style="${H.punct}">,</span>` : "";

  if (value === "null") return `<span style="${H.null_}">null</span>` + comma;
  if (value === "true" || value === "false") {
    return `<span style="${H.bool}">${escapeHTML(value)}</span>` + comma;
  }
  if (/^-?\d+(\.\d+)?([Ee][+-]?\d+)?$/.test(value)) {
    return `<span style="${H.num}">${escapeHTML(value)}</span>` + comma;
  }
  if (value === "{" || value === "[") {
    return `<span style="${H.punct}">${escapeHTML(value)}</span>` + comma;
  }
  if (value.startsWith('"') && value.endsWith('"')) {
    const inner = value.slice(1, -1);
    return `<span style="${H.str}">"${escapeHTML(inner)}"</span>` + comma;
  }
  return escapeHTML(value) + comma;
}

export function highlightJSON(text: string): string {
  return text
    .split("\n")
    .map((line) => {
      const match = line.match(/^(\s*)([\s\S]*)$/);
      const indent = match?.[1] ?? "";
      const content = match?.[2] ?? "";

      if (!content) return escapeHTML(indent);
      if (/^[{}\[\]],?$/.test(content)) {
        return (
          escapeHTML(indent) +
          `<span style="${H.punct}">${escapeHTML(content)}</span>`
        );
      }

      const keyValue = content.match(/^("(?:[^"\\]|\\.)*")(\s*:\s*)([\s\S]*)$/);
      if (keyValue) {
        const keyInner = keyValue[1].slice(1, -1);
        return (
          escapeHTML(indent) +
          `<span style="${H.key}">"${escapeHTML(keyInner)}"</span>` +
          `<span style="${H.punct}">${escapeHTML(keyValue[2])}</span>` +
          jsonValue(keyValue[3])
        );
      }

      return escapeHTML(indent) + jsonValue(content);
    })
    .join("\n");
}

export function formatJSONForViewer(
  value: string,
): { formatted: string; formattedHtml: string } | null {
  const formatted = formatJSONOrNull(value);
  if (formatted === null) return null;

  return {
    formatted,
    formattedHtml: highlightJSON(formatted),
  };
}
