<script lang="ts">
  import * as Dialog from "$lib/components/ui/dialog";
  import { Badge } from "$lib/components/ui/badge";
  import LedDot from "$lib/components/common/led-dot.svelte";
  import { CubeIcon, XIcon } from "phosphor-svelte";
  import type {
    ECSDetail,
    ECSTaskDefinitionSummary,
  } from "$lib/types";

  interface Props {
    open?: boolean;
    detail: ECSDetail | null;
    taskDefinition: ECSTaskDefinitionSummary | null;
  }

  let {
    open = $bindable(false),
    detail,
    taskDefinition,
  }: Props = $props();

  function resourceTail(value?: string): string {
    const trimmed = value?.trim();
    if (!trimmed) return "--";
    const parts = trimmed.split("/");
    return parts[parts.length - 1] || trimmed;
  }

  function detailName(value: ECSDetail): string {
    switch (value.kind) {
      case "cluster":
        return value.resource.name || resourceTail(value.resource.arn);
      case "service":
        return value.resource.name || resourceTail(value.resource.arn);
      case "task":
        return resourceTail(value.resource.arn);
    }
  }

  function detailLabel(value: ECSDetail): string {
    switch (value.kind) {
      case "cluster":
        return "ECS cluster";
      case "service":
        return "ECS service";
      case "task":
        return "ECS task";
    }
  }

  function detailArn(value: ECSDetail): string {
    return value.resource.arn || "--";
  }

  function statusColor(status?: string): "green" | "amber" | "red" | "gray" {
    switch (status?.trim().toUpperCase()) {
      case "ACTIVE":
      case "RUNNING":
        return "green";
      case "PROVISIONING":
      case "PENDING":
      case "DEACTIVATING":
      case "STOPPING":
      case "DEPROVISIONING":
      case "DRAINING":
        return "amber";
      case "INACTIVE":
      case "STOPPED":
      case "FAILED":
      case "ERROR":
        return "red";
      default:
        return "gray";
    }
  }

  function statusLabel(status?: string): string {
    return status?.trim() || "UNKNOWN";
  }

  function formatTime(value?: string): string {
    if (!value) return "--";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }

  function taskDefinitionLabel(
    arn: string,
    definition: ECSTaskDefinitionSummary | null,
  ): string {
    if (definition?.family) return `${definition.family}:${definition.revision}`;
    return resourceTail(arn);
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Content
    showCloseButton={false}
    class="flex max-h-[88vh] w-full max-w-2xl flex-col gap-0 overflow-hidden border-border/80 p-0 sm:max-w-3xl"
  >
    {#if detail}
      <div class="flex shrink-0 items-center justify-between gap-3 border-b border-border/60 px-5 py-4">
        <div class="flex min-w-0 items-center gap-3">
          <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-border bg-muted/40 text-primary">
            <CubeIcon size={17} />
          </div>
          <div class="min-w-0">
            <Dialog.Title class="truncate font-mono text-[15px] font-semibold tracking-tight text-foreground">
              {detailName(detail)}
            </Dialog.Title>
            <p class="mt-0.5 font-mono text-[11px] text-muted-foreground/55">{detailLabel(detail)}</p>
          </div>
        </div>
        <Dialog.Close
          class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-border/60 text-muted-foreground transition-colors hover:border-border hover:bg-muted/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label="Close ECS details"
        >
          <XIcon size={14} />
        </Dialog.Close>
      </div>

      <div class="min-h-0 overflow-y-auto px-5 py-5">
        {#if detail.kind === "cluster"}
          <div class="grid grid-cols-2 gap-x-8 gap-y-4 border-y border-border/60 py-4 sm:grid-cols-4">
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Status</p>
              <p class="mt-1 inline-flex items-center gap-1.5 font-mono text-[13px] font-semibold text-foreground">
                <LedDot color={statusColor(detail.resource.status)} />
                {statusLabel(detail.resource.status)}
              </p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Running tasks</p>
              <p class="mt-1 font-mono text-[13px] font-semibold tabular-nums text-foreground">{detail.resource.runningTasks}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Pending tasks</p>
              <p class="mt-1 font-mono text-[13px] font-semibold tabular-nums text-foreground">{detail.resource.pendingTasks}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Active services</p>
              <p class="mt-1 font-mono text-[13px] font-semibold tabular-nums text-foreground">{detail.resource.activeServices}</p>
            </div>
          </div>

          <div class="mt-6 border-t border-border/40 pt-5">
            <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Cluster ARN</p>
            <p class="mt-2 break-all font-mono text-[11px] leading-relaxed text-muted-foreground/75">{detailArn(detail)}</p>
          </div>
        {:else if detail.kind === "service"}
          <div class="grid grid-cols-2 gap-x-8 gap-y-4 border-y border-border/60 py-4 sm:grid-cols-3">
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Status</p>
              <p class="mt-1 inline-flex items-center gap-1.5 font-mono text-[13px] font-semibold text-foreground">
                <LedDot color={statusColor(detail.resource.status)} />
                {statusLabel(detail.resource.status)}
              </p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Launch type</p>
              <p class="mt-1 font-mono text-[13px] font-semibold text-foreground">{detail.resource.launchType || "--"}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Cluster</p>
              <p class="mt-1 truncate font-mono text-[13px] font-semibold text-foreground" title={detail.resource.clusterArn}>{resourceTail(detail.resource.clusterArn)}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Desired</p>
              <p class="mt-1 font-mono text-[13px] font-semibold tabular-nums text-foreground">{detail.resource.desiredCount}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Running</p>
              <p class="mt-1 font-mono text-[13px] font-semibold tabular-nums text-foreground">{detail.resource.runningCount}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Pending</p>
              <p class="mt-1 font-mono text-[13px] font-semibold tabular-nums text-foreground">{detail.resource.pendingCount}</p>
            </div>
          </div>

          <div class="mt-6 border-t border-border/40 pt-5">
            <div class="flex items-start justify-between gap-3">
              <div>
                <h2 class="text-[10px] uppercase tracking-widest text-muted-foreground/55">Deployed task definition</h2>
                <p class="mt-1 text-xs text-muted-foreground/70">The revision this service uses for new tasks.</p>
              </div>
              {#if taskDefinition}
                <Badge variant="outline">{statusLabel(taskDefinition.status)}</Badge>
              {/if}
            </div>
            <div class="mt-3 border border-border/60 bg-muted/20 px-3 py-3">
              <p class="font-mono text-sm font-semibold text-foreground">{taskDefinitionLabel(detail.resource.taskDefinitionArn, taskDefinition)}</p>
              <p class="mt-1 break-all font-mono text-[11px] leading-relaxed text-muted-foreground/70" title={detail.resource.taskDefinitionArn}>
                {detail.resource.taskDefinitionArn || "--"}
              </p>
            </div>
          </div>

          <div class="mt-6 border-t border-border/40 pt-5">
            <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Service ARN</p>
            <p class="mt-2 break-all font-mono text-[11px] leading-relaxed text-muted-foreground/75">{detailArn(detail)}</p>
          </div>
        {:else}
          <div class="grid grid-cols-2 gap-x-8 gap-y-4 border-y border-border/60 py-4 sm:grid-cols-3">
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Last status</p>
              <p class="mt-1 inline-flex items-center gap-1.5 font-mono text-[13px] font-semibold text-foreground">
                <LedDot color={statusColor(detail.resource.lastStatus)} />
                {statusLabel(detail.resource.lastStatus)}
              </p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Desired status</p>
              <p class="mt-1 font-mono text-[13px] font-semibold text-foreground">{statusLabel(detail.resource.desiredStatus)}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Launch type</p>
              <p class="mt-1 font-mono text-[13px] font-semibold text-foreground">{detail.resource.launchType || "--"}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Cluster</p>
              <p class="mt-1 truncate font-mono text-[13px] font-semibold text-foreground" title={detail.resource.clusterArn}>{resourceTail(detail.resource.clusterArn)}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Started</p>
              <p class="mt-1 font-mono text-[13px] font-semibold text-foreground">{formatTime(detail.resource.startedAt)}</p>
            </div>
            <div>
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Stopped</p>
              <p class="mt-1 font-mono text-[13px] font-semibold text-foreground">{formatTime(detail.resource.stoppedAt)}</p>
            </div>
          </div>

          {#if detail.resource.group}
            <div class="mt-5">
              <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Group</p>
              <p class="mt-2 break-all font-mono text-[11px] leading-relaxed text-muted-foreground/75">{detail.resource.group}</p>
            </div>
          {/if}

          {#if detail.resource.stoppedReason}
            <div class="mt-5 border-l-2 border-amber pl-3">
              <p class="text-[10px] uppercase tracking-widest text-amber/80">Stopped reason</p>
              <p class="mt-1 text-xs leading-relaxed text-muted-foreground">{detail.resource.stoppedReason}</p>
            </div>
          {/if}

          <div class="mt-6 border-t border-border/40 pt-5">
            <div class="flex items-start justify-between gap-3">
              <div>
                <h2 class="text-[10px] uppercase tracking-widest text-muted-foreground/55">Task definition</h2>
                <p class="mt-1 text-xs text-muted-foreground/70">The definition used to launch this task.</p>
              </div>
              {#if taskDefinition}
                <Badge variant="outline">{statusLabel(taskDefinition.status)}</Badge>
              {/if}
            </div>
            <div class="mt-3 border border-border/60 bg-muted/20 px-3 py-3">
              <p class="font-mono text-sm font-semibold text-foreground">{taskDefinitionLabel(detail.resource.taskDefinitionArn, taskDefinition)}</p>
              <p class="mt-1 break-all font-mono text-[11px] leading-relaxed text-muted-foreground/70" title={detail.resource.taskDefinitionArn}>
                {detail.resource.taskDefinitionArn || "--"}
              </p>
            </div>
          </div>

          <div class="mt-6 border-t border-border/40 pt-5">
            <p class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Task ARN</p>
            <p class="mt-2 break-all font-mono text-[11px] leading-relaxed text-muted-foreground/75">{detailArn(detail)}</p>
          </div>
        {/if}
      </div>
    {/if}
  </Dialog.Content>
</Dialog.Root>
