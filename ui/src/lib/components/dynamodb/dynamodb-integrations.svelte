<script lang="ts">
  import {
    ArrowRightIcon,
    ArrowsClockwiseIcon,
    CubeIcon,
    FlowArrowIcon,
    GraphIcon,
    LightningIcon,
    LockKeyIcon,
    ScrollIcon,
  } from "phosphor-svelte";
  import RcButton from "#lib/components/rack/rc-button.svelte";
  import RcPanel from "#lib/components/rack/rc-panel.svelte";
  import RcTonePill from "#lib/components/rack/rc-tone-pill.svelte";
  import { dynamoStateTone } from "#lib/dynamodb";
  import { ecsKey } from "#lib/ecs";
  import type { DynamoTableDetailResponse } from "#lib/types";

  let {
    detail,
    onNavigate = (_tab: string) => {},
  }: {
    detail: DynamoTableDetailResponse;
    onNavigate?: (tab: string) => void;
  } = $props();

  const integrations = $derived(detail.integrations);
  const triggers = $derived(integrations?.triggers ?? []);
  const lambdas = $derived(integrations?.lambdas ?? []);
  const ecs = $derived(integrations?.ecs ?? []);
  const stepFunctions = $derived(integrations?.stepFunctions ?? []);
  const kmsKey = $derived(integrations?.kmsKey);
  const kinesis = $derived(detail.kinesisDestinations ?? []);

  const totalCount = $derived(
    triggers.length +
      lambdas.length +
      ecs.length +
      stepFunctions.length +
      (kmsKey ? 1 : 0) +
      kinesis.length,
  );
</script>

