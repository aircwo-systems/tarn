<script lang="ts">
  import {
    ArrowsClockwiseIcon,
    PlusIcon,
    PlayIcon,
    TrashIcon,
    BridgeIcon,
  } from "phosphor-svelte";
  import {
    Table,
    TableHeader,
    TableBody,
    TableHead,
    TableCell,
    TableRow,
  } from "$lib/components/ui/table";
  import LedDot from "$lib/components/common/led-dot.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import EmptyState from "$lib/components/common/empty-state.svelte";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import SectionHeader from "./section-header.svelte";
  import {
    putEventBridgeRule,
    enableEventBridgeRule,
    disableEventBridgeRule,
    deleteEventBridgeRule,
    putEventBridgeTargets,
    removeEventBridgeTargets,
    fireEventBridgeRule,
  } from "$lib/api";
  import { getDashboard, refresh } from "$lib/state.svelte";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const functions = $derived(dashboard.data?.functions ?? []);
  const rules = $derived(dashboard.data?.eventBridgeRules ?? []);

  let selectedRuleName = $state("");
  let editScheduleExpression = $state("");
  let editDescription = $state("");
  let editEnabled = $state(true);
  let syncedRuleName = $state("");

  let createName = $state("");
  let createScheduleExpression = $state("rate(1 minute)");
  let createDescription = $state("");
  let createEnabled = $state(true);

  let targetIdDraft = $state("");
  let targetFunctionDraft = $state("");
  let targetInputDraft = $state("");
  let targetInputPathDraft = $state("");
  let selectedTargetIDs = $state<Set<string>>(new Set());

  let creating = $state(false);
  let saving = $state(false);
  let deleting = $state(false);
  let addingTarget = $state(false);
  let removingTargets = $state(false);
  let firingRuleName = $state("");
  let togglingRuleName = $state("");

  let feedback = $state<{ tone: "ok" | "error"; text: string } | null>(null);

  const selectedRule = $derived(
    rules.find((rule) => rule.name === selectedRuleName) ?? null,
  );
  const selectedTargetCount = $derived(selectedTargetIDs.size);

  $effect(() => {
    if (!selectedRuleName && rules.length > 0) {
      selectedRuleName = rules[0].name;
      return;
    }
    if (
      selectedRuleName &&
      !rules.some((rule) => rule.name === selectedRuleName)
    ) {
      selectedRuleName = rules[0]?.name ?? "";
    }
  });

  $effect(() => {
    if (!selectedRule) {
      syncedRuleName = "";
      editScheduleExpression = "";
      editDescription = "";
      editEnabled = true;
      selectedTargetIDs = new Set();
      return;
    }
    if (syncedRuleName !== selectedRule.name) {
      syncedRuleName = selectedRule.name;
      editScheduleExpression = selectedRule.scheduleExpression;
      editDescription = selectedRule.description ?? "";
      editEnabled = selectedRule.state !== "DISABLED";
      selectedTargetIDs = new Set();
      targetFunctionDraft = functions[0]?.name ?? "";
      targetIdDraft = selectedRule.targets?.length
        ? `t${selectedRule.targets.length + 1}`
        : "t1";
    }
  });

  function clearFeedback() {
    feedback = null;
  }

  function showError(err: unknown) {
    feedback = {
      tone: "error",
      text: err instanceof Error ? err.message : "Request failed",
    };
  }

  function showSuccess(text: string) {
    feedback = { tone: "ok", text };
  }

  async function handleCreateRule() {
    const name = createName.trim();
    const scheduleExpression = createScheduleExpression.trim();
    if (!name || !scheduleExpression) {
      feedback = {
        tone: "error",
        text: "Rule name and schedule expression are required.",
      };
      return;
    }

    creating = true;
    clearFeedback();
    try {
      await putEventBridgeRule({
        name,
        scheduleExpression,
        description: createDescription.trim(),
        state: createEnabled ? "ENABLED" : "DISABLED",
      });
      await refresh();
      selectedRuleName = name;
      createName = "";
      createDescription = "";
      createScheduleExpression = "rate(1 minute)";
      createEnabled = true;
      showSuccess(`Created rule ${name}.`);
    } catch (err) {
      showError(err);
    } finally {
      creating = false;
    }
  }

  async function handleSaveRule() {
    if (!selectedRule) return;
    const scheduleExpression = editScheduleExpression.trim();
    if (!scheduleExpression) {
      feedback = { tone: "error", text: "Schedule expression is required." };
      return;
    }

    saving = true;
    clearFeedback();
    try {
      await putEventBridgeRule({
        name: selectedRule.name,
        scheduleExpression,
        description: editDescription.trim(),
        state: editEnabled ? "ENABLED" : "DISABLED",
      });
      await refresh();
      showSuccess(`Updated rule ${selectedRule.name}.`);
    } catch (err) {
      showError(err);
    } finally {
      saving = false;
    }
  }

  async function handleDeleteRule() {
    if (!selectedRule) return;
    if (!window.confirm(`Delete EventBridge rule "${selectedRule.name}"?`))
      return;

    deleting = true;
    clearFeedback();
    try {
      await deleteEventBridgeRule(selectedRule.name);
      await refresh();
      showSuccess(`Deleted rule ${selectedRule.name}.`);
    } catch (err) {
      showError(err);
    } finally {
      deleting = false;
    }
  }

  async function handleToggleRule(ruleName: string, enable: boolean) {
    togglingRuleName = ruleName;
    clearFeedback();
    try {
      if (enable) {
        await enableEventBridgeRule(ruleName);
      } else {
        await disableEventBridgeRule(ruleName);
      }
      await refresh();
      showSuccess(`${enable ? "Enabled" : "Disabled"} rule ${ruleName}.`);
    } catch (err) {
      showError(err);
    } finally {
      togglingRuleName = "";
    }
  }

  async function handleFireRule(ruleName: string) {
    firingRuleName = ruleName;
    clearFeedback();
    try {
      const result = await fireEventBridgeRule(ruleName);
      await refresh();
      showSuccess(
        `Fired ${ruleName}: ${result.successful}/${result.targets} targets succeeded.`,
      );
    } catch (err) {
      showError(err);
    } finally {
      firingRuleName = "";
    }
  }

  async function handleAddTarget() {
    if (!selectedRule) return;

    const id = targetIdDraft.trim();
    const functionName = targetFunctionDraft.trim();
    if (!id || !functionName) {
      feedback = {
        tone: "error",
        text: "Target ID and function are required.",
      };
      return;
    }

    const fn = functions.find((candidate) => candidate.name === functionName);
    const input = targetInputDraft.trim();
    const inputPath = targetInputPathDraft.trim();

    addingTarget = true;
    clearFeedback();
    try {
      const result = await putEventBridgeTargets(selectedRule.name, [
        {
          id,
          arn: fn?.arn ?? functionName,
          input: input || undefined,
          inputPath: inputPath || undefined,
        },
      ]);
      if (result.failedEntryCount > 0) {
        throw new Error(
          result.failedEntries[0]?.errorMessage ?? "Failed to add target",
        );
      }
      await refresh();
      targetIdDraft = `t${(selectedRule.targets?.length ?? 0) + 2}`;
      targetInputDraft = "";
      targetInputPathDraft = "";
      showSuccess(`Added target ${id} to ${selectedRule.name}.`);
    } catch (err) {
      showError(err);
    } finally {
      addingTarget = false;
    }
  }

  async function handleRemoveSelectedTargets() {
    if (!selectedRule || selectedTargetIDs.size === 0) return;

    removingTargets = true;
    clearFeedback();
    try {
      const ids = [...selectedTargetIDs];
      const result = await removeEventBridgeTargets(selectedRule.name, ids);
      if (result.failedEntryCount > 0) {
        throw new Error(
          result.failedEntries[0]?.errorMessage ?? "Failed to remove targets",
        );
      }
      await refresh();
      selectedTargetIDs = new Set();
      showSuccess(`Removed ${ids.length} target(s) from ${selectedRule.name}.`);
    } catch (err) {
      showError(err);
    } finally {
      removingTargets = false;
    }
  }

  function toggleTargetSelection(id: string, checked: boolean) {
    const next = new Set(selectedTargetIDs);
    if (checked) {
      next.add(id);
    } else {
      next.delete(id);
    }
    selectedTargetIDs = next;
  }

  function stateColor(state: string): "green" | "red" | "gray" {
    if (state === "ENABLED") return "green";
    if (state === "DISABLED") return "red";
    return "gray";
  }

  function formatTime(value?: string): string {
    if (!value) return "—";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString();
  }

  function lambdaNameFromTargetArn(arn: string): string {
    const marker = ":function:";
    const idx = arn.indexOf(marker);
    if (idx < 0) return arn;
    return arn.slice(idx + marker.length).split(":")[0] || arn;
  }
