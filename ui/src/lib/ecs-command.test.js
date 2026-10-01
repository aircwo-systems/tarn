import { describe, expect, test } from "bun:test";
import { parseCommandOverride } from "./ecs-command";

describe("ECS command overrides", () => {
  test.each([
    ["", undefined],
    [" \t\n ", undefined],
    ["  node   script.js\t--verbose ", ["node", "script.js", "--verbose"]],
    ["sh -c 'echo hello'", ["sh", "-c", "echo hello"]],
    ['sh -c "echo hello"', ["sh", "-c", "echo hello"]],
    ["node \"\" ''", ["node", "", ""]],
    ['echo pre"quoted suffix"post', ["echo", "prequoted suffixpost"]],
    [String.raw`echo hello\ world`, ["echo", "hello world"]],
    [String.raw`echo 'C:\audit\file'`, ["echo", String.raw`C:\audit\file`]],
    [String.raw`echo "a\"b" "a\\b"`, ["echo", 'a"b', "a\\b"]],
    [String.raw`echo "\q" "\$HOME"`, ["echo", "\\q", "$HOME"]],
    ["echo '$HOME $(touch audit) && *'", ["echo", "$HOME $(touch audit) && *"]],
    ["echo first\\\nsecond", ["echo", "firstsecond"]],
    ['echo "first\\\nsecond"', ["echo", "firstsecond"]],
    ["echo 'first\\\nsecond'", ["echo", "first\\\nsecond"]],
  ])("preserves arguments in %j", (input, expected) => {
    expect(parseCommandOverride(input)).toEqual(expected);
  });

  test.each(["sh -c 'unfinished", 'sh -c "unfinished'])(
    "rejects an unterminated quote in %j",
    (input) => {
      expect(() => parseCommandOverride(input)).toThrow("Unterminated");
    },
  );
  test("rejects a trailing escape instead of silently changing the command", () => {
    expect(() => parseCommandOverride("echo unfinished\\")).toThrow("Incomplete escape");
  });
});
