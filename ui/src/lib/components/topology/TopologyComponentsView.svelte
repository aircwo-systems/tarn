<script lang="ts">
  import { getDashboard } from "$lib/state.svelte";
  import { fade } from "svelte/transition";

  let {
    gateways = [],
    functions = [],
    queues = [],
    topics = [],
    secrets = [],
    buckets = [],
    infra = [],
    canvasExpanded = false,
    // onGatewayClick = (_id: string) => {},
    onNavigate = (_tab: string) => {},
  } = $props();

  const dashboard = getDashboard();

  // --- 1. Tooltip State ---
  let tooltip = $state({
    visible: false,
    x: 0,
    y: 0,
    title: "",
    detail: "",
    status: "",
    color: "var(--color-primary)",
  });

  function showTooltip(e: MouseEvent, item: any, type: string, color: string) {
    tooltip = {
      visible: true,
      x: e.clientX,
      y: e.clientY,
      title: item.name || item.apiId || "Unknown Resource",
      detail: type,
      status: item.state || item.status || "Active",
      color,
    };
  }

  function hideTooltip() {
    tooltip.visible = false;
  }

  // --- 2. Adaptive Layout Logic ---
  const W = 1400;
  const H = $derived(canvasExpanded ? 900 : 640);
  const CX = W / 2;

  const zones = $derived([
    {
      id: "gateways",
      label: "INGRESS",
      items: gateways,
      color: "var(--color-destructive)",
      side: -1,
      order: 0,
    },
    {
      id: "functions",
      label: "COMPUTE",
      items: functions,
      color: "var(--color-primary)",
      side: -1,
      order: 1,
    },
    {
      id: "queues",
      label: "MESSAGING",
      items: queues,
      color: "var(--color-chart-4)",
      side: 1,
      order: 0,
    },
    {
      id: "sns",
      label: "SNS",
      items: topics,
      color: "var(--color-primary)",
      side: 1,
      order: 1,
    },
    {
      id: "secrets",
      label: "SECRETS",
      items: secrets,
      color: "var(--color-chart-2)",
      side: 1,
      order: 2,
    },
    {
      id: "storage",
      label: "OBJECTS",
      items: buckets,
      color: "var(--color-cyan)",
      side: 1,
      order: 3,
    },
  ]);

  const getScale = (count: number) => {
    const dense = count > 6;
    return {
      w: dense ? 90 : 140,
      h: dense ? 30 : 45,
      gap: dense ? 6 : 12,
      cols: dense ? 3 : 2,
      font: dense ? "9px" : "11px",
    };
  };
</script>

<div
  class="relative w-full overflow-hidden transition-all duration-700 ease-in-out bg-background border border-border"
  style="height: {H}px;"
