import { describe, expect, test } from "bun:test";
import { countECSResources, isActiveTask } from "./ecs";

describe("ECS sidebar count helpers", () => {
  test("treats non-stopped tasks as active", () => {
    expect(
      isActiveTask({
        arn: "t1",
        clusterArn: "c1",
        taskDefinitionArn: "d1",
        lastStatus: "RUNNING",
        desiredStatus: "RUNNING",
      }),
    ).toBe(true);
    expect(
      isActiveTask({
        arn: "t2",
        clusterArn: "c1",
        taskDefinitionArn: "d1",
        lastStatus: "PENDING",
        desiredStatus: "RUNNING",
      }),
    ).toBe(true);
    expect(
      isActiveTask({
        arn: "t3",
        clusterArn: "c1",
        taskDefinitionArn: "d1",
        lastStatus: "PROVISIONING",
        desiredStatus: "RUNNING",
      }),
    ).toBe(true);
    expect(
      isActiveTask({
        arn: "t4",
        clusterArn: "c1",
        taskDefinitionArn: "d1",
        lastStatus: "STOPPED",
        desiredStatus: "STOPPED",
      }),
    ).toBe(false);
    expect(
      isActiveTask({
        arn: "t5",
        clusterArn: "c1",
        taskDefinitionArn: "d1",
        lastStatus: " stopped ",
        desiredStatus: "STOPPED",
      }),
    ).toBe(false);
  });

  test("excludes stopped tasks retained after container restarts", () => {
    const base = {
      clusters: [
        {
          name: "default",
          arn: "arn:cluster/default",
          status: "ACTIVE",
          runningTasks: 1,
          pendingTasks: 0,
          activeServices: 1,
        },
      ],
      services: [
        {
          name: "api",
          arn: "arn:service/api",
          clusterArn: "arn:cluster/default",
          taskDefinitionArn: "arn:td/api:1",
          desiredCount: 1,
          runningCount: 1,
          pendingCount: 0,
          status: "ACTIVE",
        },
      ],
      taskDefinitions: [],
    };

    expect(
      countECSResources({
        ...base,
        tasks: [
          {
            arn: "arn:task/running-1",
            clusterArn: "arn:cluster/default",
            taskDefinitionArn: "arn:td/api:1",
            group: "service:api",
            lastStatus: "RUNNING",
            desiredStatus: "RUNNING",
          },
        ],
      }),
    ).toBe(3);

    expect(
      countECSResources({
        ...base,
        tasks: [
          {
            arn: "arn:task/stopped-1",
            clusterArn: "arn:cluster/default",
            taskDefinitionArn: "arn:td/api:1",
            group: "service:api",
            lastStatus: "STOPPED",
            desiredStatus: "STOPPED",
          },
          {
            arn: "arn:task/stopped-2",
            clusterArn: "arn:cluster/default",
            taskDefinitionArn: "arn:td/api:1",
            group: "service:api",
            lastStatus: "STOPPED",
            desiredStatus: "STOPPED",
          },
          {
            arn: "arn:task/stopped-3",
            clusterArn: "arn:cluster/default",
            taskDefinitionArn: "arn:td/api:1",
            group: "service:api",
            lastStatus: "STOPPED",
            desiredStatus: "STOPPED",
          },
          {
            arn: "arn:task/running-4",
            clusterArn: "arn:cluster/default",
            taskDefinitionArn: "arn:td/api:1",
            group: "service:api",
            lastStatus: "RUNNING",
            desiredStatus: "RUNNING",
          },
        ],
      }),
    ).toBe(3);
  });

  test("handles empty or missing ECS overview", () => {
    expect(countECSResources(undefined)).toBe(0);
    expect(countECSResources(null)).toBe(0);
    expect(countECSResources({ clusters: [], services: [], tasks: [], taskDefinitions: [] })).toBe(
      0,
    );
  });
});
