<script lang="ts">
  import {
    ArrowRightIcon,
    CubeIcon,
    DetectiveIcon,
    GraphIcon,
    KeyIcon,
    LightningIcon,
    PlugsConnectedIcon,
    ScrollIcon,
  } from "phosphor-svelte";
  import RcButton from "#lib/components/rack/rc-button.svelte";
  import RcPanel from "#lib/components/rack/rc-panel.svelte";
  import RcTonePill from "#lib/components/rack/rc-tone-pill.svelte";
  import { dynamoStateTone } from "#lib/dynamodb";
  import { ecsKey } from "#lib/ecs";
  import { timeAgo } from "#lib/utils";
  import type { RedisTargetDetailResponse } from "#lib/types";

  let {
    detail,
    onNavigate = (_tab: string) => {},
  }: {
    detail: RedisTargetDetailResponse;
    onNavigate?: (tab: string) => void;
  } = $props();

  const integrations = $derived(detail.integrations);
  const lambdas = $derived(integrations?.lambdas ?? []);
  const ecs = $derived(integrations?.ecs ?? []);
  const secrets = $derived(integrations?.secrets ?? []);
  const traces = $derived(integrations?.traces ?? []);
  const totalCount = $derived(lambdas.length + ecs.length + secrets.length + traces.length);
</script>

