<script lang="ts">
  import { canNavigate } from './lib/pane';
  import type { PaneEntry, PaneState, SortMode } from './lib/models';

  export let pane: PaneState;
  export let onParent: () => void;
  export let onRefresh: () => void;
  export let onSort: (mode: SortMode) => void;
  export let onToggle: (entry: PaneEntry) => void;
  export let onOpen: (entry: PaneEntry) => void;

  function entryIcon(entry: PaneEntry) {
    if (entry.kind === 'directory') return 'DIR';
    if (entry.kind === 'archive') return 'ARC';
    if (entry.kind === 'image') return 'IMG';
    if (entry.kind === 'partition') return 'PRT';
    if (entry.kind === 'container') return 'BOX';
    if (entry.kind === 'link') return 'LNK';
    return 'FIL';
  }

  function formatSize(size: number | null) {
    if (size === null) return '';
    if (size < 1024) return `${size}`;
    if (size < 1024 * 1024) return `${Math.round(size / 1024)}K`;
    if (size < 1024 * 1024 * 1024) return `${Math.round(size / 1024 / 1024)}M`;
    return `${(size / 1024 / 1024 / 1024).toFixed(1)}G`;
  }
</script>

<div class="path-strip">
  <button on:click|stopPropagation={onParent}>Parent</button>
  <div class="path">{pane.path}</div>
  <button on:click|stopPropagation={onRefresh}>Read</button>
</div>
<div class="status-strip">
  <span>{pane.entries.length} entries</span>
  <span>{pane.selected.length} selected</span>
  <span>{pane.loading ? 'BUSY' : pane.error ? 'ERROR' : 'READY'}</span>
</div>
<div class="headers">
  <button on:click|stopPropagation={() => onSort('name')}>Name</button>
  <button on:click|stopPropagation={() => onSort('kind')}>Type</button>
  <button on:click|stopPropagation={() => onSort('size')}>Size</button>
  <button on:click|stopPropagation={() => onSort('date')}>Date</button>
</div>
{#if pane.error}
  <div class="error">{pane.error}</div>
{/if}
<div class="entry-list" role="listbox" aria-label={`${pane.id} file list`}>
  {#each pane.entries as entry (entry.path)}
    <button
      class:selected={pane.selected.includes(entry.path)}
      class:navigable={canNavigate(entry)}
      role="option"
      aria-selected={pane.selected.includes(entry.path)}
      on:click|stopPropagation={() => onToggle(entry)}
      on:dblclick|stopPropagation={() => onOpen(entry)}
    >
      <span class="tag">{entryIcon(entry)}</span>
      <span class="entry-name">
        {entry.name}
        {#if entry.linkTarget}
          <span class="entry-link">-&gt; {entry.linkTarget}</span>
        {/if}
      </span>
      <span class="entry-kind">{entry.kind}</span>
      <span class="entry-size">{formatSize(entry.size)}</span>
      <span class="entry-date">{entry.date ?? ''}</span>
    </button>
  {/each}
</div>
