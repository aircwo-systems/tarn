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
  };
}

function readVar(styles: CSSStyleDeclaration, name: string, fallback: string): string {
  const value = styles.getPropertyValue(name).trim();
  return value || fallback;
}
