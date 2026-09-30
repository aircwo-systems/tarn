<script lang="ts" module>
  import type { InfraProbe } from "$lib/types";
  import type { LogPulse } from "$lib/log-pulse.svelte";

  type ServiceBlock = Pick<
    InfraProbe,
    "name" | "kind" | "status" | "latencyMs"
  > & {
    id: string;
    linkedFunctions: number;
  };

  type WidgetBlock = {
    id: string;
    title: string;
    color: string;
    dimmed: boolean;
  };

  export type SidebarWidgetData =
    | { kind: "services"; services: ServiceBlock[]; stale: boolean }
    | { kind: "logs"; pulse: LogPulse; groups: number }
    | {
        kind: "summary";
        rows: {
          label: string;
          value: string | number;
          tone?: "warning" | "error";
          blocks?: WidgetBlock[];
        }[];
        note?: string;
      };
</script>

<script lang="ts">
  import { timeAgo } from "$lib/utils";
  import { infraKindCssVar } from "$lib/components/topology/topology-canvas-theme";
  let { data }: { data: SidebarWidgetData } = $props();
</script>

<span
  class="widget"
  class:compact-widget={data.kind === "services" ||
    (data.kind === "summary" &&
      data.rows.some((row) => row.blocks !== undefined))}
  class:stale={data.kind === "services" && data.stale}
