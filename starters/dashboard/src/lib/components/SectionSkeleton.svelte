<div
  class="section-skeleton"
  role="img"
  aria-label="Suggested section layout: a toolbar, summary strip, main content and a secondary detail pane. Static structural placeholders, not a loading state."
>
  <div aria-hidden="true">
    <div class="slot-title"><span>01</span> Toolbar</div>
    <div class="skeleton-toolbar">
      <span class="bar search"></span><span class="bar action"></span><span
        class="bar action"
      ></span>
    </div>
    <div class="slot-title"><span>02</span> Summary</div>
    <div class="skeleton-summary">
      {#each [1, 2, 3] as item (item)}
        <div>
          <span class="bar short"></span><span class="bar value"></span><span
            class="bar medium"
          ></span>
        </div>
      {/each}
    </div>
    <div class="skeleton-body">
      <div class="skeleton-primary">
        <div class="slot-title"><span>03</span> Main content</div>
        <div class="skeleton-table">
          <div class="skeleton-row heading">
            <span class="bar medium"></span><span class="bar short"></span>
          </div>
          {#each [1, 2, 3, 4, 5] as item (item)}
            <div class="skeleton-row">
              <span class="placeholder-icon"></span>
              <div>
                <span class="bar medium"></span><span class="bar short"></span>
              </div>
              <span class="bar cell"></span>
            </div>
          {/each}
        </div>
      </div>
      <div class="skeleton-secondary">
        <div class="slot-title"><span>04</span> Details</div>
        <div class="skeleton-detail">
          <span class="bar medium"></span><span class="bar short"></span>
          <div class="placeholder-block"></div>
          <span class="bar"></span><span class="bar medium"></span><span
            class="bar short"
          ></span>
        </div>
      </div>
    </div>
  </div>
</div>

<style>
  .section-skeleton {
    min-width: 0;
  }
  .slot-title {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 20px 0 10px;
    color: var(--ink-secondary);
    font-size: 11.5px;
  }
  .slot-title span {
    color: var(--ink-tertiary);
    font: 10px var(--font-data);
  }
  .skeleton-toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    padding: 12px;
  }
  .bar {
    display: block;
    width: 100%;
    height: 6px;
    border-radius: 3px;
    background: var(--line-strong);
  }
  .bar.search {
    width: 38%;
    height: 28px;
    background: var(--hover-fill);
    border: 1px solid var(--line);
  }
  .bar.action {
    width: 58px;
    height: 24px;
  }
  .bar.search + .bar.action {
    margin-left: auto;
  }
  .bar.short {
    width: 36%;
  }
  .bar.medium {
    width: 66%;
  }
  .bar.value {
    width: 44%;
    height: 18px;
  }
  .skeleton-summary {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    border-block: 1px solid var(--line);
  }
  .skeleton-summary > div {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
  }
  .skeleton-summary > div + div {
    border-left: 1px solid var(--line);
  }
  .skeleton-body {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(160px, 1fr);
    gap: 24px;
  }
  .skeleton-primary,
  .skeleton-secondary {
    min-width: 0;
  }
  .skeleton-table {
    border-block: 1px solid var(--line);
  }
  .skeleton-row {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 52px;
    padding: 12px 8px;
    border-bottom: 1px solid var(--line);
  }
  .skeleton-row:last-child {
    border: 0;
  }
  .skeleton-row.heading {
    min-height: 34px;
    background: var(--surface-raised);
    justify-content: space-between;
  }
  .skeleton-row > div {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .skeleton-row .bar.cell {
    width: 48px;
  }
  .placeholder-icon {
    width: 22px;
    height: 22px;
    border: 1px solid var(--line-strong);
    border-radius: 6px;
  }
  .skeleton-detail {
    display: flex;
    flex-direction: column;
    gap: 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius-panel);
    padding: 16px 18px;
  }
  .placeholder-block {
    height: 108px;
    border: 1px dashed var(--line-strong);
    border-radius: var(--radius-control);
    background: var(--hover-fill);
    margin: 4px 0;
  }
  @media (max-width: 1100px) {
    .skeleton-body {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (max-width: 640px) {
    .skeleton-summary > div {
      padding: 12px 8px;
    }
  }
</style>
