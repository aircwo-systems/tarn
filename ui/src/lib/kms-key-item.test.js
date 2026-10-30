import { describe, expect, test } from "bun:test";
import { kmsKeyItem } from "./utils";

describe("kmsKeyItem", () => {
  test("marks a recorded key as not applied locally", () => {
    expect(kmsKeyItem("alias/app", "aws/secretsmanager")).toEqual({
      label: "KMS key",
      value: "alias/app · recorded only",
      mono: true,
    });
  });

  test("falls back to the AWS managed key, dimmed", () => {
    expect(kmsKeyItem(undefined, "aws/lambda")).toEqual({
      label: "KMS key",
      value: "aws/lambda (AWS managed)",
      mono: true,
      dim: true,
    });
  });
});
