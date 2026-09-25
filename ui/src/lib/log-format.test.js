import { describe, expect, test } from "bun:test";
import { computeFormattedMessage, highlightFormatted } from "./log-format";

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
