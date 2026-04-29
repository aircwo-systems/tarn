<script lang="ts">
  import { Tooltip } from "$lib/components/ui/simple-tooltip";
  import { CopyIcon, CheckIcon } from "phosphor-svelte";

  let {
    name,
    arn,
  }: {
    name: string;
    arn: string;
  } = $props();

  let copiedArn = $state(false);
  let copiedName = $state(false);

  async function copyArn() {
    try {
      await navigator.clipboard.writeText(arn);
      copiedArn = true;
      setTimeout(() => { copiedArn = false; }, 1400);
    } catch {}
  }

  async function copyName() {
    try {
      await navigator.clipboard.writeText(name);
      copiedName = true;
      setTimeout(() => { copiedName = false; }, 1400);
    } catch {}
  }
</script>

<div class="group flex min-w-0 flex-col gap-0.5">
  <div class="flex min-w-0 items-center gap-1">
    <span class="truncate font-medium text-foreground">{name}</span>
    <button
      type="button"
      class="shrink-0 rounded p-0.5 text-muted-foreground/40 opacity-0 transition-all group-hover:opacity-100 hover:text-foreground"
      onclick={() => void copyName()}
      title={copiedName ? "Copied" : "Copy name"}
      aria-label={copiedName ? "Copied name" : "Copy name"}
    >
      {#if copiedName}
        <CheckIcon size={11} />
      {:else}
        <CopyIcon size={11} />
      {/if}
    </button>
  </div>
  <div class="flex min-w-0 items-center gap-1">
    <Tooltip text={arn}>
      <span class="block min-w-0 truncate cursor-default font-mono text-[11px] text-muted-foreground/70">{arn}</span>
    </Tooltip>
    <button
      type="button"
      class="shrink-0 rounded p-0.5 text-muted-foreground/40 opacity-0 transition-all group-hover:opacity-100 hover:text-foreground"
      onclick={() => void copyArn()}
      title={copiedArn ? "Copied" : "Copy ARN"}
      aria-label={copiedArn ? "Copied ARN" : "Copy ARN"}
    >
      {#if copiedArn}
        <CheckIcon size={11} />
      {:else}
        <CopyIcon size={11} />
      {/if}
    </button>
  </div>
</div>
