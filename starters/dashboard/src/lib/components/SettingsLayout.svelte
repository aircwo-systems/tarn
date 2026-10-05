<script lang="ts" module>
  export interface SettingsSection {
    id: string;
    label: string;
  }
</script>

<script lang="ts">
  import { onMount, type Snippet } from "svelte";
  let {
    sections,
    section,
  }: {
    sections: readonly SettingsSection[];
    section: Snippet<[SettingsSection]>;
  } = $props();
  let root = $state<HTMLElement | null>(null);
  let activeId = $state("");
  let scrollRoot: HTMLElement | null = null;
  let jumping: ReturnType<typeof setTimeout> | undefined;
  const activeIndex = $derived(
    Math.max(
      0,
      sections.findIndex((item) => item.id === activeId),
    ),
  );

  function jump(id: string) {
    const target = root?.querySelector<HTMLElement>(`#settings-${id}`);
    if (!target) return;
    activeId = id;
    clearTimeout(jumping);
    const reduced = window.matchMedia(
      "(prefers-reduced-motion: reduce)",
    ).matches;
    jumping = setTimeout(
      () => {
        jumping = undefined;
        spy();
      },
      reduced ? 50 : 700,
    );
    target.scrollIntoView({
      behavior: reduced ? "auto" : "smooth",
      block: "start",
    });
  }
  function spy() {
    if (!scrollRoot || jumping || !sections.length) return;
    const { top, height } = scrollRoot.getBoundingClientRect();
    if (
      scrollRoot.scrollTop > 0 &&
      scrollRoot.scrollTop + scrollRoot.clientHeight >=
        scrollRoot.scrollHeight - 4
    ) {
      activeId = sections[sections.length - 1].id;
      return;
    }
    let current = sections[0].id;
    for (const item of sections) {
      const el = root?.querySelector<HTMLElement>(`#settings-${item.id}`);
      if (el && el.getBoundingClientRect().top <= top + height * 0.3)
        current = item.id;
    }
    activeId = current;
  }
  onMount(() => {
    scrollRoot = root?.closest("main") ?? null;
    scrollRoot?.addEventListener("scroll", spy, { passive: true });
    window.addEventListener("resize", spy);
    spy();
    return () => {
      scrollRoot?.removeEventListener("scroll", spy);
      window.removeEventListener("resize", spy);
      clearTimeout(jumping);
    };
  });
</script>

<div class="settings-grid" bind:this={root}>
  <nav class="settings-index" aria-label="Settings sections">
    <span
      class="index-pill"
      style:transform="translateY({activeIndex * 30}px)"
      aria-hidden="true"
    ></span>
    {#each sections as item (item.id)}
      <button
        class="index-item"
        class:active={sections[activeIndex]?.id === item.id}
        aria-current={sections[activeIndex]?.id === item.id
          ? "location"
          : undefined}
        aria-controls="settings-{item.id}"
        onclick={() => jump(item.id)}>{item.label}</button
      >
    {/each}
  </nav>
  <div class="settings-panels">
    {#each sections as item (item.id)}
      <section
        class="settings-panel"
        id="settings-{item.id}"
        aria-label={item.label}
      >
        {@render section(item)}
      </section>
    {/each}
  </div>
</div>

<style>
  .settings-grid {
    display: grid;
    grid-template-columns: 180px minmax(0, 720px);
    gap: 32px;
    padding: 20px 0 96px;
  }
  .settings-index {
    position: sticky;
    top: 0;
    align-self: start;
    display: flex;
    flex-direction: column;
  }
  .index-pill {
    position: absolute;
    inset: 0 0 auto;
    height: 28px;
    border-radius: 5px;
    background: var(--nav-active);
    border: 1px solid var(--line);
    pointer-events: none;
    transition: transform 260ms var(--ease-out);
  }
  .index-pill::before {
    content: "";
    position: absolute;
    left: 6px;
    top: 8px;
    bottom: 8px;
    width: 2.5px;
    border-radius: 2px;
    background: var(--ink);
    opacity: 0.9;
  }
  .index-item {
    position: relative;
    height: 28px;
    margin-bottom: 2px;
    padding: 0 10px;
    border: 0;
    background: transparent;
    border-radius: 7px;
    color: var(--ink-tertiary);
    font-size: 12px;
    text-align: left;
    transition:
      color 120ms,
      padding-left 220ms;
  }
  .index-item:hover {
    color: var(--ink);
  }
  .index-item.active {
    color: var(--ink);
    padding-left: 18px;
  }
  .settings-panels {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .settings-panel {
    scroll-margin-top: 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius-panel);
    padding: 16px 18px;
    background: var(--surface);
  }
  @media (max-width: 1000px) {
    .settings-grid {
      grid-template-columns: minmax(0, 1fr);
      gap: 16px;
    }
    .settings-index {
      z-index: 5;
      flex-direction: row;
      overflow-x: auto;
      padding: 6px 0;
      background: var(--surface);
      border-bottom: 1px solid var(--line);
    }
    .index-pill {
      display: none;
    }
    .index-item {
      flex-shrink: 0;
      margin: 0;
    }
    .index-item.active {
      padding-left: 10px;
      background: var(--nav-active);
    }
    .settings-panel {
      scroll-margin-top: 58px;
    }
  }
  @media (max-width: 640px) {
    .index-item {
      height: 44px;
    }
    .settings-panel {
      scroll-margin-top: 72px;
      padding: 16px;
    }
  }
</style>
