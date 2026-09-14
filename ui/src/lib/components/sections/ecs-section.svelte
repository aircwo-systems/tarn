<script lang="ts">
  import { CubeIcon } from "phosphor-svelte";
  import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from "$lib/components/ui/table";
  import { Badge } from "$lib/components/ui/badge";
  import { Skeleton } from "$lib/components/ui/skeleton";
  import ArnCell from "$lib/components/common/arn-cell.svelte";
  import EmptyState from "$lib/components/common/empty-state.svelte";
  import LedDot from "$lib/components/common/led-dot.svelte";
  import MetricCard from "$lib/components/common/metric-card.svelte";
  import SectionHeader from "./section-header.svelte";
  import ECSDetailsDialog from "./ecs-details-dialog.svelte";
  import { getDashboard } from "$lib/state.svelte";
  import type {
    ECSClusterSummary,
    ECSDetail,
    ECSServiceSummary,
    ECSTaskSummary,
  } from "$lib/types";

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const ecs = $derived(dashboard.data?.ecs);
  const clusters = $derived(ecs?.clusters ?? []);
  const services = $derived(ecs?.services ?? []);
  const tasks = $derived(ecs?.tasks ?? []);
  const taskDefinitions = $derived(ecs?.taskDefinitions ?? []);

  let selectedDetail = $state<ECSDetail | null>(null);
  let detailsOpen = $state(false);

  function detailTaskDefinitionArn(detail: ECSDetail | null): string | null {
    if (!detail) return null;

    switch (detail.kind) {
      case "cluster":
        return null;
      case "service":
      case "task":
        return detail.resource.taskDefinitionArn;
    }
  }

  const selectedTaskDefinition = $derived(
    taskDefinitions.find(
      (definition) => definition.arn === detailTaskDefinitionArn(selectedDetail),
    ) ?? null,
  );
  const runningTasks = $derived(
    tasks.filter((task) => normalizedStatus(task.lastStatus) === "RUNNING").length,
  );
  const pendingTasks = $derived(
    tasks.filter((task) =>
      ["PROVISIONING", "PENDING"].includes(normalizedStatus(task.lastStatus)),
    ).length,
  );
  const activeServices = $derived(
    services.filter((service) => normalizedStatus(service.status) === "ACTIVE").length,
  );

  const numberFormatter = new Intl.NumberFormat("en-GB");

  function normalizedStatus(status?: string): string {
    return status?.trim().toUpperCase() || "UNKNOWN";
  }

  function statusLabel(status?: string): string {
    return status?.trim() || "UNKNOWN";
  }

  function statusColor(status?: string): "green" | "amber" | "red" | "gray" {
    const normalized = normalizedStatus(status);
    if (["ACTIVE", "RUNNING", "SUCCEEDED"].includes(normalized)) return "green";
    if (
      [
        "PROVISIONING",
        "PENDING",
        "DEACTIVATING",
        "STOPPING",
        "DEPROVISIONING",
        "DRAINING",
      ].includes(normalized)
    ) {
      return "amber";
    }
    if (["INACTIVE", "STOPPED", "FAILED", "ERROR"].includes(normalized)) return "red";
    return "gray";
  }

  function formatCount(value?: number): string {
    return numberFormatter.format(value ?? 0);
  }

  function resourceTail(value?: string): string {
    const trimmed = value?.trim();
    if (!trimmed) return "--";
    const parts = trimmed.split("/");
    return parts[parts.length - 1] || trimmed;
  }

  function resourceName(name: string | undefined, arn: string | undefined, fallback: string): string {
    const trimmedName = name?.trim();
    if (trimmedName && trimmedName !== "--") return trimmedName;
    const tail = resourceTail(arn);
    return tail === "--" ? fallback : tail;
  }

  function clusterName(arn: string): string {
    return resourceTail(arn);
  }

  function taskDefinitionName(arn: string): string {
    return resourceTail(arn);
  }

  function taskDefinitionDisplayName(arn: string): string {
    const definition = taskDefinitions.find((item) => item.arn === arn);
    return definition ? `${definition.family}:${definition.revision}` : taskDefinitionName(arn);
  }

  function formatTime(value?: string): string {
    if (!value) return "--";
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }

  function clusterTaskSummary(cluster: ECSClusterSummary): string {
    return `${formatCount(cluster.runningTasks)} running · ${formatCount(cluster.pendingTasks)} pending`;
  }

  function serviceTaskSummary(service: ECSServiceSummary): string {
    return `${formatCount(service.desiredCount)} desired · ${formatCount(service.runningCount)} running`;
  }

  function taskDisplayName(task: ECSTaskSummary): string {
    return resourceName(undefined, task.arn, "Unnamed task");
  }

  function openDetail(detail: ECSDetail): void {
    selectedDetail = detail;
    detailsOpen = true;
  }

  function onRowClick(event: MouseEvent, detail: ECSDetail): void {
    const target = event.target;
    if (target instanceof Element && target.closest("button, a")) return;
    openDetail(detail);
  }

  function onRowKeydown(event: KeyboardEvent, detail: ECSDetail): void {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      openDetail(detail);
    }
  }
