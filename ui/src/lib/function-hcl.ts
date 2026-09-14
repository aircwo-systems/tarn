import type { FunctionSummary } from "$lib/types";
import { formatBytes, formatDate } from "$lib/utils";

/** Render a function summary as an `aws_lambda_function` Terraform block. */
export function functionToHcl(f: FunctionSummary): string {
  const COL = 13;
  const s = (key: string, val: string) => `  ${key.padEnd(COL)} = ${val}`;
  const q = (v: string) => `"${v}"`;

  const lines: string[] = [];
  lines.push(`resource "aws_lambda_function" ${q(f.name)} {`);
  lines.push(s("function_name", q(f.name)));
  lines.push(s("runtime", q(f.runtime)));
  lines.push(s("memory_size", String(f.memoryMB)));
  lines.push(s("timeout", String(f.timeoutSec)));
  lines.push(``);
  lines.push(`  # deployment`);
  lines.push(s("code_size", q(formatBytes(f.codeSize))));
  if (f.layers > 0) lines.push(s("layers", String(f.layers)));
  lines.push(s("version", q(f.version)));
  lines.push(``);
  lines.push(`  # metadata`);
  lines.push(s("state", q(f.state)));
  lines.push(s("last_modified", q(formatDate(f.lastModified))));
  lines.push(s("arn", q(f.arn)));
  if (f.tags && Object.keys(f.tags).length > 0) {
    lines.push(``);
    lines.push(`  tags = {`);
    for (const [k, v] of Object.entries(f.tags)) lines.push(`    ${k} = ${q(v)}`);
    lines.push(`  }`);
  }
  lines.push(`}`);
  return lines.join("\n");
}
