import { describe, expect, test } from "bun:test";
import { computeFormattedMessage, formatDatadogLog, highlightFormatted, shortTraceId } from "./log-format";

describe("Invoke Error logs", () => {
  const error = {
    errorType: "FullBatchFailureError",
    errorMessage: "All records failed processing. See individual errors below.",
    recordErrors: [
      {
        errorType: "Error",
        errorMessage: "CRS case creation unsuccessful for caseRefId: 30420002",
        stack: [
          "Error: CRS case creation unsuccessful",
          "    at postCrsCases (/var/task/index.js:18:216074)",
        ],
      },
    ],
    stack: ["FullBatchFailureError: All records failed processing."],
  };

  test("formats the JSON payload while retaining the log prefix", () => {
    const formatted = computeFormattedMessage(`Invoke Error \t${JSON.stringify(error)}`);
    expect(formatted).toBe(`Invoke Error\n${JSON.stringify(error, null, 2)}`);
  });

  test("highlights the JSON payload and search matches", () => {
    const formatted = `Invoke Error\n${JSON.stringify(error, null, 2)}`;
    const html = highlightFormatted(formatted, "FullBatchFailureError");
    expect(html).toContain('color:var(--lh-key)">"errorType"</span>');
    expect(html).toContain('<mark class="log-search-match">FullBatchFailureError</mark>');
  });

  test("leaves an invalid payload unformatted", () => {
    expect(computeFormattedMessage("Invoke Error \t{broken JSON}")).toBeNull();
  });
});

describe("JSON embedded in log messages", () => {
  test("formats a request payload inside the message field", () => {
    const payload = {
      caseRefId: 30420002,
      applicantId: "HCE30420002",
      title: "",
      postcode: "NE15 8NY",
      accept: true,
    };
    const prefix = "[Axios][Request] POST http://host.docker.internal:8303/cases";
    const raw = JSON.stringify({ level: "info", message: `${prefix} ${JSON.stringify(payload)}` });

    expect(computeFormattedMessage(raw)).toBe(
      JSON.stringify({ level: "info", message: { text: prefix, payload } }, null, 2),
    );
  });

  test("keeps ordinary text and malformed trailing JSON as strings", () => {
    const ordinary = { level: "info", message: "Request completed {success}" };
    expect(computeFormattedMessage(JSON.stringify(ordinary))).toBe(
      JSON.stringify(ordinary, null, 2),
    );
  });
});

describe("Datadog logs", () => {
  const record = (fields) =>
    JSON.stringify({
      dd: { env: "nhs-jobs-local", service: "employer-frontend", span_id: "6116946858867865602", trace_id: "6ac5554a000000004d7759caae6feb73" },
      http: {},
      level: "info",
      timestamp: "2026-10-06T20:08:42.534Z",
      service: "employer-frontend",
      hostname: "2d1b49c5e757",
      ddsource: "docker",
      ddtags: "tarn.account_id:123123123123",
      ...fields,
    });

  test("splits request lines into request, status and duration", () => {
    expect(formatDatadogLog(record({ message: "GET /employer/home 200 108ms" }))).toEqual({
      message: "GET /employer/home",
      extras: "",
      service: "employer-frontend",
      traceId: "6ac5554a000000004d7759caae6feb73",
      status: 200,
      durationMs: 108,
    });
  });

  test("keeps other messages whole and reads status from http fields", () => {
    expect(formatDatadogLog(record({ message: "Redis session storage in use" }))).toEqual({
      message: "Redis session storage in use",
      extras: "",
      service: "employer-frontend",
      traceId: "6ac5554a000000004d7759caae6feb73",
    });
    expect(formatDatadogLog(record({ message: "upstream failed", http: { status_code: 502 } }))?.status).toBe(502);
  });

  test("keeps application fields as compact key=value pairs", () => {
    expect(
      formatDatadogLog(record({ message: "request failed", userId: 42, http: { status_code: 500 }, note: "two words" })),
    ).toMatchObject({ message: "request failed", extras: 'http={"status_code":500} userId=42 note="two words"', status: 500 });
  });

  test("recognises records that carry only dotted trace IDs", () => {
    expect(formatDatadogLog(JSON.stringify({ message: "ok", "dd.trace_id": "1", ddsource: "nodejs" }))?.message).toBe("ok");
  });

  test("leaves other JSON and plain text alone", () => {
    expect(formatDatadogLog(JSON.stringify({ level: "info", message: "plain winston" }))).toBeNull();
    expect(formatDatadogLog("App running on port 80")).toBeNull();
    expect(formatDatadogLog(record({}))).toBeNull();
  });
});

describe("shortTraceId", () => {
  test("shows the low 64 bits as eight hex digits for decimal and 128-bit IDs", () => {
    expect(shortTraceId("6ac5554a000000004d7759caae6feb73")).toBe("ae6feb73");
    expect(shortTraceId("5582934475893058419")).toBe(BigInt("5582934475893058419").toString(16).slice(-8));
    expect(shortTraceId("0")).toBe("");
    expect(shortTraceId("not-an-id")).toBe("");
  });
});
