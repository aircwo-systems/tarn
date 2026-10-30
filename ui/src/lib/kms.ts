import type { KmsKeyReference, KmsKeySummary } from "#lib/types";
import type { Tone } from "#lib/components/rack/rc-tone-pill.svelte";

/** The name a key is shown by: its first alias, else its key ID. */
export function kmsKeyName(key: Pick<KmsKeySummary, "aliases" | "id">): string {
  return key.aliases[0] ?? key.id;
}

export function kmsStateLabel(state: string): string {
  return state === "PendingDeletion" ? "Pending deletion" : state;
}

export function kmsStateTone(state: string): Tone {
  if (state === "Enabled") return "green";
  if (state === "Disabled") return "amber";
  if (state === "PendingDeletion") return "red";
  return "neutral";
}

export function kmsManagerLabel(manager: string): string {
  return manager === "AWS" ? "AWS managed key" : "Customer managed key";
}

/** Successful cryptographic operations since Tarn started. */
export function kmsOperationCount(key: Pick<KmsKeySummary, "usage">): number {
  const u = key.usage;
  return u.encrypt + u.decrypt + u.dataKeys + u.reEncrypt;
}

const referenceKinds: Record<string, { label: string; param: string }> = {
  function: { label: "Lambda function", param: "fn" },
  secret: { label: "Secret", param: "name" },
  queue: { label: "SQS queue", param: "queue" },
  topic: { label: "SNS topic", param: "topic" },
  userpool: { label: "User pool", param: "pool" },
};

export function kmsReferenceLabel(ref: KmsKeyReference): string {
  return referenceKinds[ref.kind]?.label ?? ref.kind;
}

/** The dashboard location that selects the referencing resource. */
export function kmsReferenceTarget(ref: KmsKeyReference): string {
  const param = referenceKinds[ref.kind]?.param;
  if (!param) return ref.tab;
  return `${ref.tab}?${new URLSearchParams({ [param]: ref.id || ref.name })}`;
}

/** Matches a key against a list filter by alias, key ID, ARN or description. */
export function kmsKeyMatches(key: KmsKeySummary, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return [key.id, key.arn, key.description ?? "", ...key.aliases].some((v) =>
    v.toLowerCase().includes(q),
  );
}

/** Whether ref (a key ID, key ARN, alias name or alias ARN) names key. */
export function kmsKeyIsNamed(
  key: Pick<KmsKeySummary, "id" | "arn" | "aliases">,
  ref: string,
): boolean {
  return (
    ref === key.id || ref === key.arn || key.aliases.some((a) => ref === a || ref.endsWith(`:${a}`))
  );
}
