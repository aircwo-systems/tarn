import { describe, expect, test } from "bun:test";
import {
  escapeHTML,
  highlightJSON,
  highlightSearchText,
  renderTokensWithHighlight,
  formatJSONForViewer,
  splitFilterTerms,
  formatFilterTerm,
} from "./json-format";
import { highlightFormatted } from "./log-format";

describe("escapeHTML", () => {
  test("escapes &, <, >", () => {
    expect(escapeHTML("<foo & bar>")).toBe("&lt;foo &amp; bar&gt;");
  });
});

describe("highlightSearchText", () => {
  test("returns escaped text when pattern is missing or whitespace", () => {
    expect(highlightSearchText("hello <world>", "")).toBe("hello &lt;world&gt;");
    expect(highlightSearchText("hello <world>", "   ")).toBe("hello &lt;world&gt;");
  });

  test("highlights case-insensitive matches", () => {
    const res = highlightSearchText("Error in processing: connection refused", "error");
    expect(res).toBe(
      '<mark class="log-search-match">Error</mark> in processing: connection refused',
    );
  });

  test("highlights multiple occurrences", () => {
    const res = highlightSearchText("foo bar foo baz", "foo");
    expect(res).toBe(
      '<mark class="log-search-match">foo</mark> bar <mark class="log-search-match">foo</mark> baz',
    );
  });

  test("escapes HTML within matches and surrounding text", () => {
    const res = highlightSearchText("<code>Hello</code>", "Hello");
    expect(res).toBe(
      '&lt;code&gt;<mark class="log-search-match">Hello</mark>&lt;/code&gt;',
    );
  });
});

describe("highlightJSON", () => {
  const sampleJSON = JSON.stringify(
    {
      status: "error",
      code: 500,
      active: true,
      data: null,
      message: "Database connection failed",
    },
    null,
    2,
  );

  test("produces syntax-highlighted HTML without search pattern", () => {
    const html = highlightJSON(sampleJSON);
    expect(html).toContain('color:var(--lh-key)">"status"</span>');
    expect(html).toContain('color:var(--lh-str-json)">"error"</span>');
    expect(html).toContain('color:var(--lh-num)">500</span>');
    expect(html).toContain('color:var(--lh-bool)">true</span>');
    expect(html).toContain('color:var(--lh-null);font-style:italic">null</span>');
    expect(html).not.toContain("log-search-match");
  });

  test("highlights match inside JSON key", () => {
    const html = highlightJSON(sampleJSON, "status");
    expect(html).toContain(
      '<span style="color:var(--lh-key)">"<mark class="log-search-match">status</mark>"</span>',
    );
  });

  test("highlights substring match inside JSON key", () => {
    const html = highlightJSON(sampleJSON, "stat");
    expect(html).toContain(
      '<span style="color:var(--lh-key)">"<mark class="log-search-match">stat</mark>us"</span>',
    );
  });

  test("highlights match inside JSON string value", () => {
    const html = highlightJSON(sampleJSON, "connection");
    expect(html).toContain(
      '<span style="color:var(--lh-str-json)">"Database <mark class="log-search-match">connection</mark> failed"</span>',
    );
  });

  test("highlights match inside number value", () => {
    const html = highlightJSON(sampleJSON, "500");
    expect(html).toContain(
      '<span style="color:var(--lh-num)"><mark class="log-search-match">500</mark></span>',
    );
  });

  test("highlights match inside boolean value", () => {
    const html = highlightJSON(sampleJSON, "true");
    expect(html).toContain(
      '<span style="color:var(--lh-bool)"><mark class="log-search-match">true</mark></span>',
    );
  });

  test("highlights match inside null value", () => {
    const html = highlightJSON(sampleJSON, "null");
    expect(html).toContain(
      '<span style="color:var(--lh-null);font-style:italic"><mark class="log-search-match">null</mark></span>',
    );
  });

  test("highlights match case-insensitively", () => {
    const html = highlightJSON(sampleJSON, "DATABASE");
    expect(html).toContain(
      '<span style="color:var(--lh-str-json)">"<mark class="log-search-match">Database</mark> connection failed"</span>',
    );
  });

  test("highlights match spanning quotes and key syntax", () => {
    const html = highlightJSON(sampleJSON, '"status"');
    expect(html).toContain(
      '<span style="color:var(--lh-key)"><mark class="log-search-match">"status"</mark></span>',
    );
  });

  test("highlights match across key, colon, and value", () => {
    const html = highlightJSON(sampleJSON, '"status": "error"');
    expect(html).toContain(
      '<span style="color:var(--lh-key)"><mark class="log-search-match">"status"</mark></span>' +
        '<span style="color:var(--lh-punct)"><mark class="log-search-match">: </mark></span>' +
        '<span style="color:var(--lh-str-json)"><mark class="log-search-match">"error"</mark></span>',
    );
  });

  test("handles arrays and standalone values in JSON", () => {
    const arrayJSON = JSON.stringify({ tags: ["prod", "us-east-1"] }, null, 2);
    const html = highlightJSON(arrayJSON, "prod");
    expect(html).toContain(
      '<span style="color:var(--lh-str-json)">"<mark class="log-search-match">prod</mark>"</span>',
    );
  });
});

