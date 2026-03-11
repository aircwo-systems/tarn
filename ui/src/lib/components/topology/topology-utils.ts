export function fanPositions(
  cx: number,
  count: number,
  baseY: number,
  spacing: number,
): { x: number; y: number }[] {
  if (count === 0) return [];
  const totalWidth = (count - 1) * spacing;
  const startX = cx - totalWidth / 2;
  return Array.from({ length: count }, (_, i) => ({
    x: startX + i * spacing,
    y: baseY,
  }));
}

export function trimLabel(label: string, max = 14): string {
  if (label.length <= max) return label;
  return `${label.slice(0, max - 1)}…`;
}

export function stateColor(state: string): "green" | "amber" | "red" | "gray" {
  const s = state.toLowerCase();
  if (s === "active" || s === "running") return "green";
  if (s === "pending") return "amber";
  if (s === "failed" || s === "inactive") return "red";
  return "gray";
}

export const ledColorMap: Record<string, string> = {
  green: "var(--color-accent)",
  amber: "var(--color-amber)",
  red: "var(--color-red)",
  gray: "var(--color-text-faint)",
};

// laneAwarePath, infraLadderPath can be added here as needed
