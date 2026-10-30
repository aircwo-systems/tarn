<script lang="ts">
  import { CaretDownIcon } from "phosphor-svelte";
  import type { LogEvent } from "#lib/types";

  let {
    events,
    viewport = { first: 0, last: 0 },
    selectedTimestamp = null,
    groupForEvent,
    onSelectBucket,
  }: {
    events: LogEvent[];
    viewport?: { first: number; last: number };
    selectedTimestamp?: string | null;
    groupForEvent: (event: LogEvent) => string;
    onSelectBucket: (index: number) => void;
  } = $props();

  const bucketCount = 48;
  let comparing = $state(false);
  let hoveredIndex = $state<number | null>(null);
  let hoveredGroup = $state<string | null>(null);

  const bounds = $derived.by(() => {
    const times = events.map(event => Date.parse(event.timestamp)).filter(Number.isFinite);
    if (!times.length) return null;
    const min = Math.min(...times);
    return { min, max: Math.max(min + 1000, ...times) };
  });
  function bucketIndex(timestamp: string) {
    return bounds ? Math.max(0, Math.min(bucketCount - 1, Math.floor((Date.parse(timestamp) - bounds.min) / (bounds.max - bounds.min) * bucketCount))) : 0;
  }
  function time(timestamp: number | string) {
    return new Date(timestamp).toLocaleTimeString([], { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" });
  }
  function shortName(name: string) {
    if (name === "/tarn/api") return "Tarn API";
    if (name === "/tarn/system") return "Tarn system";
    return name.replace(/^\/(aws\/lambda|ecs|services)\//, "") || "Unknown group";
  }
  const buckets = $derived.by(() => {
    const result = Array.from({ length: bucketCount }, (_, index) => ({ index, rows: Array<number>(), errors: 0, warnings: 0 }));
    events.forEach((event, index) => {
      const bucket = result[bucketIndex(event.timestamp)];
      bucket.rows.push(index);
      if (event.level === "ERROR") bucket.errors++;
      if (event.level === "WARN") bucket.warnings++;
    });
    return result;
  });
  const peak = $derived(Math.max(1, ...buckets.map(bucket => bucket.rows.length)));
  const totals = $derived({ errors: buckets.reduce((sum, bucket) => sum + bucket.errors, 0), warnings: buckets.reduce((sum, bucket) => sum + bucket.warnings, 0) });
  const eventGroups = $derived(events.map(groupForEvent));
  const groups = $derived.by(() => {
    const names = [...new Set(eventGroups)];
    return names.map(name => {
      const groupBuckets = buckets.map(bucket => {
        const rows = bucket.rows.filter(index => eventGroups[index] === name);
        return { index: bucket.index, rows, errors: rows.filter(index => events[index].level === "ERROR").length, warnings: rows.filter(index => events[index].level === "WARN").length };
      });
      return { name, buckets: groupBuckets, count: groupBuckets.reduce((sum, bucket) => sum + bucket.rows.length, 0), errors: groupBuckets.reduce((sum, bucket) => sum + bucket.errors, 0) };
    }).sort((a, b) => b.errors - a.errors || b.count - a.count || a.name.localeCompare(b.name));
  });
  const comparisonGroups = $derived(groups.slice(0, 6));
  const comparisonPeak = $derived(Math.max(1, ...comparisonGroups.flatMap(group => group.buckets.map(bucket => bucket.rows.length))));
  const showComparison = $derived(comparing && groups.length > 1);
  const visible = $derived(new Set(events.slice(Math.max(0, viewport.first), Math.max(0, viewport.last + 1)).map(event => bucketIndex(event.timestamp))));
  const viewWindow = $derived.by(() => {
    const indices = [...visible];
    if (!indices.length) return null;
    return { left: Math.min(...indices) / bucketCount * 100, width: (Math.max(...indices) - Math.min(...indices) + 1) / bucketCount * 100 };
  });
  const selectedBucket = $derived(selectedTimestamp ? bucketIndex(selectedTimestamp) : -1);
  const hovered = $derived(hoveredIndex === null ? null : hoveredGroup ? groups.find(group => group.name === hoveredGroup)?.buckets[hoveredIndex] : buckets[hoveredIndex]);
  const first = $derived(events[viewport.first]);
  const last = $derived(events[viewport.last]);
  function inspect(index: number | null, group: string | null = null) { hoveredIndex = index; hoveredGroup = group; }
  function jump(rows: number[]) { if (rows.length) onSelectBucket(rows[0]); }
  function bucketLabel(index: number, count: number, errors: number, warnings: number, group = "") {
    const timestamp = bounds ? time(bounds.min + index / bucketCount * (bounds.max - bounds.min)) : "";
    return `${group ? shortName(group) + ", " : ""}${timestamp}, ${count} events, ${errors} errors, ${warnings} warnings. Jump to logs`;
  }
</script>

<section class="log-activity" aria-label="Loaded log activity">
  <div class="activity-heading">
    <div><h2>{showComparison ? "Activity across groups" : "Activity"}</h2><p>{events.length.toLocaleString()} loaded events{#if bounds} · {time(bounds.min)} to {time(bounds.max)}{/if}{#if showComparison && groups.length > 6} · showing 6 of {groups.length} groups{/if}</p></div>
    <div class="activity-actions">
      <span class="severity"><i class="error-key"></i>{totals.errors} errors</span><span class="severity"><i class="warning-key"></i>{totals.warnings} warnings</span>
      <button class="compare-toggle" disabled={groups.length < 2} title={groups.length < 2 ? "Select multiple log groups to compare their loaded activity" : undefined} aria-expanded={showComparison} aria-controls="log-activity-tracks" onclick={() => comparing = !comparing}>{showComparison ? "Overview" : "Compare groups"}<CaretDownIcon size={12} class={showComparison ? "rotated" : ""} /></button>
    </div>
  </div>
  {#if bounds}
    <div id="log-activity-tracks" class="track-region">
      {#if showComparison}
        <div class="comparison-tracks">{#each comparisonGroups as group}<div class="lane"><span class="lane-name" title={group.name}>{shortName(group.name)}</span><div class="lane-track">{#each group.buckets as bucket}<button class="lane-slot" class:in-view={visible.has(bucket.index)} class:has-errors={bucket.errors > 0} class:selected-time={selectedBucket === bucket.index} disabled={!bucket.rows.length} aria-label={bucketLabel(bucket.index, bucket.rows.length, bucket.errors, bucket.warnings, group.name)} onpointerenter={() => inspect(bucket.index,group.name)} onpointerleave={() => inspect(null)} onfocus={() => inspect(bucket.index,group.name)} onblur={() => inspect(null)} onclick={() => jump(bucket.rows)}><span class="lane-stem" style:height={`${bucket.rows.length ? Math.max(2,bucket.rows.length / comparisonPeak * 16) : 1}px`}></span>{#if bucket.errors}<span class="lane-error" aria-hidden="true"></span>{/if}</button>{/each}</div><span class="lane-count">{group.count}</span></div>{/each}</div>
      {:else}
        <div class="pulse-chart" role="group" aria-label="Event volume over time">
          {#if viewWindow}<div class="viewport-window" style:left={`${viewWindow.left}%`} style:width={`${viewWindow.width}%`} aria-hidden="true"></div>{/if}
          {#if selectedTimestamp}<div class="event-marker" style:left={`${Math.max(0,Math.min(100,(Date.parse(selectedTimestamp) - bounds.min) / (bounds.max - bounds.min) * 100))}%`} aria-hidden="true"></div>{/if}
          {#each buckets as bucket}<button class="pulse-slot" class:in-view={visible.has(bucket.index)} class:inspecting={hoveredIndex === bucket.index} disabled={!bucket.rows.length} aria-label={bucketLabel(bucket.index,bucket.rows.length,bucket.errors,bucket.warnings)} onpointerenter={() => inspect(bucket.index)} onpointerleave={() => inspect(null)} onfocus={() => inspect(bucket.index)} onblur={() => inspect(null)} onclick={() => jump(bucket.rows)}><span class="pulse-stem" style:height={`${bucket.rows.length ? Math.max(3,bucket.rows.length / peak * 60) : 2}px`}></span><span class="issue-mark" class:error-mark={bucket.errors > 0} class:warning-mark={bucket.errors === 0 && bucket.warnings > 0}></span></button>{/each}
        </div>
      {/if}
    </div>
    <div class="axis" class:comparison-axis={showComparison}><span>{time(bounds.min)}</span><span>{time((bounds.min + bounds.max)/2)}</span><span>{time(bounds.max)}</span></div>
    <div class="activity-caption"><span>{#if hovered && hoveredIndex !== null}{hoveredGroup ? `${shortName(hoveredGroup)} · ` : ""}{time(bounds.min + hoveredIndex / bucketCount * (bounds.max - bounds.min))} · {hovered.rows.length} events · {hovered.errors} errors · {hovered.warnings} warnings{:else}<span class="viewport-key"></span>In view {first && last ? `${time(first.timestamp)} to ${time(last.timestamp)}` : "none"}{/if}</span><span class="activity-hint">{selectedTimestamp ? "Line marks the selected event" : "Click a time to jump"}</span></div>
  {:else}<p class="activity-empty">No activity in the current log selection.</p>{/if}
</section>

<style>
  .log-activity { flex-shrink:0; padding:4px 10px 8px; }
  .activity-heading { display:flex; align-items:start; justify-content:space-between; gap:12px; margin-bottom:8px; }
  h2 { font-size:13px; font-weight:600; letter-spacing:-.01em; color:var(--text-primary); }
  p { font-size:11px; color:var(--text-secondary); margin-top:3px; }
  .activity-actions { display:flex; align-items:center; gap:14px; padding-top:1px; flex-wrap:wrap; }
  .severity { display:flex; align-items:center; gap:5px; color:var(--text-secondary); font-size:11px; }
  .error-key { width:3px; height:6px; border-radius:2px; background:var(--accent-red); }.warning-key { width:4px; height:4px; border-radius:50%; background:var(--accent-amber); }
  button { cursor:pointer; border:0; background:transparent; font:inherit; }.compare-toggle { display:flex; align-items:center; gap:5px; color:var(--text-secondary); font-size:11px; padding:2px 0; }.compare-toggle:hover { color:var(--text-primary); }.compare-toggle:disabled { opacity:.45; cursor:default; }.compare-toggle :global(.rotated) { transform:rotate(180deg); }
  button:focus-visible { outline:2px solid var(--border-focus); outline-offset:2px; }
  .track-region { height:92px; }
  .pulse-chart { position:relative; display:flex; height:100%; isolation:isolate; }
  .pulse-slot { flex:1; position:relative; display:flex; justify-content:center; align-items:end; height:100%; padding:0 0 12px; min-width:0; border-radius:5px; }.pulse-slot:disabled { cursor:default; }
  .pulse-stem { width:calc(100% - 1px); border-radius:2px 2px 0 0; background:color-mix(in srgb,var(--accent-green) 30%,var(--bg-stage)); pointer-events:none; transition:background 120ms ease; }.pulse-slot.in-view .pulse-stem { background:color-mix(in srgb,var(--accent-green) 65%,var(--bg-stage)); }.pulse-slot.inspecting { background:var(--bg-element-hover); }.pulse-slot.inspecting .pulse-stem { background:var(--accent-green); }
  .issue-mark { position:absolute; bottom:3px; height:4px; width:3px; border-radius:2px; pointer-events:none; }.issue-mark.error-mark { height:6px; background:var(--accent-red); }.issue-mark.warning-mark { width:4px; border-radius:50%; background:var(--accent-amber); }
  .viewport-window { position:absolute; top:0; bottom:0; border-radius:5px; background:color-mix(in srgb,var(--bg-element) 45%,transparent); z-index:-1; pointer-events:none; }
  .event-marker { position:absolute; top:3px; bottom:1px; width:1px; background:var(--text-primary); z-index:2; pointer-events:none; }
  .axis { display:flex; justify-content:space-between; margin:5px 0 8px; font-family:var(--font-ui-mono,var(--font-mono)); font-size:10px; color:var(--text-secondary); }.comparison-axis { margin-left:137px; margin-right:42px; }
  .activity-caption { display:flex; justify-content:space-between; gap:12px; min-height:18px; font-size:10.5px; color:var(--text-secondary); }.activity-caption>span:first-child { font-family:var(--font-ui-mono,var(--font-mono)); font-variant-numeric:tabular-nums; }.viewport-key { display:inline-block; width:10px; height:7px; margin-right:6px; border-radius:2px; background:var(--bg-element); }
  .comparison-tracks { height:100%; overflow-y:auto; overscroll-behavior:contain; scrollbar-width:thin; }.lane { display:flex; align-items:center; gap:12px; height:23px; }.lane-name { width:125px; flex-shrink:0; font-size:11px; color:var(--text-secondary); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.lane-track { display:flex; flex:1; min-width:0; height:23px; }.lane-slot { position:relative; flex:1; min-width:0; height:23px; display:flex; align-items:end; justify-content:center; padding:0 0 3px; border-radius:3px; }.lane-slot.in-view { background:var(--bg-element-hover); }.lane-stem { width:calc(100% - 1px); border-radius:2px 2px 0 0; background:color-mix(in srgb,var(--text-secondary) 35%,var(--bg-stage)); }.lane-slot.has-errors .lane-stem { background:color-mix(in srgb,var(--accent-red) 65%,var(--bg-stage)); }.lane-error { position:absolute; bottom:0; width:3px; height:2px; background:var(--accent-red); }.lane-slot.selected-time::after { content:""; position:absolute; left:50%; top:0; bottom:0; width:1px; background:var(--text-primary); pointer-events:none; }.lane-slot:hover { background:var(--bg-element-hover); }.lane-slot:disabled { cursor:default; }.lane-count { width:30px; text-align:right; font-size:10px; color:var(--text-secondary); font-family:var(--font-ui-mono,var(--font-mono)); }
  .activity-empty { padding:12px 0 8px; }
  @media(max-width:800px) { .activity-heading { flex-wrap:wrap; gap:6px; }.activity-actions { gap:10px; }.activity-hint { display:none; }.lane-name { width:90px; }.comparison-axis { margin-left:102px; } }
  @media(prefers-reduced-motion:reduce) { .pulse-stem { transition:none; } }
</style>
