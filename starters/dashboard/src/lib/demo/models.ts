import type { Tone } from "$lib/components/StatusBadge.svelte";

export type WorkerStatus = "running" | "idle" | "paused" | "error";
export interface Worker {
  id: string;
  name: string;
  pool: string;
  status: WorkerStatus;
  concurrency: number;
  completed: number;
  latencyMs: number;
  currentTask: string | null;
  history: readonly number[];
}
export interface Activity {
  id: string;
  timestamp: string;
  level: "info" | "success" | "warning" | "error";
  resource: string;
  message: string;
}
export const statusTone = {
  running: "success",
  idle: "neutral",
  paused: "warning",
  error: "danger",
} satisfies Record<WorkerStatus, Tone>;

export const activityTone = {
  info: "info",
  success: "success",
  warning: "warning",
  error: "danger",
} satisfies Record<Activity["level"], Tone>;
