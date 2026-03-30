export interface TopologyCanvasPalette {
  isDark: boolean;
  background: string;
  foreground: string;
  mutedForeground: string;
  border: string;
  popover: string;
  muted: string;
  primary: string;
  destructive: string;
  warning: string;
  chart1: string;
  chart2: string;
  chart4: string;
  chart5: string;
  blue: string;
  gateway: string;
  externalPostgresql: string;
  externalMysql: string;
  externalRedis: string;
  externalHttp: string;
  externalMongodb: string;
  externalDocker: string;
  externalDefault: string;
}

const FALLBACK_PALETTE: TopologyCanvasPalette = {
  isDark: true,
  background: "#0a0f14",
  foreground: "#edf3f8",
  mutedForeground: "#8a9bb0",
  border: "rgba(186, 210, 230, 0.2)",
  popover: "#111820",
  muted: "#161d26",
  primary: "#00e5a0",
  destructive: "#fb7185",
  warning: "#f59e0b",
  chart1: "#4ade80",
  chart2: "#22d3ee",
  chart4: "#f59e0b",
  chart5: "#60a5fa",
  blue: "#60a5fa",
  gateway: "#facc15",
  externalPostgresql: "#38bdf8",
  externalMysql: "#2dd4bf",
  externalRedis: "#f97316",
  externalHttp: "#a78bfa",
  externalMongodb: "#34d399",
  externalDocker: "#60a5fa",
  externalDefault: "#94a3b8",
};

export function readTopologyCanvasPalette(): TopologyCanvasPalette {
  if (typeof window === "undefined") return FALLBACK_PALETTE;

  const styles = getComputedStyle(document.documentElement);
  const isDark = document.documentElement.classList.contains("dark");

  return {
    isDark,
    background: readVar(styles, "--color-background", FALLBACK_PALETTE.background),
    foreground: readVar(styles, "--color-foreground", FALLBACK_PALETTE.foreground),
    mutedForeground: readVar(styles, "--color-muted-foreground", FALLBACK_PALETTE.mutedForeground),
    border: readVar(styles, "--color-border", FALLBACK_PALETTE.border),
    popover: readVar(styles, "--color-popover", FALLBACK_PALETTE.popover),
    muted: readVar(styles, "--color-muted", FALLBACK_PALETTE.muted),
    primary: readVar(styles, "--color-primary", FALLBACK_PALETTE.primary),
    destructive: readVar(styles, "--color-destructive", FALLBACK_PALETTE.destructive),
    warning: readVar(styles, "--color-amber", FALLBACK_PALETTE.warning),
    chart1: readVar(styles, "--color-chart-1", FALLBACK_PALETTE.chart1),
    chart2: readVar(styles, "--color-chart-2", FALLBACK_PALETTE.chart2),
    chart4: readVar(styles, "--color-chart-4", FALLBACK_PALETTE.chart4),
    chart5: readVar(styles, "--color-chart-5", FALLBACK_PALETTE.chart5),
    blue: readVar(styles, "--color-blue", FALLBACK_PALETTE.blue),
    gateway: readVar(styles, "--topology-gateway", FALLBACK_PALETTE.gateway),
    externalPostgresql: readVar(styles, "--topology-external-postgresql", FALLBACK_PALETTE.externalPostgresql),
    externalMysql: readVar(styles, "--topology-external-mysql", FALLBACK_PALETTE.externalMysql),
    externalRedis: readVar(styles, "--topology-external-redis", FALLBACK_PALETTE.externalRedis),
    externalHttp: readVar(styles, "--topology-external-http", FALLBACK_PALETTE.externalHttp),
    externalMongodb: readVar(styles, "--topology-external-mongodb", FALLBACK_PALETTE.externalMongodb),
    externalDocker: readVar(styles, "--topology-external-docker", FALLBACK_PALETTE.externalDocker),
    externalDefault: readVar(styles, "--topology-external-default", FALLBACK_PALETTE.externalDefault),
  };
}

function readVar(styles: CSSStyleDeclaration, name: string, fallback: string): string {
  const value = styles.getPropertyValue(name).trim();
  return value || fallback;
}

/**
 * Returns the canonical brand color for a topology node kind.
 * Avoid palette.destructive for structural connections — red is reserved for errors.
 */
export function kindColor(kind: string, palette: TopologyCanvasPalette): string {
  switch (kind) {
    case "gateway":     return palette.gateway;
    case "eventbridge": return palette.blue;       // blue
    case "topic":       return "#a855f7";          // purple (hardcoded — chart vars are oklch green)
    case "queue":       return palette.warning;    // amber
    case "function":    return palette.chart1;     // green
    case "secret":      return palette.chart2;     // cyan
    case "bucket":      return "#94a3b8";          // slate
    case "extension":   return palette.chart2;     // cyan (same as secret)
    default:            return palette.primary;
  }
}

export function normalizeTopologyExternalKind(kind: string): string {
  switch (kind?.toLowerCase()) {
    case "postgres":
    case "postgresql":
      return "postgresql";
    case "mysql":
      return "mysql";
    case "redis":
      return "redis";
    case "http":
    case "https":
      return "http";
    case "mongo":
    case "mongodb":
      return "mongodb";
    case "docker":
      return "docker";
    default:
      return "default";
  }
}

export function externalKindColor(kind: string, palette: TopologyCanvasPalette): string {
  switch (normalizeTopologyExternalKind(kind)) {
    case "postgresql": return palette.externalPostgresql;
    case "mysql": return palette.externalMysql;
    case "redis": return palette.externalRedis;
    case "http": return palette.externalHttp;
    case "mongodb": return palette.externalMongodb;
    case "docker": return palette.externalDocker;
    default: return palette.externalDefault;
  }
}

export function externalKindCssVar(kind: string): string {
  switch (normalizeTopologyExternalKind(kind)) {
    case "postgresql": return "var(--topology-external-postgresql)";
    case "mysql": return "var(--topology-external-mysql)";
    case "redis": return "var(--topology-external-redis)";
    case "http": return "var(--topology-external-http)";
    case "mongodb": return "var(--topology-external-mongodb)";
    case "docker": return "var(--topology-external-docker)";
    default: return "var(--topology-external-default)";
  }
}

export const normalizeTopologyInfraKind = normalizeTopologyExternalKind;
export const infraKindColor = externalKindColor;
export const infraKindCssVar = externalKindCssVar;
