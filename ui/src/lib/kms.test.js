import { describe, expect, test } from "bun:test";
import {
  kmsKeyIsNamed,
  kmsKeyMatches,
  kmsKeyName,
  kmsOperationCount,
  kmsReferenceTarget,
  kmsStateLabel,
  kmsStateTone,
} from "./kms";

const key = {
  id: "1234abcd-12ab-34cd-56ef-1234567890ab",
  arn: "arn:aws:kms:eu-west-2:000000000000:key/1234abcd-12ab-34cd-56ef-1234567890ab",
  description: "Cognito codes",
  aliases: ["alias/cognito"],
  usage: { encrypt: 1, decrypt: 4, dataKeys: 2, reEncrypt: 0, failures: 1 },
};

describe("kms helpers", () => {
  test("names a key by its first alias, else its ID", () => {
    expect(kmsKeyName(key)).toBe("alias/cognito");
    expect(kmsKeyName({ ...key, aliases: [] })).toBe(key.id);
  });

  test("maps key states to labels and tones", () => {
    expect(kmsStateLabel("PendingDeletion")).toBe("Pending deletion");
    expect(kmsStateTone("Enabled")).toBe("green");
    expect(kmsStateTone("Disabled")).toBe("amber");
    expect(kmsStateTone("PendingDeletion")).toBe("red");
  });

  test("counts successful operations only", () => {
    expect(kmsOperationCount(key)).toBe(7);
  });

  test("links each reference to the section parameter that selects it", () => {
    expect(
      kmsReferenceTarget({
        kind: "function",
        name: "email-sender",
        tab: "functions",
        field: "KMSKeyArn",
      }),
    ).toBe("functions?fn=email-sender");
    expect(
      kmsReferenceTarget({
        kind: "secret",
        name: "db/password",
        tab: "secrets",
        field: "KmsKeyId",
      }),
    ).toBe("secrets?name=db%2Fpassword");
    expect(
      kmsReferenceTarget({
        kind: "userpool",
        name: "candidates",
        id: "eu-west-2_abc",
        tab: "cognito",
        field: "KMSKeyID",
      }),
    ).toBe("cognito?pool=eu-west-2_abc");
    expect(
      kmsReferenceTarget({
        kind: "table",
        name: "orders",
        tab: "dynamodb",
        field: "SSESpecification",
      }),
    ).toBe("dynamodb?table=orders");
  });

  test("filters by alias, ID, ARN or description", () => {
    expect(kmsKeyMatches(key, "COGNITO")).toBe(true);
    expect(kmsKeyMatches(key, "1234abcd")).toBe(true);
    expect(kmsKeyMatches(key, "eu-west-2")).toBe(true);
    expect(kmsKeyMatches(key, "payments")).toBe(false);
    expect(kmsKeyMatches(key, "  ")).toBe(true);
  });

  test("recognises a key by ID, ARN, alias or alias ARN", () => {
    expect(kmsKeyIsNamed(key, key.id)).toBe(true);
    expect(kmsKeyIsNamed(key, key.arn)).toBe(true);
    expect(kmsKeyIsNamed(key, "alias/cognito")).toBe(true);
    expect(kmsKeyIsNamed(key, "arn:aws:kms:eu-west-2:000000000000:alias/cognito")).toBe(true);
    expect(kmsKeyIsNamed(key, "alias/other")).toBe(false);
  });
});