>
  {#if data.kind === "services"}
    {@const connected = data.services.filter(
      (service) => service.status === "connected",
    ).length}
    {@const linked = data.services.filter(
      (service) => service.linkedFunctions > 0,
    ).length}
    <span class="widget-row">
      <span>Reachable</span>
      <span class="metric-value">
        {#if data.services.length}
          <span
            class="square-meter"
            role="meter"
            aria-label="Reachable services"
            aria-valuemin={0}
            aria-valuemax={data.services.length}
            aria-valuenow={connected}
            aria-valuetext="{connected} reachable, {data.services.length -
              connected} need attention"
          >
            {#each data.services as service (service.id)}
              <span
                class="square-block"
                class:dimmed={service.status !== "connected"}
                style:background={infraKindCssVar(service.kind)}
                class:linked={service.linkedFunctions > 0}
                aria-hidden="true"
                title="{service.name} · {service.status}{service.status ===
                'connected'
                  ? ` · ${service.latencyMs}ms`
                  : ''} · {service.linkedFunctions} linked function{service.linkedFunctions ===
                1
                  ? ''
                  : 's'}"
              ></span>
            {/each}
          </span>
        {/if}
        <b
          >{connected}<span class="value-divider">
            / {data.services.length}</span
          ></b
        >
      </span>
    </span>
    {#if data.services.length}
      <span class="widget-row"
        ><span>Linked to functions</span><b
          >{linked}<span class="value-divider">
            / {data.services.length}</span
          ></b
        ></span
      >
      {#if data.stale}<span class="widget-note warning"
          >Last known state · refresh failed</span
        >{/if}
    {:else}<span class="widget-note">No probes in this view</span>{/if}
  {:else if data.kind === "logs"}
    {@const pulse = data.pulse}
    {@const peak = Math.max(1, ...pulse.bars)}
    {#if pulse.status === "ready"}
      <span class="widget-row"
        ><span>Sample · last 60s</span><b
          >{pulse.bars.reduce((sum, count) => sum + count, 0)} events</b
        ></span
      >
      <span class="log-chart" aria-hidden="true">
        {#each pulse.bars as count, index (index)}
          <span
            class="log-bar"
            data-severity={pulse.severity[index]}
            style:height="{count ? 4 + Math.sqrt(count / peak) * 18 : 2}px"
          ></span>
        {/each}
      </span>
      <span class="log-totals"
        ><span class:error={pulse.recentErrors > 0}
          >{pulse.recentErrors} errors</span
        ><span class:warning={pulse.recentWarnings > 0}
          >{pulse.recentWarnings} warnings</span
        ><span class="sample-window">5m sample</span></span
      >
      <span class="widget-note"
        >{data.groups} groups · {pulse.lastEventAt
          ? `last event ${timeAgo(new Date(pulse.lastEventAt).toISOString(), pulse.now)}`
          : "no events sampled"}</span
      >
    {:else}
      <span class="widget-note" class:warning={pulse.status === "unavailable"}
        >{pulse.status === "loading"
          ? "Reading recent log activity…"
          : "Cannot refresh log activity"}</span
      >
      <span class="widget-note"
        >{data.groups} log groups{pulse.sampledAt
          ? ` · last sample ${timeAgo(new Date(pulse.sampledAt).toISOString(), pulse.now)}`
          : ""}</span
      >
    {/if}
  {:else}
    {#each data.rows as row (row.label)}
      <span class="widget-row">
        <span>{row.label}</span>
        <span class="metric-value">
          {#if row.blocks?.length}
            <span
              class="square-meter"
              role="img"
              aria-label={row.blocks.map((block) => block.title).join("; ")}
            >
              {#each row.blocks as block (block.id)}
                <span
                  class="square-block"
                  class:dimmed={block.dimmed}
                  style:background={block.color}
                  title={block.title}
                  aria-hidden="true"
                ></span>
              {/each}
            </span>
          {/if}
          <b
            class:warning={row.tone === "warning"}
            class:error={row.tone === "error"}
            title={String(row.value)}>{row.value}</b
          >
        </span>
      </span>
    {/each}
    {#if data.note}<span class="widget-note">{data.note}</span>{/if}
  {/if}
</span>

<style>
  .widget {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
    font-weight: 400;
  }
  .widget-row {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 12px;
    font-size: 10.5px;
    color: var(--text-tertiary);
  }
  .widget-row > span {
    flex-shrink: 0;
  }
  b {
    color: var(--text-primary);
    font: 500 10.5px var(--font-ui-mono, var(--font-mono, monospace));
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .value-divider {
    color: var(--text-tertiary);
    font-weight: 400;
  }
  .compact-widget {
    gap: 4px;
  }
  .metric-value {
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    min-width: 0;
  }
  .metric-value > b {
    flex-shrink: 0;
  }
  .widget-note {
    font-size: 10px;
    line-height: 1.5;
    color: var(--text-tertiary);
  }
  .square-meter {
    display: flex;
    flex: 1;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 3px;
    min-width: 0;
  }
  .square-block {
    position: relative;
    flex: 0 0 8px;
    width: 8px;
    height: 8px;
    border-radius: 1px;
    background: var(--border-default);
  }
  .square-block.dimmed {
    opacity: 0.35;
  }
  .square-block.linked::after {
    content: "";
    position: absolute;
    bottom: 1px;
    left: 1px;
    right: 1px;
    height: 1px;
    background: var(--text-primary);
    opacity: 0.7;
  }
  .stale .square-meter {
    opacity: 0.45;
  }
  .log-chart {
    display: flex;
    align-items: flex-end;
    gap: 3px;
    height: 24px;
    margin: 1px 0 2px;
  }
  .log-bar {
    flex: 1;
    min-width: 0;
    border-radius: 2px 2px 0 0;
    background: color-mix(in srgb, var(--text-secondary) 45%, transparent);
    transition: height 220ms var(--ease-snappy);
  }
  .log-bar[data-severity="1"] {
    background: var(--accent-amber);
  }
  .log-bar[data-severity="2"] {
    background: var(--accent-red);
  }
  .log-totals {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
    color: var(--text-secondary);
    font: 10px var(--font-ui-mono, var(--font-mono, monospace));
  }
  .sample-window {
    margin-left: auto;
    color: var(--text-tertiary);
  }
  .warning {
    color: var(--accent-amber);
  }
  .error {
    color: var(--accent-red);
  }
  @media (prefers-reduced-motion: reduce) {
    .log-bar {
      transition: none;
    }
  }
</style>
