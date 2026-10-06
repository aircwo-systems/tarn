import type { Tone } from "$lib/components/rack/rc-tone-pill.svelte";
import type { CognitoUser, CognitoUserStatus } from "$lib/types";

/** The name to lead with. Pools that sign in with email or phone store the
 * user's sub as the username, which says nothing to a reader. */
export function displayName(u: Pick<CognitoUser, "username" | "sub" | "email" | "phone">): string {
  if (u.username === u.sub) return u.email || u.phone || u.username;
  return u.username;
}

export function statusTone(status: CognitoUserStatus): Tone {
  switch (status) {
    case "CONFIRMED":
      return "green";
    case "UNCONFIRMED":
    case "FORCE_CHANGE_PASSWORD":
      return "amber";
    case "RESET_REQUIRED":
      return "red";
    default:
      return "neutral";
  }
}

export function statusLabel(status: CognitoUserStatus): string {
  switch (status) {
    case "FORCE_CHANGE_PASSWORD":
      return "force change password";
    case "RESET_REQUIRED":
      return "reset required";
    default:
      return status.toLowerCase();
  }
}

/** Human label for a pending code's purpose, as the backend names them. */
export function purposeLabel(purpose: string, attribute?: string): string {
  switch (purpose) {
    case "signup":
      return "Sign-up confirmation";
    case "reset":
      return "Password reset";
    case "invite":
      return "Temporary password";
    case "mfa":
      return attribute === "email" ? "Email MFA" : "SMS MFA";
    default:
      if (purpose.startsWith("verify:")) {
        return purpose.slice(7) === "phone_number" ? "Verify phone" : "Verify email";
      }
      return purpose;
  }
}

/** "in 23h", "in 4m", or "expired". */
export function expiresIn(value: string, now = Date.now()): string {
  const diff = new Date(value).getTime() - now;
  if (Number.isNaN(diff)) return "--";
  if (diff <= 0) return "expired";
  if (diff < 60_000) return `in ${Math.ceil(diff / 1000)}s`;
  if (diff < 3_600_000) return `in ${Math.round(diff / 60_000)}m`;
  if (diff < 86_400_000) return `in ${Math.round(diff / 3_600_000)}h`;
  return `in ${Math.round(diff / 86_400_000)}d`;
}

/** The function name from a Lambda ARN, for compact trigger lists. */
export function functionName(arn: string): string {
  const i = arn.indexOf(":function:");
  return i < 0 ? arn : arn.slice(i + 10).split(":")[0];
}

/** A copy-to-clipboard helper that remembers which field was copied last. */
export function createCopier() {
  let copied = $state<string | null>(null);
  let timer: ReturnType<typeof setTimeout> | undefined;
  return {
    get copied() {
      return copied;
    },
    async copy(text: string, field: string) {
      try {
        await navigator.clipboard.writeText(text);
        copied = field;
        clearTimeout(timer);
        timer = setTimeout(() => (copied = null), 1600);
      } catch {}
    },
  };
}
