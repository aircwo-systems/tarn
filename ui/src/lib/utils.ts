import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import type { Snippet } from "svelte";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatBytes(bytes: number): string {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value.toFixed(value >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatDate(value: string): string {
  if (!value) return "--";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "--";
  return `${date.toLocaleDateString()} ${date.toLocaleTimeString()}`;
}

/** Compact relative time: "just now", "42s ago", "5m ago", "3h ago", then a date. */
export function timeAgo(value: string | undefined, now = Date.now()): string {
  if (!value) return "--";
  const t = new Date(value).getTime();
  if (Number.isNaN(t)) return "--";
  const diff = now - t;
  if (diff < 2000) return "just now";
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return new Date(t).toLocaleDateString();
}

export function formatUnixSeconds(value: number): string {
  if (!value) return "--";
  return formatDate(new Date(value * 1000).toISOString());
}

export type WithoutChildrenOrChild<T> = Omit<T, "children">;

export type WithoutChild<T> = Omit<T, "child">;

export type WithElementRef<T> = T & {
  ref?: Element | null;
  children?: Snippet;
};
