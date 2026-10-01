import { describeSchedule } from "$lib/eventbridge-schedule";

/** Validate the form's definition; the backend validates event matching operators. */
export function ruleDefinitionError({
  scheduleExpression,
  eventPattern,
}: {
  scheduleExpression: string;
  eventPattern: string;
}): string | null {
  const schedule = scheduleExpression.trim();
  const pattern = eventPattern.trim();
  if (!schedule && !pattern) return "Enter a schedule or an event pattern.";
  if (schedule && pattern) return "Use either a schedule or an event pattern, not both.";
  if (schedule) {
    const info = describeSchedule(schedule);
    return info.kind === "invalid" ? info.label : null;
  }
  try {
    const value: unknown = JSON.parse(pattern);
    if (value === null || typeof value !== "object" || Array.isArray(value))
      return "Event pattern must be a JSON object.";
    return null;
  } catch {
    return "Event pattern must be valid JSON.";
  }
}
