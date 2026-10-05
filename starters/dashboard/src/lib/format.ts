const numbers = new Intl.NumberFormat("en-GB");
const compact = new Intl.NumberFormat("en-GB", {
  notation: "compact",
  maximumFractionDigits: 1,
});
const time = new Intl.DateTimeFormat("en-GB", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  timeZone: "UTC",
});

export function formatNumber(value: number) {
  return numbers.format(value);
}
export function formatCompact(value: number) {
  return compact.format(value);
}
export function formatTime(timestamp: string) {
  return time.format(new Date(timestamp));
}