</script>

<div class="space-y-4">
  <section>
    <SectionHeader
      title="EventBridge rules"
      description={`${rules.length} rule${rules.length === 1 ? "" : "s"} · default bus`}
      icon={BridgeIcon}
      {sidebarCollapsed}
      {onToggleSidebar}
    />

    <div class="mt-3 grid gap-2 md:grid-cols-[12rem_minmax(0,1fr)_12rem_auto]">
      <input
        type="text"
        bind:value={createName}
        placeholder="Rule name"
        class="h-8 rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
      />
      <input
        type="text"
        bind:value={createScheduleExpression}
        placeholder="rate(1 minute) or cron(...)"
        class="h-8 rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
      />
      <input
        type="text"
        bind:value={createDescription}
        placeholder="Description (optional)"
        class="h-8 rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
      />
      <div class="flex items-center gap-2">
        <label
          class="inline-flex items-center gap-1.5 text-xs text-muted-foreground"
        >
          <input type="checkbox" bind:checked={createEnabled} />
          Enabled
        </label>
        <button
          type="button"
          disabled={creating}
          onclick={handleCreateRule}
          class="inline-flex items-center gap-1 rounded-md border border-primary/50 bg-primary/10 px-2 py-1 text-xs text-primary transition-colors hover:bg-primary/20 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <PlusIcon size={12} />
          {creating ? "Creating..." : "Create"}
        </button>
      </div>
    </div>
    {#if feedback}
      <p
        class={`mt-2 text-xs ${feedback.tone === "error" ? "text-destructive" : "text-primary"}`}
      >
        {feedback.text}
      </p>
    {/if}
  </section>

  <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_26rem]">
    <div class="min-h-0 overflow-hidden rounded-lg border border-border/70 bg-background/60">
      {#if dashboard.loading && !dashboard.data}
        <div class="space-y-2 p-3">
          {#each Array(5) as _, index (index)}
            <Skeleton class="h-11 w-full" />
          {/each}
        </div>
      {:else if rules.length === 0}
        <div class="flex min-h-[18rem] items-center justify-center">
          <EmptyState
            message="No EventBridge rules configured yet."
            icon={BridgeIcon}
          />
        </div>
      {:else}
        <div class="h-full overflow-auto">
          <Table>
            <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
              <TableRow class="hover:bg-transparent">
                <TableHead class="w-[13rem]">Rule</TableHead>
                <TableHead>Schedule</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Targets</TableHead>
                <TableHead>Next/Last</TableHead>
                <TableHead>Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each rules as rule}
                <TableRow
                  class={`cursor-pointer ${selectedRuleName === rule.name ? "bg-muted/50" : ""}`}
                  onclick={() => (selectedRuleName = rule.name)}
                >
                  <TableCell class="w-[13rem] max-w-[13rem] align-top !whitespace-normal">
                    <div class="space-y-1">
                      <p
                        class="line-clamp-2 break-words text-xs font-semibold leading-4 text-foreground"
                        title={rule.name}
                      >
                        {rule.name}
                      </p>
                      <p
                        class="truncate font-mono text-[11px] text-muted-foreground/70"
                        title={rule.arn}
                      >
                        {rule.arn}
                      </p>
                    </div>
                  </TableCell>
                  <TableCell class="font-mono text-xs text-muted-foreground">
                    {rule.scheduleExpression}
                  </TableCell>
                  <TableCell>
                    <span class="inline-flex items-center gap-1.5 text-xs">
                      <LedDot color={stateColor(rule.state)} />
                      <span class="text-muted-foreground">{rule.state}</span>
                    </span>
                  </TableCell>
                  <TableCell class="font-mono text-xs text-muted-foreground">
                    {rule.targets?.length ?? 0}
                  </TableCell>
                  <TableCell>
                    <div class="space-y-0.5 text-[11px] text-muted-foreground/80">
                      <p>next {formatTime(rule.nextRunAt)}</p>
                      <p>last {formatTime(rule.lastRunAt)}</p>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div class="flex items-center gap-1">
                      <button
                        type="button"
                        disabled={togglingRuleName === rule.name}
                        onclick={(event) => {
                          event.stopPropagation();
                          handleToggleRule(rule.name, rule.state === "DISABLED");
                        }}
                        class="rounded border border-border px-2 py-1 text-[10px] text-muted-foreground transition-colors hover:bg-muted disabled:opacity-50"
                      >
                        {rule.state === "DISABLED" ? "Enable" : "Disable"}
                      </button>
                      <button
                        type="button"
                        disabled={firingRuleName === rule.name}
                        onclick={(event) => {
                          event.stopPropagation();
                          handleFireRule(rule.name);
                        }}
                        class="inline-flex items-center gap-1 rounded border border-primary/50 bg-primary/10 px-2 py-1 text-[10px] text-primary transition-colors hover:bg-primary/20 disabled:opacity-50"
                      >
                        <PlayIcon size={10} />
                        Fire
                      </button>
                    </div>
                  </TableCell>
                </TableRow>
              {/each}
            </TableBody>
          </Table>
        </div>
      {/if}
    </div>

    {#if selectedRule}
      <section class="rounded-lg border border-border bg-card p-3 space-y-3">
        <div class="space-y-1 border-b border-border pb-2">
          <h3 class="text-sm font-semibold text-foreground">
            {selectedRule.name}
          </h3>
          <p class="font-mono text-[11px] text-muted-foreground/70">
            {selectedRule.arn}
          </p>
        </div>

        <div class="space-y-2">
          <label
            for="eventbridge-edit-schedule"
            class="block text-[11px] text-muted-foreground"
            >Schedule expression</label
          >
          <input
            id="eventbridge-edit-schedule"
            type="text"
            bind:value={editScheduleExpression}
            class="h-8 w-full rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
          />

          <label
            for="eventbridge-edit-description"
            class="block text-[11px] text-muted-foreground">Description</label
          >
          <input
            id="eventbridge-edit-description"
            type="text"
            bind:value={editDescription}
            class="h-8 w-full rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
          />

          <label
            class="inline-flex items-center gap-1.5 text-xs text-muted-foreground"
          >
            <input type="checkbox" bind:checked={editEnabled} />
            Enabled
          </label>

          <div class="flex items-center gap-2">
            <button
              type="button"
              disabled={saving}
              onclick={handleSaveRule}
              class="inline-flex items-center gap-1 rounded border border-primary/50 bg-primary/10 px-2 py-1 text-xs text-primary transition-colors hover:bg-primary/20 disabled:opacity-50"
            >
              {saving ? "Saving..." : "Save"}
            </button>
            <button
              type="button"
              disabled={deleting}
              onclick={handleDeleteRule}
              class="inline-flex items-center gap-1 rounded border border-destructive/40 bg-destructive/10 px-2 py-1 text-xs text-destructive transition-colors hover:bg-destructive/20 disabled:opacity-50"
            >
              <TrashIcon size={10} />
              {deleting ? "Deleting..." : "Delete"}
            </button>
          </div>
        </div>

        <div class="rounded-md border border-border p-2 space-y-2">
          <div class="flex items-center justify-between">
            <h4 class="text-xs font-semibold text-foreground">Targets</h4>
            <span class="text-[11px] font-mono text-muted-foreground">
              {selectedRule.targets?.length ?? 0} total
            </span>
          </div>

          <div class="grid gap-2">
            <div class="grid gap-2 md:grid-cols-2">
              <input
                type="text"
                bind:value={targetIdDraft}
                placeholder="target id"
                class="h-8 rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
              />
              <select
                bind:value={targetFunctionDraft}
                class="h-8 rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
              >
                <option value="">Select function</option>
                {#each functions as fn (fn.name)}
                  <option value={fn.name}>{fn.name}</option>
                {/each}
              </select>
            </div>

            <input
              type="text"
              bind:value={targetInputDraft}
              placeholder="Input override JSON (optional)"
              class="h-8 rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
            />
            <input
              type="text"
              bind:value={targetInputPathDraft}
              placeholder="InputPath (optional, e.g. $.detail)"
              class="h-8 rounded border border-border bg-background px-2 text-xs text-foreground outline-none focus:ring-1 focus:ring-primary"
            />

            <button
              type="button"
              disabled={addingTarget}
              onclick={handleAddTarget}
              class="inline-flex items-center justify-center gap-1 rounded border border-primary/50 bg-primary/10 px-2 py-1 text-xs text-primary transition-colors hover:bg-primary/20 disabled:opacity-50"
            >
              <PlusIcon size={11} />
              {addingTarget ? "Adding..." : "Add target"}
            </button>
          </div>

          {#if (selectedRule.targets?.length ?? 0) > 0}
            <div
              class="max-h-56 overflow-y-auto rounded border border-border/60"
            >
              {#each selectedRule.targets ?? [] as target (target.id)}
                <label
                  class="flex items-start gap-2 border-b border-border/50 px-2 py-1.5 text-xs last:border-b-0"
                >
                  <input
                    type="checkbox"
                    checked={selectedTargetIDs.has(target.id)}
                    onchange={(event) =>
                      toggleTargetSelection(
                        target.id,
                        (event.currentTarget as HTMLInputElement).checked,
                      )}
                  />
                  <div class="min-w-0 flex-1 space-y-0.5">
                    <div class="flex items-center gap-2">
                      <span class="font-mono text-foreground">{target.id}</span>
                      <span class="text-[10px] text-muted-foreground/70 font-mono">Lambda</span>
                    </div>
                    <ArnCell
                      name={lambdaNameFromTargetArn(target.arn)}
                      arn={target.arn}
                    />
                    {#if target.lastResult}
                      <p class="text-[11px] text-muted-foreground/70">
                        last result: {target.lastResult}
                      </p>
                    {/if}
                  </div>
                </label>
              {/each}
            </div>

            <button
              type="button"
              disabled={selectedTargetCount === 0 || removingTargets}
              onclick={handleRemoveSelectedTargets}
              class="inline-flex items-center justify-center gap-1 rounded border border-destructive/40 bg-destructive/10 px-2 py-1 text-xs text-destructive transition-colors hover:bg-destructive/20 disabled:opacity-50"
            >
              <TrashIcon size={11} />
              {removingTargets
                ? "Removing..."
                : `Remove selected (${selectedTargetCount})`}
            </button>
          {:else}
            <p class="text-xs text-muted-foreground/70">
              No targets configured for this rule.
            </p>
          {/if}
        </div>

      </section>
    {:else}
      <section
        class="rounded-lg border border-border bg-card flex min-h-56 items-center justify-center px-4 py-6 text-center text-sm text-muted-foreground"
      >
        Select a rule to edit schedule, targets, and run manual fires.
      </section>
    {/if}
  </div>
</div>
