import { getContext, setContext } from "svelte";
import { activity, workers } from "./fixtures";
import type { Activity, Worker } from "./models";

const demoContext = Symbol("dashboard-demo");

/** Create once per layout instance. Never put user state in a server module singleton. */
export function createDemo() {
  const data = $state({
    workers: structuredClone([...workers]),
    activity: structuredClone([...activity]),
  });

  function changeWorker(worker: Worker) {
    const current = data.workers.find((item) => item.id === worker.id);
    if (!current) return;
    const start = current.status === "paused" || current.status === "error";
    current.status = start ? "running" : "paused";
    current.currentTask = start ? "Processing queued work" : null;
    const event: Activity = {
      id: crypto.randomUUID(),
      timestamp: new Date().toISOString(),
      level: start ? "success" : "warning",
      resource: current.name,
      message: start
        ? "Worker resumed in the demo session."
        : "Worker paused in the demo session.",
    };
    data.activity.unshift(event);
  }

  function reset() {
    data.workers = structuredClone([...workers]);
    data.activity = structuredClone([...activity]);
  }

  return setContext(demoContext, { data, changeWorker, reset });
}

export function getDemo() {
  return getContext<ReturnType<typeof createDemo>>(demoContext);
}
