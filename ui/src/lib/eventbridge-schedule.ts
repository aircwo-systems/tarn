const UNIT_MS: Record<string, number> = {
  minute: 60_000,
  minutes: 60_000,
  hour: 3_600_000,
  hours: 3_600_000,
  day: 86_400_000,
  days: 86_400_000,
};

export interface ScheduleInfo {
  kind: "rate" | "cron" | "pattern" | "invalid";
  /** plain-language reading, e.g. "Every 5 minutes" */
  label: string;
  /** fixed interval for rate() expressions */
  intervalMs?: number;
}

export function describeSchedule(expr: string): ScheduleInfo {
  const value = expr.trim();
  if (!value) return { kind: "pattern", label: "Event pattern" };

  const rate = /^rate\(\s*(\d+)\s+(minutes?|hours?|days?)\s*\)$/i.exec(value);
  if (rate) {
    const n = Number(rate[1]);
    const unit = rate[2].toLowerCase().replace(/s$/, "");
    if (n < 1) return { kind: "invalid", label: "Rate must be at least 1" };
    return {
      kind: "rate",
      label: n === 1 ? `Every ${unit}` : `Every ${n} ${unit}s`,
      intervalMs: n * UNIT_MS[unit],
    };
  }

  const cron = /^cron\((.+)\)$/i.exec(value);
  if (cron) {
    const fields = cron[1].trim().split(/\s+/);
    if (fields.length !== 6) return { kind: "invalid", label: "cron() needs 6 fields" };
    return { kind: "cron", label: describeCron(fields) };
  }

  return { kind: "invalid", label: "Use rate(…) or cron(…)" };
}

function describeCron([min, hour, dom, month, dow]: string[]): string {
  const pad = (v: string) => v.padStart(2, "0");
  const plainTime = /^\d+$/.test(min) && /^\d+$/.test(hour);
  const at = plainTime ? ` at ${pad(hour)}:${pad(min)} UTC` : "";
  const everyDay = (dom === "*" || dom === "?") && (dow === "*" || dow === "?") && month === "*";

  if (/^\d+$/.test(min) && hour === "*" && everyDay) return `Hourly at :${pad(min)}`;
  if (plainTime && everyDay) return `Daily${at}`;
  if (plainTime && (dom === "?" || dom === "*") && dow !== "*" && dow !== "?") return `${dow}${at}`;
  if (plainTime && /^\d+$/.test(dom) && month === "*") return `Monthly on day ${dom}${at}`;
  return "Custom cron";
}

export const SCHEDULE_PRESETS = [
  { label: "1 min", expr: "rate(1 minute)" },
  { label: "5 min", expr: "rate(5 minutes)" },
  { label: "Hourly", expr: "rate(1 hour)" },
  { label: "Daily 09:00", expr: "cron(0 9 * * ? *)" },
  { label: "Weekdays", expr: "cron(0 9 ? * MON-FRI *)" },
];

export function functionNameFromArn(arn: string): string | null {
  const idx = arn.indexOf(":function:");
  if (idx < 0) return null;
  return arn.slice(idx + ":function:".length).split(":")[0] || null;
}

export function targetService(arn: string): string {
  const service = arn.split(":")[2];
  const labels: Record<string, string> = { lambda: "Lambda", sqs: "SQS", sns: "SNS", states: "SFN", events: "Bus" };
  return labels[service] ?? (service ? service.toUpperCase() : "ARN");
}
