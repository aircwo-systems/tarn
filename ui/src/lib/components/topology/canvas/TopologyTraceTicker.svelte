<script lang="ts">
  import type { RequestTrace } from "$lib/types";
  import Badge from "$lib/components/ui/badge/badge.svelte";
  import { Card, CardContent, CardHeader } from "$lib/components/ui/card";

  let {
    traces = [],
    selectedTraceId = null,
    embedded = false,
    onSelect = (_id: string) => {},
  }: {
    traces?: RequestTrace[];
    selectedTraceId?: string | null;
    embedded?: boolean;
    onSelect?: (id: string) => void;
  } = $props();

  function statusBadgeVariant(
    status: number,
  ): "default" | "secondary" | "destructive" | "outline" {
    if (status >= 500) return "destructive";
    if (status >= 400) return "secondary";
    return "outline";
  }

  function traceLabel(trace: RequestTrace): string {
    const method = trace.method ?? "";
    const path = trace.path ?? "";
    const eventBridge = trace.spans.find((span) => span.kind === "eventbridge");
    if (eventBridge) return `EVENTBRIDGE ${eventBridge.name}`;
    return `${method} ${path}`.trim() || `trace:${trace.id.slice(0, 8)}`;
  }

  // Keep 10 recent requests visible; scroll after that.
  const TRACE_VISIBLE_LIMIT = 10;
  const TRACE_ROW_HEIGHT_REM = 2.5;
  const TRACE_ROW_GAP_REM = 0.25;
  const traceListMaxHeight = `calc(${TRACE_VISIBLE_LIMIT} * ${TRACE_ROW_HEIGHT_REM}rem + ${
    TRACE_VISIBLE_LIMIT - 1
  } * ${TRACE_ROW_GAP_REM}rem)`;
</script>

<Card>
  <CardHeader class="border-b border-border">
    <div class="flex items-center justify-between">
      <h4 class="text-sm font-semibold text-foreground">Recent Requests</h4>
      <span class="font-mono text-[10px] text-muted-foreground/70"
        >{traces.length}</span
      >
    </div>
  </CardHeader>
  <CardContent class="p-3">
    {#if traces.length > 0}
      <div>
        <div
          class="space-y-1 overflow-y-auto pr-1"
          style={`max-height: ${traceListMaxHeight};`}
        >
          {#each traces as trace (trace.id)}
            <button
              type="button"
              class={`group flex w-full items-center gap-2 rounded-md border px-2 py-1.5 text-left transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring ${
                selectedTraceId === trace.id
                  ? "border-primary/50 bg-primary/10 shadow-sm"
                  : "border-transparent hover:border-border/80 hover:bg-background"
              }`}
              onclick={() => onSelect(trace.id)}
            >
              <Badge
                variant={statusBadgeVariant(trace.status)}
                class="h-5 min-w-9 justify-center px-1.5 font-mono text-[10px]"
              >
                {trace.status}
              </Badge>

              <div class="min-w-0 flex-1">
                <p
                  class="truncate text-xs text-foreground/90 group-hover:text-foreground"
                >
                  {traceLabel(trace)}
                </p>
                <p class="font-mono text-[10px] text-muted-foreground">
                  {trace.durationMs}ms
                </p>
              </div>
            </button>
          {/each}
        </div>
      </div>
    {:else}
      <div
        class="flex h-48 items-center justify-center rounded-md border border-dashed border-border/80 bg-muted/20 text-xs text-muted-foreground"
      >
        No recent requests yet
      </div>
    {/if}
  </CardContent>
</Card>
