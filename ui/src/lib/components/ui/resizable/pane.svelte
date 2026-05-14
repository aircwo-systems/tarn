<script lang="ts">
  import { Pane } from "paneforge";
  import type { ComponentProps } from "svelte";

  let { ...restProps }: ComponentProps<typeof Pane> = $props();
</script>

<Pane {...restProps} />
