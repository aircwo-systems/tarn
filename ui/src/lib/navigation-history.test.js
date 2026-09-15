import { describe, expect, test } from "bun:test";
import { TabNavigationHistory } from "./navigation-history";

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
});