describe("formatJSONForViewer", () => {
  test("passes search pattern through to highlightJSON", () => {
    const result = formatJSONForViewer('{"msg":"alert"}', "alert");
    expect(result).not.toBeNull();
    expect(result?.formattedHtml).toContain(
      '<mark class="log-search-match">alert</mark>',
    );
  });
});

describe("highlightFormatted", () => {
  test("highlights JSON formatted strings with search pattern", () => {
    const jsonStr = '{\n  "error": "not found"\n}';
    const html = highlightFormatted(jsonStr, "found");
    expect(html).toContain(
      '<mark class="log-search-match">found</mark>',
    );
  });

  test("highlights Java toString formatted strings with search pattern", () => {
    const javaStr = 'OrderDto(\n  orderId: 12345,\n  status: COMPLETED\n)';
    const html = highlightFormatted(javaStr, "orderId");
    expect(html).toContain(
      '<mark class="log-search-match">orderId</mark>',
    );
  });
});

  test("highlightSearchText matches across flexible whitespace in JSON", () => {
    const raw = `{"level":"warn","message":"test"}`;
    const result = highlightSearchText(raw, `"level": "warn"`);
    expect(result).toContain(`<mark class="log-search-match">"level":"warn"</mark>`);
  });

  test("highlightJSON matches unspaced pattern in spaced JSON", () => {
    const json = JSON.stringify({ level: "warn" }, null, 2);
    const result = highlightJSON(json, `"level":"warn"`);
    expect(result).toContain(`<mark class="log-search-match">"level"</mark>`);
    expect(result).toContain(`<mark class="log-search-match">: </mark>`);
    expect(result).toContain(`<mark class="log-search-match">"warn"</mark>`);
  });


  test("splitFilterTerms splits whitespace, quotes, and key-values", () => {
    const terms = splitFilterTerms('orders-api "Internal Server Error" "level": "warn" status: 500');
    expect(terms).toEqual([
      "orders-api",
      `"Internal Server Error"`,
      `"level": "warn"`,
      "status: 500"
    ]);
  });

  test("formatFilterTerm preserves quoted/key-value and quotes multi-word phrases", () => {
    expect(formatFilterTerm("warn")).toBe("warn");
    expect(formatFilterTerm(`"level": "warn"`)).toBe(`"level": "warn"`);
    expect(formatFilterTerm("status: 500")).toBe("status: 500");
    expect(formatFilterTerm("Internal Server Error")).toBe(`"Internal Server Error"`);
    expect(formatFilterTerm(`"already quoted"`)).toBe(`"already quoted"`);
  });

  test("highlightSearchText highlights multiple terms", () => {
    const raw = `[orders-api] 2026-09-19 ERROR: database connection timed out`;
    const result = highlightSearchText(raw, `orders-api ERROR`);
    expect(result).toContain(`<mark class="log-search-match">orders-api</mark>`);
    expect(result).toContain(`<mark class="log-search-match">ERROR</mark>`);
  });
