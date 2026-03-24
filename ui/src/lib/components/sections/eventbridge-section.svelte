<script lang="ts">
  import {
    ArrowsClockwiseIcon,
    PlusIcon,
    PlayIcon,
    TrashIcon,
    BridgeIcon,
  } from "phosphor-svelte";
  import { TableCell, TableRow } from "$lib/components/ui/table";
  import Badge from "$lib/components/ui/badge/badge.svelte";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import ResourceTable from "$lib/components/common/resource-table.svelte";
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

  function scheduleBadgeVariant(
    scheduleExpression: string,
  ): "default" | "secondary" {
    return scheduleExpression.startsWith("rate(") ? "secondary" : "default";
  }

  function stateBadgeVariant(
    state: string,
  ): "default" | "destructive" | "secondary" {
    if (state === "ENABLED") return "default";
    if (state === "DISABLED") return "destructive";
    return "secondary";
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
  <section class="rounded-lg border border-border bg-card px-4 py-3">
    <div class="flex items-center justify-between gap-3">
      <div>
        <h2 class="text-sm font-semibold text-foreground">EventBridge Rules</h2>
        <p class="text-[10px] font-mono text-muted-foreground/70">
          Scheduled rules on the default bus, with Lambda targets.
        </p>
      </div>
      <Badge variant="secondary"
        >{rules.length} rule{rules.length === 1 ? "" : "s"}</Badge
      >
    </div>

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
    <ResourceTable
      title="Rules"
      count={rules.length}
      loading={dashboard.loading && !dashboard.data}
      empty={rules.length === 0}
      emptyMessage="No EventBridge rules configured yet."
      emptyIcon={BridgeIcon}
      columns={["Rule", "Schedule", "State", "Targets", "Next/Last", "Actions"]}
      onRefresh={refresh}
    >
      {#each rules as rule}
        <TableRow
          class={`cursor-pointer ${selectedRuleName === rule.name ? "bg-muted/50" : ""}`}
          onclick={() => (selectedRuleName = rule.name)}
        >
          <TableCell>
            <div class="space-y-0.5">
              <p class="text-xs font-semibold text-foreground">{rule.name}</p>
              <p class="font-mono text-[11px] text-muted-foreground/70">
                {rule.arn}
              </p>
            </div>
          </TableCell>
          <TableCell>
            <Badge variant={scheduleBadgeVariant(rule.scheduleExpression)}>
              {rule.scheduleExpression}
            </Badge>
          </TableCell>
          <TableCell>
            <Badge variant={stateBadgeVariant(rule.state)}>{rule.state}</Badge>
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
    </ResourceTable>

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
                      <Badge variant="secondary">Lambda</Badge>
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

        <div
          class="rounded border border-border/60 bg-muted/30 px-2 py-2 text-xs"
        >
          <p class="font-medium text-foreground">Last result</p>
          <p class="mt-1 text-muted-foreground">
            {selectedRule.lastResult || "—"}
          </p>
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
