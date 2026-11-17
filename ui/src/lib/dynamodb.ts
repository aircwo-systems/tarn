import type { DynamoAttributeValue, DynamoItem, DynamoKeySchemaItem } from "#lib/types";

export const DYNAMO_VIEWER_CHANNEL = "tarn-dynamodb-viewer";

export function notifyDynamoMutation(tableName: string) {
  try {
    const ch = new BroadcastChannel(DYNAMO_VIEWER_CHANNEL);
    ch.postMessage({ type: "items-mutated", tableName, at: Date.now() });
    ch.close();
  } catch {
    // BroadcastChannel may be unavailable in restricted contexts.
  }
}

function unmarshalAttr(av: DynamoAttributeValue | undefined): unknown {
  if (!av || typeof av !== "object") return undefined;
  if ("S" in av && av.S !== undefined) return av.S;
  if ("N" in av && av.N !== undefined) {
    const num = Number(av.N);
    return Number.isSafeInteger(num) || (!Number.isNaN(num) && String(num) === av.N) ? num : av.N;
  }
  if ("BOOL" in av && av.BOOL !== undefined) return av.BOOL;
  if ("NULL" in av && av.NULL) return null;
  if ("B" in av && av.B !== undefined) return av.B;
  if ("SS" in av && Array.isArray(av.SS)) return av.SS;
  if ("NS" in av && Array.isArray(av.NS)) return av.NS.map((n) => Number(n));
  if ("BS" in av && Array.isArray(av.BS)) return av.BS;
  if ("L" in av && Array.isArray(av.L)) return av.L.map((v) => unmarshalAttr(v));
  if ("M" in av && av.M && typeof av.M === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(av.M)) {
      out[k] = unmarshalAttr(v);
    }
    return out;
  }
  return av;
}

export function unmarshalItem(item: DynamoItem | null | undefined): Record<string, unknown> {
  if (!item || typeof item !== "object") return {};
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(item)) {
    out[k] = unmarshalAttr(v);
  }
  return out;
}

function marshalAttr(val: unknown): DynamoAttributeValue {
  if (val === null || val === undefined) return { NULL: true };
  if (typeof val === "string") return { S: val };
  if (typeof val === "number") return { N: Number.isFinite(val) ? String(val) : "0" };
  if (typeof val === "boolean") return { BOOL: val };
  if (Array.isArray(val)) return { L: val.map((v) => marshalAttr(v)) };
  if (typeof val === "object") {
    const m: Record<string, DynamoAttributeValue> = {};
    for (const [k, v] of Object.entries(val as Record<string, unknown>)) {
      if (v !== undefined) {
        m[k] = marshalAttr(v);
      }
    }
    return { M: m };
  }
  return { S: String(val) };
}

export function marshalItem(obj: Record<string, unknown>): DynamoItem {
  const out: DynamoItem = {};
  for (const [k, v] of Object.entries(obj)) {
    if (v !== undefined) {
      out[k] = marshalAttr(v);
    }
  }
  return out;
}

export function marshalScalar(raw: string, attrType: string): DynamoAttributeValue {
  const t = attrType.toUpperCase();
  if (t === "N") return { N: raw.trim() };
  if (t === "B") return { B: raw.trim() };
  return { S: raw };
}

export function attrTypeTag(av: DynamoAttributeValue | undefined): string {
  if (!av || typeof av !== "object") return "?";
  if ("S" in av && av.S !== undefined) return "S";
  if ("N" in av && av.N !== undefined) return "N";
  if ("BOOL" in av && av.BOOL !== undefined) return "BOOL";
  if ("NULL" in av && av.NULL) return "NULL";
  if ("B" in av && av.B !== undefined) return "B";
  if ("M" in av && av.M !== undefined) return "M";
  if ("L" in av && av.L !== undefined) return "L";
  if ("SS" in av && av.SS !== undefined) return "SS";
  if ("NS" in av && av.NS !== undefined) return "NS";
  if ("BS" in av && av.BS !== undefined) return "BS";
  return "?";
}

export function formatAttrPreview(av: DynamoAttributeValue | undefined): string {
  if (!av || typeof av !== "object") return "";
  if ("S" in av && av.S !== undefined) return av.S;
  if ("N" in av && av.N !== undefined) return av.N;
  if ("BOOL" in av && av.BOOL !== undefined) return av.BOOL ? "true" : "false";
  if ("NULL" in av && av.NULL) return "null";
  if ("B" in av && av.B !== undefined) return `<binary ${av.B.length}b>`;
  if ("SS" in av && Array.isArray(av.SS)) return `[${av.SS.join(", ")}]`;
  if ("NS" in av && Array.isArray(av.NS)) return `[${av.NS.join(", ")}]`;
  if ("BS" in av && Array.isArray(av.BS)) return `<${av.BS.length} binary>`;
  if ("L" in av && Array.isArray(av.L)) return `[${av.L.length} items]`;
  if ("M" in av && av.M) {
    const keys = Object.keys(av.M);
    return keys.length === 0 ? "{}" : `{${keys.slice(0, 3).join(", ")}${keys.length > 3 ? ", ..." : ""}}`;
  }
  return JSON.stringify(av);
}

export function extractItemKey(item: DynamoItem, keySchema: DynamoKeySchemaItem[]): DynamoItem {
  const key: DynamoItem = {};
  for (const ks of keySchema) {
    if (item[ks.attributeName] !== undefined) {
      key[ks.attributeName] = item[ks.attributeName];
    }
  }
  return key;
}

export function itemKeySignature(item: DynamoItem, keySchema: DynamoKeySchemaItem[], fallbackIndex = 0): string {
  const parts: string[] = [];
  let complete = keySchema.length > 0;
  for (const ks of keySchema) {
    const av = item[ks.attributeName];
    if (av === undefined) complete = false;
    parts.push(`${ks.attributeName}=${av ? JSON.stringify(av) : "?"}`);
  }
  return complete ? parts.join("|") : `${parts.join("|")}#${fallbackIndex}`;
}

export function collectColumnNames(items: DynamoItem[], keySchema: DynamoKeySchemaItem[]): string[] {
  const cols: string[] = [];
  const seen = new Set<string>();
  for (const ks of keySchema) {
    if (!seen.has(ks.attributeName)) {
      seen.add(ks.attributeName);
      cols.push(ks.attributeName);
    }
  }
  const extra: string[] = [];
  for (const item of items) {
    for (const k of Object.keys(item)) {
      if (!seen.has(k)) {
        seen.add(k);
        extra.push(k);
      }
    }
  }
  extra.sort();
  return [...cols, ...extra];
}

export function dynamoStateTone(value?: string): "green" | "amber" | "red" | "neutral" {
  const upper = (value ?? "").toUpperCase();
  if (upper === "ACTIVE" || upper === "ENABLED" || upper === "AVAILABLE") return "green";
  if (
    upper.includes("CREATING") ||
    upper.includes("UPDATING") ||
    upper.includes("ENABLING") ||
    upper.includes("ARCHIVING")
  ) {
    return "amber";
  }
  if (
    upper.includes("DELET") ||
    upper.includes("FAIL") ||
    upper.includes("DISABLED") ||
    upper.includes("ERROR") ||
    upper === "INACTIVE"
  ) {
    return "red";
  }
  return "neutral";
}
