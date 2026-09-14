import type { Tone } from "$lib/components/rack/rc-tone-pill.svelte";
import type {
  ECSClusterSummary,
  ECSOverview,
  ECSServiceSummary,
  ECSTaskDefinitionSummary,
  ECSTaskSummary,
} from "$lib/types";

export type EcsSelection =
  | { kind: "cluster"; resource: ECSClusterSummary }
  | { kind: "service"; resource: ECSServiceSummary }
  | { kind: "task"; resource: ECSTaskSummary };

export function ecsKey(kind: EcsSelection["kind"], arn: string): string {
  return `${kind}:${arn}`;
}

export function tail(value?: string): string {
  const trimmed = value?.trim();
  if (!trimmed) return "--";
  return trimmed.split("/").pop() || trimmed;
}

export function clusterLabel(c: ECSClusterSummary): string {
  return c.name?.trim() || tail(c.arn);
}

export function serviceLabel(s: ECSServiceSummary): string {
  return s.name?.trim() || tail(s.arn);
}

export function taskLabel(t: ECSTaskSummary): string {
  return tail(t.arn).slice(0, 12);
}

export function statusTone(status?: string): Tone {
  const s = status?.trim().toUpperCase() ?? "";
  if (["ACTIVE", "RUNNING", "SUCCEEDED"].includes(s)) return "green";
  if (["PROVISIONING", "PENDING", "ACTIVATING", "DEACTIVATING", "STOPPING", "DEPROVISIONING", "DRAINING"].includes(s)) {
    return "amber";
  }
  if (["STOPPED", "FAILED", "ERROR"].includes(s)) return "red";
  return "neutral";
}

export function findTaskDefinition(ecs: ECSOverview | undefined, arn?: string): ECSTaskDefinitionSummary | null {
  if (!arn) return null;
  return ecs?.taskDefinitions?.find((d) => d.arn === arn || d.taskDefinitionArn === arn) ?? null;
}

export function taskDefinitionLabel(ecs: ECSOverview | undefined, arn?: string): string {
  const def = findTaskDefinition(ecs, arn);
  return def ? `${def.family}:${def.revision}` : tail(arn);
}

/** Task definition family, from the summary or parsed out of the ARN tail ("family:rev"). */
export function taskFamily(ecs: ECSOverview | undefined, arn?: string): string | null {
  const def = findTaskDefinition(ecs, arn);
  if (def?.family) return def.family;
  const t = tail(arn);
  return t === "--" ? null : t.split(":")[0];
}

// Matches the backend default (resolveLogGroup in internal/ecs/runner.go); a custom
// awslogs-group on the container definition isn't in the overview payload.
export function logGroupFor(ecs: ECSOverview | undefined, taskDefinitionArn?: string): string | null {
  const family = taskFamily(ecs, taskDefinitionArn);
  return family ? `/ecs/${family}` : null;
}

export function tasksForService(tasks: ECSTaskSummary[], service: ECSServiceSummary): ECSTaskSummary[] {
  return tasks.filter((t) => t.clusterArn === service.clusterArn && t.group === `service:${service.name}`);
}

export function isServiceTask(task: ECSTaskSummary): boolean {
  return task.group?.startsWith("service:") ?? false;
}