</script>

<div class="flex min-h-full flex-col gap-4 pb-16 md:pb-0">
  <SectionHeader
    title="ECS"
    description="Clusters, services, and task lifecycle reported by Tarn."
    icon={CubeIcon}
    {sidebarCollapsed}
    {onToggleSidebar}
  />

  <!-- Metric Cards Row -->
  <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
    <MetricCard
      label="Clusters"
      value={formatCount(clusters.length)}
      sub="Registered compute clusters"
    />
    <MetricCard
      label="Active Services"
      value={formatCount(activeServices)}
      valueColor="var(--accent-green)"
      sub="{services.length} total provisioned"
    />
    <MetricCard
      label="Running Tasks"
      value={formatCount(runningTasks)}
      valueColor="var(--accent-green)"
      sub="{tasks.length} total tasks"
    />
    <MetricCard
      label="Pending Tasks"
      value={formatCount(pendingTasks)}
      valueColor={pendingTasks > 0 ? "var(--accent-amber)" : "var(--text-tertiary)"}
      sub={pendingTasks > 0 ? "Provisioning in progress" : "No pending queue"}
    />
  </div>

  {#if dashboard.loading && !dashboard.data}
    <div class="grid gap-4 xl:grid-cols-3">
      {#each Array(3) as _, index (index)}
        <div class="space-y-2 rounded-md border border-border/70 bg-card/30 p-3">
          <Skeleton class="h-5 w-28" />
          <Skeleton class="h-9 w-full" />
          <Skeleton class="h-9 w-full" />
          <Skeleton class="h-9 w-full" />
        </div>
      {/each}
    </div>
  {:else if !ecs}
    <div class="flex min-h-[22rem] flex-1 items-center justify-center rounded-md border border-border/70 bg-card/30 px-4 text-center">
      <EmptyState
        icon={CubeIcon}
        message="ECS data is not available in this Tarn overview yet."
      />
    </div>
  {:else}
    <div class="grid min-w-0 gap-4 xl:grid-cols-3">
      <section class="min-w-0 overflow-hidden rounded-md border border-border/70 bg-card/30">
        <div class="flex items-start justify-between gap-4 border-b border-border/70 px-4 py-3">
          <div class="min-w-0">
            <h2 class="text-sm font-semibold text-foreground">Clusters</h2>
            <p class="mt-1 text-[11px] text-muted-foreground/70">Capacity and task counts by cluster.</p>
          </div>
          <span class="shrink-0 font-mono text-sm text-foreground">{formatCount(clusters.length)}</span>
        </div>

        {#if clusters.length === 0}
          <div class="flex min-h-48 items-center justify-center px-4">
            <EmptyState icon={CubeIcon} message="No ECS clusters reported." />
          </div>
        {:else}
          <div class="max-h-[34rem] overflow-auto">
            <Table>
              <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
                <TableRow class="hover:bg-transparent">
                  <TableHead>Cluster</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Tasks</TableHead>
                  <TableHead>Services</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {#each clusters as cluster (cluster.arn)}
                  <TableRow
                    class="cursor-pointer transition-colors hover:bg-muted/30 focus-visible:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                    role="button"
                    tabindex={0}
                    aria-label={`Open details for ECS cluster ${resourceName(cluster.name, cluster.arn, "Unnamed cluster")}`}
                    onclick={(event: MouseEvent) =>
                      onRowClick(event, { kind: "cluster", resource: cluster })}
                    onkeydown={(event: KeyboardEvent) =>
                      onRowKeydown(event, { kind: "cluster", resource: cluster })}
                  >
                    <TableCell class="max-w-[15rem]">
                      <ArnCell
                        name={resourceName(cluster.name, cluster.arn, "Unnamed cluster")}
                        arn={cluster.arn || "--"}
                      />
                    </TableCell>
                    <TableCell>
                      <span class="inline-flex items-center gap-1.5 text-xs">
                        <LedDot color={statusColor(cluster.status)} />
                        <span class="text-muted-foreground">{statusLabel(cluster.status)}</span>
                      </span>
                    </TableCell>
                    <TableCell class="font-mono text-[11px] text-muted-foreground">
                      {clusterTaskSummary(cluster)}
                    </TableCell>
                    <TableCell class="font-mono text-xs text-muted-foreground">
                      {formatCount(cluster.activeServices)}
                    </TableCell>
                  </TableRow>
                {/each}
              </TableBody>
            </Table>
          </div>
        {/if}
      </section>

      <section class="min-w-0 overflow-hidden rounded-md border border-border/70 bg-card/30">
        <div class="flex items-start justify-between gap-4 border-b border-border/70 px-4 py-3">
          <div class="min-w-0">
            <h2 class="text-sm font-semibold text-foreground">Services</h2>
            <p class="mt-1 text-[11px] text-muted-foreground/70">Desired replicas and reconciliation state.</p>
          </div>
          <span class="shrink-0 font-mono text-sm text-foreground">{formatCount(services.length)}</span>
        </div>

        {#if services.length === 0}
          <div class="flex min-h-48 items-center justify-center px-4">
            <EmptyState icon={CubeIcon} message="No ECS services reported." />
          </div>
        {:else}
          <div class="max-h-[34rem] overflow-auto">
            <Table>
              <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
                <TableRow class="hover:bg-transparent">
                  <TableHead>Service</TableHead>
                  <TableHead>Cluster</TableHead>
                  <TableHead>Task definition</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Replicas</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {#each services as service (service.arn)}
                  <TableRow
                    class="cursor-pointer transition-colors hover:bg-muted/30 focus-visible:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                    role="button"
                    tabindex={0}
                    aria-label={`Open details for ECS service ${resourceName(service.name, service.arn, "Unnamed service")}`}
                    onclick={(event: MouseEvent) =>
                      onRowClick(event, { kind: "service", resource: service })}
                    onkeydown={(event: KeyboardEvent) =>
                      onRowKeydown(event, { kind: "service", resource: service })}
                  >
                    <TableCell class="max-w-[14rem]">
                      <ArnCell
                        name={resourceName(service.name, service.arn, "Unnamed service")}
                        arn={service.arn || "--"}
                      />
                    </TableCell>
                    <TableCell class="max-w-[10rem] truncate font-mono text-xs text-muted-foreground" title={service.clusterArn}>
                      {clusterName(service.clusterArn)}
                    </TableCell>
                    <TableCell class="max-w-[10rem] truncate font-mono text-[11px] text-muted-foreground" title={service.taskDefinitionArn}>
                      {taskDefinitionDisplayName(service.taskDefinitionArn)}
                    </TableCell>
                    <TableCell>
                      <span class="inline-flex items-center gap-1.5 text-xs">
                        <LedDot color={statusColor(service.status)} />
                        <span class="text-muted-foreground">{statusLabel(service.status)}</span>
                      </span>
                      {#if service.launchType}
                        <Badge class="mt-1" variant="outline">{service.launchType}</Badge>
                      {/if}
                    </TableCell>
                    <TableCell class="font-mono text-[11px] text-muted-foreground" title={serviceTaskSummary(service)}>
                      <span class="text-foreground">{formatCount(service.desiredCount)}</span>
                      <span class="text-muted-foreground/50"> / </span>
                      <span>{formatCount(service.runningCount)}</span>
                      <span class="block text-[10px] text-muted-foreground/60">
                        {formatCount(service.pendingCount)} pending
                      </span>
                    </TableCell>
                  </TableRow>
                {/each}
              </TableBody>
            </Table>
          </div>
        {/if}
      </section>

      <section class="min-w-0 overflow-hidden rounded-md border border-border/70 bg-card/30">
        <div class="flex items-start justify-between gap-4 border-b border-border/70 px-4 py-3">
          <div class="min-w-0">
            <h2 class="text-sm font-semibold text-foreground">Tasks</h2>
            <p class="mt-1 text-[11px] text-muted-foreground/70">Individual task lifecycle and placement.</p>
          </div>
          <span class="shrink-0 font-mono text-sm text-foreground">{formatCount(tasks.length)}</span>
        </div>

        {#if tasks.length === 0}
          <div class="flex min-h-48 items-center justify-center px-4">
            <EmptyState icon={CubeIcon} message="No ECS tasks reported." />
          </div>
        {:else}
          <div class="max-h-[34rem] overflow-auto">
            <Table>
              <TableHeader class="sticky top-0 z-10 bg-background/95 backdrop-blur [&_th]:bg-background/95">
                <TableRow class="hover:bg-transparent">
                  <TableHead>Task</TableHead>
                  <TableHead>Cluster</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Definition</TableHead>
                  <TableHead>Started</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {#each tasks as task (task.arn)}
                  <TableRow
                    class="cursor-pointer transition-colors hover:bg-muted/30 focus-visible:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                    role="button"
                    tabindex={0}
                    aria-label={`Open details for ECS task ${taskDisplayName(task)}`}
                    onclick={(event: MouseEvent) =>
                      onRowClick(event, { kind: "task", resource: task })}
                    onkeydown={(event: KeyboardEvent) =>
                      onRowKeydown(event, { kind: "task", resource: task })}
                  >
                    <TableCell class="max-w-[14rem]">
                      <ArnCell name={taskDisplayName(task)} arn={task.arn || "--"} />
                      {#if task.group}
                        <p class="mt-1 truncate text-[10px] text-muted-foreground/60" title={task.group}>
                          {task.group}
                        </p>
                      {/if}
                    </TableCell>
                    <TableCell class="max-w-[9rem] truncate font-mono text-xs text-muted-foreground" title={task.clusterArn}>
                      {clusterName(task.clusterArn)}
                    </TableCell>
                    <TableCell>
                      <span class="inline-flex items-center gap-1.5 text-xs">
                        <LedDot color={statusColor(task.lastStatus)} />
                        <span class="text-muted-foreground">{statusLabel(task.lastStatus)}</span>
                      </span>
                      <span class="mt-1 block text-[10px] text-muted-foreground/60">
                        desired {statusLabel(task.desiredStatus)}
                      </span>
                    </TableCell>
                    <TableCell class="max-w-[10rem] truncate font-mono text-[11px] text-muted-foreground" title={task.taskDefinitionArn}>
                      {taskDefinitionName(task.taskDefinitionArn)}
                    </TableCell>
                    <TableCell class="whitespace-nowrap text-[11px] text-muted-foreground/80">
                      {formatTime(task.startedAt)}
                    </TableCell>
                  </TableRow>
                {/each}
              </TableBody>
            </Table>
          </div>
        {/if}
      </section>
    </div>

  {/if}
</div>

<ECSDetailsDialog
  bind:open={detailsOpen}
  detail={selectedDetail}
  taskDefinition={selectedTaskDefinition}
/>
