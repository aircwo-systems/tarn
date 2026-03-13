<script lang="ts">
  import { Tooltip } from "$lib/components/ui/simple-tooltip";

  let {
    name,
    arn,
  }: {
    name: string;
    arn: string;
  } = $props();
</script>

<div class="flex flex-col gap-0.5 max-w-[20rem]">
  <span class="font-medium text-foreground">{name}</span>
  <Tooltip text={arn}>
    <span
      class="font-mono text-[11px] text-muted-foreground/70 truncate block cursor-default"
      >{arn}</span
    >
  </Tooltip>
</div>
