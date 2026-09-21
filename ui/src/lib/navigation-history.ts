export const DASHBOARD_TABS = [
  "overview",
  "gateways",
  "chaos",
  "functions",
  "ecs",
  "queues",
  "dynamodb",
  "sns",
  "secrets",
  "triggers",
  "eventbridge",
  "stepfunctions",
  "storage",
  "services",
  "logs",
  "xray",
  "stack",
  "settings",
] as const;

export type DashboardTab = (typeof DASHBOARD_TABS)[number];

export type LogSortOrder = "desc" | "asc";

export interface LogNavigationFilters {
  levels: string[];
  pattern: string;
  stream: string;
  order: LogSortOrder;
}

export interface LogNavigationState extends LogNavigationFilters {
  group: string;
  timestamp: string;
}

const EMPTY_LOG_STATE: LogNavigationState = {
  group: "",
  timestamp: "",
  levels: [],
  pattern: "",
  stream: "",
  order: "desc",
};

export function tabFromHash(hash: string): DashboardTab | null {
  const tab = hash.replace(/^#/, "").split("?", 1)[0];
  return DASHBOARD_TABS.find((candidate) => candidate === tab) ?? null;
}

export function logStateFromLocation(hash: string): LogNavigationState {
  if (tabFromHash(hash) !== "logs") return { ...EMPTY_LOG_STATE, levels: [] };

  const query = hash.split("?").slice(1).join("?");
  const params = new URLSearchParams(query);
  return {
    group: params.get("groups") ?? params.get("group") ?? "",
    timestamp: params.get("ts") ?? "",
    levels: (params.get("level") ?? "").split(",").filter(Boolean),
    pattern: params.get("pattern") ?? "",
    stream: params.get("stream") ?? "",
    order: params.get("order") === "asc" ? "asc" : "desc",
  };
}

export function logLocationWithFilters(
  hash: string,
  filters: LogNavigationFilters,
): string {
  const query = hash.split("?").slice(1).join("?");
  const params = new URLSearchParams(query);

  if (filters.levels.length > 0) params.set("level", filters.levels.join(","));
  else params.delete("level");

  if (filters.pattern) params.set("pattern", filters.pattern);
  else params.delete("pattern");

  if (filters.stream) params.set("stream", filters.stream);
  else params.delete("stream");

  if (filters.order === "asc") params.set("order", filters.order);
  else params.delete("order");

  const nextQuery = params.toString();
  return nextQuery ? `#logs?${nextQuery}` : "#logs";
}

export class TabNavigationHistory {
  private readonly locations = new Map<DashboardTab, string>();

  remember(hash: string): void {
    const tab = tabFromHash(hash);
    if (!tab) return;
    this.locations.set(tab, hash.startsWith("#") ? hash : `#${hash}`);
  }

  destination(tab: DashboardTab): string {
    return this.locations.get(tab) ?? `#${tab}`;
  }
}