<div class="integrations-tab">
  <RcPanel
    title="Tarn Links, Logs & Traces"
    description="Resources in the active account referencing {detail.target.host}:{detail.target.port} via env vars, secrets, or observed traces."
    index={0}
  >
    {#snippet actions()}
      <div class="panel-actions">
        <RcTonePill tone={totalCount > 0 ? "green" : "neutral"}>
          {totalCount} link{totalCount === 1 ? "" : "s"}
        </RcTonePill>
        <RcButton small variant="ghost" onclick={() => onNavigate("home?section=topology")}>
          <GraphIcon size={12} /> Topology
        </RcButton>
        <RcButton small variant="ghost" onclick={() => onNavigate(`services?id=${encodeURIComponent(detail.target.id)}`)}>
          <PlugsConnectedIcon size={12} /> Service probe
        </RcButton>
        <RcButton small variant="ghost" onclick={() => onNavigate("logs?pattern=redis")}>
          <ScrollIcon size={12} /> Search logs
        </RcButton>
      </div>
    {/snippet}

    {#if totalCount === 0}
      <p class="empty">
        No Lambda functions, ECS services, secrets, or traces currently link to <code>{detail.target.host}:{detail.target.port}</code> in this account.
        Set <code>REDIS_URL</code> or <code>REDIS_HOST</code> on a function or secret and it will appear here and on the canvas.
      </p>
    {:else}
      <div class="groups">
        {#if lambdas.length > 0}
          <section class="group">
            <h3 class="group-title"><LightningIcon size={13} /> Lambda Functions ({lambdas.length})</h3>
            <div class="item-list">
              {#each lambdas as fn (`${fn.functionName}:${fn.source}`)}
                <div class="item-row">
                  <div class="item-info">
                    <button type="button" class="link-btn mono" onclick={() => onNavigate(`functions?function=${encodeURIComponent(fn.functionName)}`)}>
                      {fn.functionName} <ArrowRightIcon size={11} />
                    </button>
                    <RcTonePill tone={dynamoStateTone(fn.state)}>{fn.state.toLowerCase()}</RcTonePill>
                    <span class="chip">{fn.runtime}</span>
                    <span class="chip">{fn.evidence}: {fn.source.replace(/^secret:/, "")}</span>
                  </div>
                  {#if fn.logGroup}
                    <RcButton small variant="ghost" onclick={() => onNavigate(`logs?groups=${encodeURIComponent(fn.logGroup!)}`)}>
                      <ScrollIcon size={12} /> Logs
                    </RcButton>
                  {/if}
                </div>
              {/each}
            </div>
          </section>
        {/if}

        {#if ecs.length > 0}
          <section class="group">
            <h3 class="group-title"><CubeIcon size={13} /> ECS Services &amp; Tasks ({ecs.length})</h3>
            <div class="item-list">
              {#each ecs as svc (`${svc.kind}:${svc.arn}:${svc.envKey}`)}
                <div class="item-row">
                  <div class="item-info">
                    <button
                      type="button"
                      class="link-btn mono"
                      onclick={() => onNavigate(svc.kind === "service" ? `ecs?sel=${encodeURIComponent(ecsKey("service", svc.arn))}` : "ecs")}
                    >
                      {svc.name} <ArrowRightIcon size={11} />
                    </button>
                    <RcTonePill tone={dynamoStateTone(svc.status)}>{svc.status.toLowerCase()}</RcTonePill>
                    <span class="chip">{svc.containerName} · {svc.envKey}</span>
                  </div>
                  {#if svc.logGroup}
                    <RcButton small variant="ghost" onclick={() => onNavigate(`logs?groups=${encodeURIComponent(svc.logGroup!)}`)}>
                      <ScrollIcon size={12} /> Logs
                    </RcButton>
                  {/if}
                </div>
              {/each}
            </div>
          </section>
        {/if}

        {#if secrets.length > 0}
          <section class="group">
            <h3 class="group-title"><KeyIcon size={13} /> Secrets ({secrets.length})</h3>
            <div class="item-list">
              {#each secrets as sec (sec.name)}
                <div class="item-row">
                  <div class="item-info">
                    <button type="button" class="link-btn mono" onclick={() => onNavigate(`secrets?secret=${encodeURIComponent(sec.name)}`)}>
                      {sec.name} <ArrowRightIcon size={11} />
                    </button>
                    {#if sec.description}<span class="chip">{sec.description}</span>{/if}
                  </div>
                </div>
              {/each}
            </div>
          </section>
        {/if}

        {#if traces.length > 0}
          <section class="group">
            <h3 class="group-title"><DetectiveIcon size={13} /> Recent Traces ({traces.length})</h3>
            <div class="item-list">
              {#each traces as tr (tr.id)}
                <div class="item-row">
                  <div class="item-info">
                    <button type="button" class="link-btn mono" onclick={() => onNavigate(`xray?trace=${encodeURIComponent(tr.id)}`)}>
                      {tr.method ? `${tr.method} ${tr.path || "/"}` : tr.id} <ArrowRightIcon size={11} />
                    </button>
                    <RcTonePill tone={tr.status >= 500 ? "red" : tr.status >= 400 ? "amber" : "green"}>{tr.status || 200}</RcTonePill>
                    <span class="chip">{tr.durationMs}ms</span>
                    <span class="chip">{tr.reason === "redis-span" ? "redis span" : "linked function"}</span>
                    <span class="chip">{timeAgo(tr.startedAt)}</span>
                  </div>
                </div>
              {/each}
            </div>
          </section>
        {/if}
      </div>
    {/if}
  </RcPanel>
</div>

<style>
  .integrations-tab { display: flex; flex-direction: column; gap: 14px; }
  .panel-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .empty { font-size: 12px; color: var(--text-tertiary); padding: 12px 0; line-height: 1.5; }
  .groups { display: flex; flex-direction: column; gap: 16px; }
  .group { display: flex; flex-direction: column; gap: 8px; }
  .group-title {
    margin: 0; display: flex; align-items: center; gap: 6px;
    font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em;
    color: var(--text-tertiary);
  }
  .item-list { display: flex; flex-direction: column; gap: 6px; }
  .item-row {
    display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap;
    padding: 8px 10px; border-radius: 7px; border: 1px solid var(--border-subtle); background: var(--bg-element);
  }
  .item-info { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-width: 0; }
  .link-btn {
    border: 0; background: transparent; padding: 0; color: var(--text-primary);
    font-size: 12px; font-weight: 500; display: inline-flex; align-items: center; gap: 5px; cursor: pointer;
  }
  .link-btn:hover { color: var(--accent-green); }
  .chip {
    padding: 2px 7px; border-radius: 5px; font-size: 10.5px;
    background: var(--bg-app); border: 1px solid var(--border-subtle); color: var(--text-secondary);
  }
  .mono { font-family: var(--font-mono, ui-monospace, monospace); }
</style>