>
  {#if tooltip.visible}
    <div
      transition:fade={{ duration: 100 }}
      class="fixed pointer-events-none z-50 flex flex-col gap-1 px-3 py-2 rounded border shadow-xl backdrop-blur-md bg-muted/90 border-primary/50"
      style="left: {tooltip.x + 15}px; top: {tooltip.y +
        15}px; min-width: 160px;"
    >
      <div class="flex items-center justify-between gap-4">
        <span class="text-[10px] font-mono opacity-50 tracking-tighter"
          >{tooltip.detail}</span
        >
        <div
          class="h-1.5 w-1.5 rounded-full"
          style="background: {tooltip.color}"
        ></div>
      </div>
      <div class="text-xs font-bold font-mono truncate text-foreground">
        {tooltip.title}
      </div>
      <div
        class="text-[10px] font-mono py-0.5 px-1.5 rounded w-fit uppercase"
        style="background: {tooltip.color}22; color: {tooltip.color}"
      >
        {tooltip.status}
      </div>
    </div>
  {/if}

  <svg viewBox="0 0 {W} {H}" class="w-full h-full">
    <defs>
      <pattern
        id="dotGrid"
        width="40"
        height="40"
        patternUnits="userSpaceOnUse"
      >
        <circle
          cx="2"
          cy="2"
          r="1"
          fill="var(--color-foreground)"
          opacity="0.05"
        />
      </pattern>
    </defs>

    <rect width="100%" height="100%" fill="url(#dotGrid)" />

    <g transform="translate({CX - 100}, 40)">
      <rect
        width="200"
        height="54"
        rx="2"
        fill="var(--color-bg-surface)"
        stroke="var(--color-foreground)"
        stroke-width="0.5"
      />
      <text
        x="100"
        y="34"
        text-anchor="middle"
        font-family="var(--font-mono)"
        font-size="14"
        font-weight="900"
        letter-spacing="4"
        fill="var(--color-foreground)"
      >
        OPEN STACK
      </text>
      <path
        d="M 0 54 L 200 54"
        stroke="var(--color-primary)"
        stroke-width="2"
      />
    </g>

    {#if !dashboard.loading}
      {#each zones as zone}
        {#if zone.items.length > 0}
          {@const s = getScale(zone.items.length)}
          {@const xBase = zone.side === -1 ? CX - 460 : CX + 100}
          {@const yBase = 160 + zone.order * (canvasExpanded ? 240 : 180)}

          <g transform="translate({xBase}, {yBase})">
            <path
              d="M {zone.side === -1 ? 320 : -60} 0 L {zone.side === -1
                ? 360
                : -100} 0"
              stroke={zone.color}
              stroke-width="1"
              opacity="0.2"
            />

            <text
              x="0"
              y="-15"
              fill={zone.color}
              font-family="var(--font-mono)"
              font-size="10"
              font-weight="bold"
              opacity="0.8"
            >
              // {zone.label}
            </text>

            {#each zone.items.slice(0, 18) as item, i}
              {@const col = i % s.cols}
              {@const row = Math.floor(i / s.cols)}
              <g
                transform="translate({col * (s.w + s.gap)}, {row *
                  (s.h + s.gap)})"
                role="button"
                tabindex="0"
                class="cursor-pointer group"
                onmouseenter={(e) => showTooltip(e, item, zone.id, zone.color)}
                onmouseleave={hideTooltip}
                // temp disable until we have an idea on gateway data display
                // onclick={() => zone.id === 'gateways' ? onGatewayClick(item.apiId) : onNavigate(zone.id)}
                onclick={() => onNavigate(zone.id)}
                onkeydown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onNavigate(zone.id);
                  }
                }}
              >
                <rect
                  width={s.w}
                  height={s.h}
                  rx="1"
                  fill="var(--color-bg-overlay)"
                  stroke="var(--color-border)"
                  class="group-hover:stroke-text group-hover:fill-muted transition-all duration-200"
                />
                <rect width="3" height={s.h} fill={zone.color} opacity="0.5" />
                <text
                  x="10"
                  y={s.h / 2 + 4}
                  fill="var(--color-text-muted)"
                  font-family="var(--font-mono)"
                  font-size={s.font}
                  class="group-hover:fill-foreground truncate"
                >
                  {item.name?.slice(0, 12).toUpperCase() || "UNTITLED"}
                </text>
              </g>
            {/each}
          </g>
        {/if}
      {/each}

      <g transform="translate({W - 260}, 40)">
        <rect
          width="220"
          height={H - 80}
          rx="4"
          fill="var(--color-bg-surface)"
          fill-opacity="0.3"
          stroke="var(--color-border)"
          stroke-dasharray="4 4"
        />
        <text
          x="15"
          y="25"
          fill="var(--color-muted-foreground)"
          font-family="var(--font-mono)"
          font-size="9"
          font-weight="bold">SYSTEM_PROBES</text
        >

        {#each infra.slice(0, 14) as probe, i}
          <g
            role="button"
            tabindex="0"
            transform="translate(15, {50 + i * 40})"
            class="cursor-pointer"
            onmouseenter={(e) =>
              showTooltip(e, probe, "LOCAL_PROBE", "var(--color-primary)")}
            onmouseleave={hideTooltip}
          >
            <circle
              cx="5"
              cy="12"
              r="3"
              fill={probe.status === "connected"
                ? "var(--color-primary)"
                : "var(--color-destructive)"}
            />
            <text
              x="18"
              y="16"
              fill="var(--color-foreground)"
              font-family="var(--font-mono)"
              font-size="11"
              opacity="0.8">{probe.name}</text
            >
            <line
              x1="0"
              y1="30"
              x2="190"
              y2="30"
              stroke="var(--color-border)"
              opacity="0.2"
            />
          </g>
        {/each}
      </g>
    {/if}
  </svg>
</div>

<style>
  svg {
    shape-rendering: geometricPrecision;
  }
  g,
  rect,
  path {
    transition:
      transform 0.6s cubic-bezier(0.2, 0.8, 0.2, 1),
      height 0.6s ease;
  }
</style>
