import { describe, expect, test } from "bun:test";
import {
  TabNavigationHistory,
  logStateFromLocation,
  logLocationWithFilters,
} from "./navigation-history";

describe("tab navigation history", () => {
  test("restores the selected ECS row after visiting another section", () => {
    const history = new TabNavigationHistory();
    const selectedService =
      "#ecs?sel=service%3Aarn%3Aaws%3Aecs%3Aeu-west-2%3A000000000000%3Aservice%2Fdemo%2Fworker";

    history.remember(selectedService);
    history.remember("#logs");

    expect(history.destination("ecs")).toBe(selectedService);
  });

  test("restores the selected resource when returning through the sidebar", () => {
    const history = new TabNavigationHistory();

    history.remember("#queues?queue=orders-dead-letter");

    expect(history.destination("queues")).toBe(
      "#queues?queue=orders-dead-letter",
    );
  });

  test("remembers the source of a direct link from a browser old URL", () => {
    const history = new TabNavigationHistory();

    history.remember("#functions?fn=invoice-worker");

    expect(history.destination("functions")).toBe(
      "#functions?fn=invoice-worker",
    );
  });

  test("uses a bare hash until a tab has a remembered location", () => {
    const history = new TabNavigationHistory();

    expect(history.destination("logs")).toBe("#logs");
  });

  test("restores log filters after visiting another section", () => {
    const history = new TabNavigationHistory();
    const filteredLogs = logLocationWithFilters(
      "#logs?groups=%2Faws%2Fecs%2Fworker%2C%2Faws%2Fecs%2Fapi",
      {
        levels: ["ERROR", "WARN"],
        pattern: "request timed out",
        stream: "ecs/worker/abc123",
        order: "asc",
      },
    );

    history.remember(filteredLogs);
    history.remember("#ecs?sel=service%3Aworker");

    const destination = history.destination("logs");
    expect(destination).toBe(
      "#logs?groups=%2Faws%2Fecs%2Fworker%2C%2Faws%2Fecs%2Fapi&level=ERROR%2CWARN&pattern=request+timed+out&stream=ecs%2Fworker%2Fabc123&order=asc",
    );
    expect(logStateFromLocation(destination)).toEqual({
      group: "/aws/ecs/worker,/aws/ecs/api",
      timestamp: "",
      levels: ["ERROR", "WARN"],
      pattern: "request timed out",
      stream: "ecs/worker/abc123",
      order: "asc",
    });
  });
});
