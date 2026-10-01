import { mount, tick, unmount } from "svelte";
import EcsRunTaskDialog from "$lib/components/ecs/ecs-run-task-dialog.svelte";

// Run in a dashboard dev-server tab:
// await (await import('/src/testing/ecs-task-dialog.browser.js')).checkEcsTaskDialog()
const clusterArn = "arn:aws:ecs:us-east-1:111111111111:cluster/audit-cluster";
const definitionArn = (revision) =>
  `arn:aws:ecs:us-east-1:111111111111:task-definition/audit-task:${revision}`;
const detail = (revision) => ({
  taskDefinitionArn: definitionArn(revision),
  family: "audit-task",
  revision,
  status: "ACTIVE",
  containers: [
    { name: `revision-${revision}-container`, image: "audit.invalid/image", essential: true },
  ],
});

async function withDialog(check) {
  const host = document.createElement("div");
  document.body.append(host);
  const originalFetch = window.fetch;
  const requests = [];
  const launches = [];
  let holdRequests = false;
  let failRequests = false;
  const pending = [];
  window.fetch = async (input, init) => {
    const path = new URL(String(input), location.origin).pathname;
    if (path.includes("/_tarn/admin/ecs/task-definitions/")) {
      const reference = decodeURIComponent(path.split("/").at(-1));
      requests.push(reference);
      const revision = Number(reference.split(":").at(-1)) || 2;
      const respond = () =>
        failRequests
          ? Response.json({ message: "synthetic detail failure" }, { status: 503 })
          : Response.json(detail(revision));
      if (holdRequests)
        return new Promise((resolve) =>
          pending.push({ reference, resolve: () => resolve(respond()) }),
        );
      return respond();
    }
    if (path.endsWith("/ecs/run-task")) {
      launches.push(JSON.parse(init.body));
      return Response.json({ tasks: [], failures: [{ reason: "Synthetic launch intercepted" }] });
    }
    return originalFetch(input, init);
  };

  const component = mount(EcsRunTaskDialog, {
    target: host,
    props: {
      clusters: [
        {
          name: "audit-cluster",
          arn: clusterArn,
          status: "ACTIVE",
          runningTasks: 0,
          pendingTasks: 0,
          activeServices: 0,
        },
      ],
      taskDefinitions: [1, 2].map((revision) => ({
        ...detail(revision),
        arn: definitionArn(revision),
        name: "audit-task",
      })),
      initialTaskDefinitionArn: definitionArn(1),
      onclose: () => {},
      onlaunched: () => {},
    },
  });
  const field = (label) =>
    [...host.querySelectorAll("label")]
      .find((el) => el.textContent.trim().startsWith(label))
      ?.querySelector("input, select, textarea");
  const runButton = () =>
    [...host.querySelectorAll("button")].find((el) => el.textContent.trim() === "Run task");
  const waitFor = async (predicate, message) => {
    const deadline = Date.now() + 3000;
    while (!predicate()) {
      if (Date.now() > deadline) throw new Error(message);
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
  };
  const setField = async (label, value) => {
    const element = field(label);
    element.value = value;
    element.dispatchEvent(
      new Event(element.tagName === "SELECT" ? "change" : "input", { bubbles: true }),
    );
    await tick();
  };
  try {
    await waitFor(
      () => field("Container for overrides") && !field("Container for overrides").disabled,
      "Initial containers did not load",
    );
    return await check({
      host,
      field,
      runButton,
      waitFor,
      setField,
      requests,
      launches,
      pending,
      hold: (value) => {
        holdRequests = value;
      },
      fail: (value) => {
        failRequests = value;
      },
    });
  } finally {
    await unmount(component);
    for (const request of pending) request.resolve();
    window.fetch = originalFetch;
    host.remove();
  }
}

export async function checkEcsRevision() {
  return withDialog(
    async ({
      host,
      field,
      runButton,
      waitFor,
      setField,
      requests,
      launches,
      pending,
      hold,
      fail,
    }) => {
      if (
        requests[0] !== "audit-task:1" ||
        field("Container for overrides").value !== "revision-1-container"
      ) {
        throw new Error("Revision 1 loaded the latest revision's containers");
      }
      await setField("Environment overrides", "AUDIT=revision-test");
      hold(true);
      await setField("Task definition", definitionArn(2));
      await waitFor(() => pending.length === 1, "Revision 2 request was not deferred");
      if (!runButton().disabled || field("Container for overrides").value)
        throw new Error("Stale containers remained launchable during a revision change");
      await setField("Task definition", definitionArn(1));
      await waitFor(() => pending.length === 2, "Revision 1 reload was not deferred");
      pending[1].resolve();
      await waitFor(
        () => !field("Container for overrides").disabled,
        "Current revision did not finish loading",
      );
      pending[0].resolve();
      await tick();
      await new Promise((resolve) => setTimeout(resolve, 20));
      if (field("Container for overrides").value !== "revision-1-container")
        throw new Error("A late revision 2 response overwrote revision 1");
      runButton().click();
      await waitFor(() => launches.length === 1, "Override launch was not sent");
      if (
        launches[0].taskDefinition !== definitionArn(1) ||
        launches[0].overrides.containerOverrides[0].name !== "revision-1-container"
      )
        throw new Error("Launch combined different task-definition revisions");
      await waitFor(() => runButton(), "Launch did not settle");
      hold(false);
      fail(true);
      await setField("Task definition", definitionArn(2));
      await waitFor(
        () => host.textContent.includes("synthetic detail failure"),
        "Detail errors were not visible",
      );
      if (!runButton().disabled || field("Container for overrides").value)
        throw new Error("Failed detail load still allowed stale overrides");
      await setField("Environment overrides", "");
      if (runButton().disabled)
        throw new Error("A task without overrides was blocked by a detail error");
      return {
        passed: [
          "exact revision fetched",
          "overrides wait for containers",
          "late responses ignored",
          "matching container sent",
          "detail errors block only overrides",
        ],
      };
    },
  );
}

export async function checkEcsCommand() {
  return withDialog(async ({ host, runButton, waitFor, setField, launches }) => {
    await setField("Command override", "sh -c 'echo hello'");
    runButton().click();
    await waitFor(() => launches.length === 1, "Command launch was not sent");
    const command = launches[0].overrides.containerOverrides[0].command;
    if (JSON.stringify(command) !== JSON.stringify(["sh", "-c", "echo hello"]))
      throw new Error(`Quoted command was split incorrectly: ${JSON.stringify(command)}`);
    await waitFor(() => runButton(), "Command launch did not settle");
    await setField("Command override", "sh -c 'unterminated");
    runButton().click();
    await waitFor(
      () => host.textContent.includes("Unterminated"),
      "Unterminated quote error was not shown",
    );
    if (launches.length !== 1) throw new Error("Invalid quoted command was sent to the backend");
    return { passed: ["quoted argument sent intact", "invalid quotes fail before launch"] };
  });
}

export async function checkEcsTaskDialog() {
  return { revision: await checkEcsRevision(), command: await checkEcsCommand() };
}
