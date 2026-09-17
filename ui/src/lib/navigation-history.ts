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
  "settings",
] as const;

export type DashboardTab = (typeof DASHBOARD_TABS)[number];

export function tabFromHash(hash: string): DashboardTab | null {
  const tab = hash.replace(/^#/, "").split("?", 1)[0];
  return DASHBOARD_TABS.find((candidate) => candidate === tab) ?? null;
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
