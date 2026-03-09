<script lang="ts">
  export let W: number; export let H: number; export let gap = 40;
</script>

{#each Array(Math.floor(W / gap)) as _, ix}
  {#each Array(Math.floor(H / gap)) as _, iy}
    <circle cx={gap/2 + ix * gap} cy={gap/2 + iy * gap} r="0.6" class="fill-border" opacity="0.5" />
  {/each}
{/each}