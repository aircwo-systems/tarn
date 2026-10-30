import { expect, test } from "bun:test";
import { looksLikeAWSCredentialBlock, parseAWSCredentialPaste } from "./aws-credential-paste";

const expected = {
  kind: "temporary",
  accessKeyId: "ASIAEXAMPLE1234567890",
  secretAccessKey: "example/secret+key=",
  sessionToken: "example/session+token==",
};

test.each([
  'export AWS_ACCESS_KEY_ID="ASIAEXAMPLE1234567890"\nexport AWS_SECRET_ACCESS_KEY="example/secret+key="\nexport AWS_SESSION_TOKEN="example/session+token=="',
  "$Env:AWS_ACCESS_KEY_ID='ASIAEXAMPLE1234567890'\r\n$Env:AWS_SECRET_ACCESS_KEY='example/secret+key='\r\n$Env:AWS_SESSION_TOKEN='example/session+token=='",
  "set AWS_ACCESS_KEY_ID=ASIAEXAMPLE1234567890\nset AWS_SECRET_ACCESS_KEY=example/secret+key=\nset AWS_SESSION_TOKEN=example/session+token==",
  "[company-dev]\naws_access_key_id=ASIAEXAMPLE1234567890\naws_secret_access_key=example/secret+key=\naws_session_token=example/session+token==",
  '{"Version":1,"AccessKeyId":"ASIAEXAMPLE1234567890","SecretAccessKey":"example/secret+key=","SessionToken":"example/session+token=="}',
  "export AWS_ACCESS_KEY_ID='ASIAEXAMPLE1234567890';\nexport AWS_SECRET_ACCESS_KEY='example/secret+key=';\nexport AWS_SESSION_TOKEN='example/session+token==';",
])("parses copied AWS credentials as data: %#", (text) => {
  expect(looksLikeAWSCredentialBlock(text)).toBe(true);
  expect(parseAWSCredentialPaste(text)).toEqual(expected);
});

test("a key pair without a session token selects long-lived credentials", () => {
  expect(
    parseAWSCredentialPaste(
      "AWS_ACCESS_KEY_ID=AKIAEXAMPLE1234567890\nAWS_SECRET_ACCESS_KEY=secret",
    ),
  ).toEqual({
    kind: "long-lived",
    accessKeyId: "AKIAEXAMPLE1234567890",
    secretAccessKey: "secret",
  });
});

test.each([
  "export AWS_ACCESS_KEY_ID=only-one-key",
  "AWS_ACCESS_KEY_ID=one\nAWS_ACCESS_KEY_ID=two\nAWS_SECRET_ACCESS_KEY=secret",
  'AWS_ACCESS_KEY_ID="incomplete\nAWS_SECRET_ACCESS_KEY=secret',
  "AWS_ACCESS_KEY_ID=$(touch /tmp/should-not-run)\nAWS_SECRET_ACCESS_KEY=secret",
  "AWS_ACCESS_KEY_ID=one\nAWS_SECRET_ACCESS_KEY=secret\nrm -rf anything",
])("rejects incomplete, duplicate or command-bearing blocks: %#", (text) => {
  expect(() => parseAWSCredentialPaste(text)).toThrow();
});

test("paste errors never include credential values", () => {
  try {
    parseAWSCredentialPaste("AWS_ACCESS_KEY_ID=private-access-key");
  } catch (error) {
    expect(error.message).not.toContain("private-access-key");
  }
});