<div class="integrations-tab">
  <RcPanel
    title="Deployed Infrastructure Integrations"
    description="Local resources in this Tarn account referencing {detail.table.name} by table name, ARN, stream ARN, or ASL task."
    index={0}
  >
    {#snippet actions()}
      <div class="panel-actions">
        <RcTonePill tone={totalCount > 0 ? "green" : "neutral"}>
          {totalCount} linked resource{totalCount === 1 ? "" : "s"}
        </RcTonePill>
        <RcButton small variant="ghost" onclick={() => onNavigate("home?section=topology")}>
          <GraphIcon size={12} /> Topology
        </RcButton>
      </div>
    {/snippet}

    {#if totalCount === 0}
      <p class="empty">
        No Lambda functions, stream triggers, ECS tasks/services, Step Functions, or KMS keys are currently linked to this table.
      </p>
    {:else}
      <div class="groups">
        {#if triggers.length > 0}
          <section class="group">
            <h3 class="group-title">
              <ArrowsClockwiseIcon size={13} /> Stream Event Source Triggers ({triggers.length})
            </h3>
            <div class="item-list">
              {#each triggers as tr (tr.uuid)}
                <div class="item-row">
                  <div class="item-info">
                    <button
                      type="button"
                      class="link-btn mono"
                      onclick={() => onNavigate(`functions?fn=${encodeURIComponent(tr.functionName)}`)}
                    >
                      {tr.functionName} <ArrowRightIcon size={11} />
                    </button>
                    <RcTonePill tone={dynamoStateTone(tr.state)}>{tr.state.toLowerCase()}</RcTonePill>
                    <span class="chip">batch {tr.batchSize}</span>
                    {#if tr.lastResult}<span class="chip">{tr.lastResult}</span>{/if}
                  </div>
                  <div class="item-actions">
                    <RcButton
                      small
                      variant="ghost"
                      onclick={() => onNavigate(`triggers?id=${encodeURIComponent(`dynamodb-${tr.uuid}`)}`)}
                    >
                      Trigger
                    </RcButton>
                    {#if tr.logGroup}
                      <RcButton
                        small
                        variant="ghost"
                        onclick={() => onNavigate(`logs?groups=${encodeURIComponent(tr.logGroup!)}`)}
                      >
                        <ScrollIcon size={12} /> Logs
                      </RcButton>
                    {/if}
                  </div>
                </div>
              {/each}
            </div>
          </section>
        {/if}

        {#if lambdas.length > 0}
          <section class="group">
            <h3 class="group-title">
              <LightningIcon size={13} /> Lambda Functions (Environment References) ({lambdas.length})
            </h3>
            <div class="item-list">
              {#each lambdas as fn (`${fn.functionName}:${fn.envKey}`)}
                <div class="item-row">
                  <div class="item-info">
                    <button
                      type="button"
                      class="link-btn mono"
                      onclick={() => onNavigate(`functions?fn=${encodeURIComponent(fn.functionName)}`)}
                    >
                      {fn.functionName} <ArrowRightIcon size={11} />
                    </button>
                    <RcTonePill tone={dynamoStateTone(fn.state)}>{fn.state.toLowerCase()}</RcTonePill>
                    <span class="chip">{fn.runtime}</span>
                    <span class="chip">env: {fn.envKey} ({fn.matchType})</span>
                  </div>
                  <div class="item-actions">
                    {#if fn.logGroup}
                      <RcButton
                        small
                        variant="ghost"
                        onclick={() => onNavigate(`logs?groups=${encodeURIComponent(fn.logGroup!)}`)}
                      >
                        <ScrollIcon size={12} /> Logs
                      </RcButton>
                    {/if}
                  </div>
                </div>
              {/each}
            </div>
          </section>
        {/if}

        {#if ecs.length > 0}
          <section class="group">
            <h3 class="group-title">
              <CubeIcon size={13} /> ECS Services &amp; Task Definitions ({ecs.length})
            </h3>
            <div class="item-list">
              {#each ecs as svc (`${svc.kind}:${svc.arn}:${svc.envKey}`)}
                <div class="item-row">
                  <div class="item-info">
                    <button
                      type="button"
                      class="link-btn mono"
                      onclick={() =>
                        onNavigate(
                          svc.kind === "service"
                            ? `ecs?sel=${encodeURIComponent(ecsKey("service", svc.arn))}`
                            : "ecs",
                        )}
                    >
                      {svc.name} <ArrowRightIcon size={11} />
                    </button>
                    <RcTonePill tone={dynamoStateTone(svc.status)}>{svc.status.toLowerCase()}</RcTonePill>
                    <span class="chip">{svc.kind}</span>
                    {#if svc.clusterName}<span class="chip">cluster: {svc.clusterName}</span>{/if}
                    <span class="chip">{svc.containerName} · {svc.envKey}</span>
                  </div>
                  <div class="item-actions">
                    {#if svc.logGroup}
                      <RcButton
                        small
                        variant="ghost"
                        onclick={() => onNavigate(`logs?groups=${encodeURIComponent(svc.logGroup!)}`)}
                      >
                        <ScrollIcon size={12} /> Logs
                      </RcButton>
                    {/if}
                  </div>
                </div>
              {/each}
            </div>
          </section>
        {/if}

        {#if stepFunctions.length > 0}
          <section class="group">
            <h3 class="group-title">
              <FlowArrowIcon size={13} /> Step Functions State Machines ({stepFunctions.length})
            </h3>
            <div class="item-list">
              {#each stepFunctions as sm (sm.arn)}
                <div class="item-row">
                  <div class="item-info">
                    <button
                      type="button"
                      class="link-btn mono"
                      onclick={() => onNavigate(`stepfunctions?machine=${encodeURIComponent(sm.arn)}`)}
                    >
                      {sm.name} <ArrowRightIcon size={11} />
                    </button>
                    <RcTonePill tone={dynamoStateTone(sm.status)}>{sm.status.toLowerCase()}</RcTonePill>
                    <span class="chip">{sm.type}</span>
                    <span class="chip">{sm.matchType}</span>
                    {#if sm.stateNames.length > 0}
                      <span class="chip">states: {sm.stateNames.join(", ")}</span>
                    {/if}
                  </div>
                </div>
              {/each}
            </div>
          </section>
        {/if}

        {#if kmsKey}
          <section class="group">
            <h3 class="group-title">
              <LockKeyIcon size={13} /> KMS Encryption Key
            </h3>
            <div class="item-list">
              <div class="item-row">
                <div class="item-info">
                  <button
                    type="button"
                    class="link-btn mono"
                    onclick={() => onNavigate(`kms?key=${encodeURIComponent(kmsKey.keyId)}`)}
                  >
                    {kmsKey.aliases?.[0] || kmsKey.keyId} <ArrowRightIcon size={11} />
                  </button>
                  <RcTonePill tone={dynamoStateTone(kmsKey.state)}>{kmsKey.state.toLowerCase()}</RcTonePill>
                  {#if kmsKey.description}
                    <span class="chip">{kmsKey.description}</span>
                  {/if}
                </div>
              </div>
            </div>
          </section>
        {/if}

        {#if kinesis.length > 0}
          <section class="group">
            <h3 class="group-title">Kinesis Streaming Destinations ({kinesis.length})</h3>
            <div class="item-list">
              {#each kinesis as dest (dest.streamArn)}
                <div class="item-row">
                  <div class="item-info">
                    <span class="mono-text">{dest.streamArn}</span>
                    <RcTonePill tone={dynamoStateTone(dest.destinationStatus)}>
                      {dest.destinationStatus.toLowerCase()}
                    </RcTonePill>
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
  .integrations-tab {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }
  .panel-actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .empty {
    font-size: 12px;
    color: var(--text-tertiary);
    padding: 8px 0;
  }
  .groups {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .group-title {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 10.5px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }
  .item-list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    overflow: hidden;
  }
  .item-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    flex-wrap: wrap;
    padding: 9px 12px;
    border-top: 1px solid var(--border-subtle);
    background: var(--bg-app);
  }
  .item-row:first-child {
    border-top: 0;
  }
  .item-info {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    min-width: 0;
  }
  .item-actions {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .link-btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 12.5px;
    font-weight: 500;
    color: var(--text-primary);
  }
  .link-btn.mono,
  .mono-text {
    font-family: var(--font-mono, ui-monospace, monospace);
  }
  .mono-text {
    font-size: 11.5px;
    color: var(--text-primary);
    word-break: break-all;
  }
  .link-btn:hover {
    text-decoration: underline;
  }
  .chip {
    padding: 1px 6px;
    border-radius: 4px;
    background: var(--bg-element);
    font: 10.5px var(--font-mono, ui-monospace, monospace);
    color: var(--text-secondary);
  }
</style>
