import { describe, expect, test } from "bun:test";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { functionToHcl } from "./function-hcl";

const summary = {
  name: "audit-function",
  arn: "arn:aws:lambda:us-east-1:111111111111:function:audit-function",
  runtime: "nodejs22.x",
  state: "Active",
  timeoutSec: 3,
  memoryMB: 128,
  codeSize: 1024,
  messagesProcessed: 0,
  version: "$LATEST",
  lastModified: "2026-10-01T00:00:00Z",
  layers: 0,
  tagCount: 0,
};

const tags = {
  "team name": 'The "platform" team',
  "path\\key": "C:\\projects\\lambda",
  "line\nkey": "first\nsecond\r\tthird",
  controls: "\u0000\b\f\u001f",
  "${key}": "${var.literal} %{ if true } $${already} %%{also}",
  unicode: "café 🚀",
};

describe("functionToHcl", () => {
  test("quotes tag keys and escapes string delimiters and controls", () => {
    const hcl = functionToHcl({ ...summary, tags });
    expect(hcl).toContain('"team name" = "The \\"platform\\" team"');
    expect(hcl).toContain('"path\\\\key" = "C:\\\\projects\\\\lambda"');
    expect(hcl).toContain('"line\\nkey" = "first\\nsecond\\r\\tthird"');
    expect(hcl).toContain('"controls" = "\\u0000\\u0008\\u000c\\u001f"');
    expect(hcl).toContain('"unicode" = "café 🚀"');
  });

  test("preserves Terraform template markers as literal text", () => {
    const hcl = functionToHcl({ ...summary, tags });
    expect(hcl).toContain('"$${key}" = "$${var.literal} %%{ if true } $$${already} %%%{also}"');
  });

  test("escapes metadata strings as well as tags", () => {
    const hcl = functionToHcl({ ...summary, version: 'v"1\\${literal}\n' });
    expect(hcl).toMatch(/version\s*= "v\\"1\\\\\$\$\{literal\}\\n"/);
  });

  const terraform = Bun.which("terraform");
  test.skipIf(!terraform)("Terraform parses the export and round-trips every tag", () => {
    const hcl = functionToHcl({ ...summary, tags });
    const formatted = spawnSync(terraform, ["fmt", "-no-color", "-"], {
      input: hcl,
      encoding: "utf8",
    });
    expect(formatted.stderr).toBe("");
    expect(formatted.status).toBe(0);

    const directory = mkdtempSync(join(tmpdir(), "tarn-hcl-test-"));
    try {
      const tagExpression = hcl.match(/tags = (\{[\s\S]*\})\n\}/)?.[1];
      expect(tagExpression).toBeDefined();
      // Console evaluates the exported map without an AWS provider or state.
      const singleLineMap = `{${tagExpression.split("\n").slice(1, -1).join(",")}}`;
      const evaluated = spawnSync(terraform, ["console", "-no-color"], {
        cwd: directory,
        input: `jsonencode(${singleLineMap})\n`,
        encoding: "utf8",
      });
      expect(evaluated.stderr).toBe("");
      expect(evaluated.status).toBe(0);
      expect(JSON.parse(JSON.parse(evaluated.stdout.trim()))).toEqual(tags);
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });
});
