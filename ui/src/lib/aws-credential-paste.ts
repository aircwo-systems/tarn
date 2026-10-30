import type { ManualAWSCredentials } from "./aws-connections";

const aliases = new Map([
  ["aws_access_key_id", "accessKeyId"],
  ["accesskeyid", "accessKeyId"],
  ["aws_secret_access_key", "secretAccessKey"],
  ["secretaccesskey", "secretAccessKey"],
  ["aws_session_token", "sessionToken"],
  ["sessiontoken", "sessionToken"],
]);

export function looksLikeAWSCredentialBlock(text: string): boolean {
  return (
    /(?:AWS_ACCESS_KEY_ID|["']AccessKeyId["'])/i.test(text) &&
    /(?:AWS_SECRET_ACCESS_KEY|["']SecretAccessKey["'])/i.test(text)
  );
}

// Parse data only. Shell statements, substitutions and expressions are never run.
export function parseAWSCredentialPaste(text: string): ManualAWSCredentials {
  if (text.length > 32768) throw new Error("The credential block is too large.");
  const values = new Map<string, string>();
  const add = (name: string, value: unknown) => {
    const field = aliases.get(name.toLowerCase());
    if (!field) return;
    if (typeof value !== "string" || !value.trim())
      throw new Error("A copied AWS credential field is empty.");
    if (values.has(field))
      throw new Error(
        "The block contains duplicate AWS credential fields. Paste one credential set.",
      );
    values.set(field, value.trim());
  };
  const input = text.trim();
  if (input.startsWith("{")) {
    let parsed: unknown;
    try {
      parsed = JSON.parse(input);
    } catch {
      throw new Error("The copied AWS credential JSON is incomplete or invalid.");
    }
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed))
      throw new Error("Paste an AWS credential object.");
    for (const [key, value] of Object.entries(parsed)) add(key, value);
  } else {
    for (const line of input.split(/\r?\n/)) {
      const clean = line.trim();
      if (!clean || clean.startsWith("#") || clean.startsWith("[")) continue;
      const match = /^(?:export\s+|set\s+|\$env:)?(\w+)\s*=\s*(.*?)\s*;?$/i.exec(clean);
      if (!match)
        throw new Error("Paste the AWS console's credential assignments or credential JSON.");
      const key = match[1];
      let value = match[2] ?? "";
      if (!key) continue;
      if (value.startsWith('"') || value.startsWith("'")) {
        if (value.length < 2 || value.at(-1) !== value[0])
          throw new Error("The copied AWS credential block has an incomplete quoted value.");
        value = value.slice(1, -1);
      }
      if (/[\s`;]|\$\(/.test(value))
        throw new Error("The copied AWS credential block contains an invalid value.");
      add(key, value);
    }
  }
  const accessKeyId = values.get("accessKeyId");
  const secretAccessKey = values.get("secretAccessKey");
  if (!accessKeyId || !secretAccessKey)
    throw new Error("Paste a complete set containing AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.");
  const sessionToken = values.get("sessionToken");
  return sessionToken
    ? { kind: "temporary", accessKeyId, secretAccessKey, sessionToken }
    : { kind: "long-lived", accessKeyId, secretAccessKey };
}
