<script lang="ts">
  import { onMount } from 'svelte';
  import { DEFAULT_COMMAND_BAR, commandBarToJson, parseCommandBarConfig } from './lib/commandBar';
  import { browsePath, copyEntries, deleteEntries, extractEntry, inspectPath, makeDirectory, renameEntry } from './lib/engine';
  import PaneView from './PaneView.svelte';
  import {
    canNavigate,
    createPane,
    normalizeListing,
    parentPath,
    setEntries,
    setSortMode,
    toggleSelection
  } from './lib/pane';
  import type { CommandButton, EngineError, PaneEntry, PaneState, SortMode } from './lib/models';

  const fallbackLeft = typeof window !== 'undefined' ? homeishPath() : '.';
  let left: PaneState = createPane('left', fallbackLeft, true);
  let right: PaneState = createPane('right', fallbackLeft, false);
  let commandBar: CommandButton[] = DEFAULT_COMMAND_BAR;
  let commandJson = commandBarToJson(commandBar);
  let showConfig = false;
  let activity = 'Ready.';
  let infoText = '';

  $: activePane = left.active ? left : right;
  $: passivePane = left.active ? right : left;

  onMount(() => {
    void refreshPane('left');
    void refreshPane('right');
  });

  async function refreshPane(id: 'left' | 'right') {
    updatePane(id, { loading: true, error: null });
    const path = id === 'left' ? left.path : right.path;
    try {
      const listing = await browsePath(path);
      replacePane(id, (pane) => setEntries(pane, listing));
      activity = `${id.toUpperCase()} reread ${listing.entries.length} entries.`;
    } catch (error) {
      updatePane(id, { loading: false, error: engineMessage(error) });
      activity = `Engine error: ${engineMessage(error)}`;
    }
  }

  async function navigate(id: 'left' | 'right', entry: PaneEntry) {
    if (!canNavigate(entry)) return;
    updatePane(id, { path: entry.path, selected: [] });
    await refreshPane(id);
  }

  async function goParent(id: 'left' | 'right') {
    updatePane(id, { path: parentPath(id === 'left' ? left.path : right.path), selected: [] });
    await refreshPane(id);
  }

  function activatePane(id: 'left' | 'right') {
    left = { ...left, active: id === 'left' };
    right = { ...right, active: id === 'right' };
  }

  function selectEntry(id: 'left' | 'right', entry: PaneEntry) {
    activatePane(id);
    replacePane(id, (pane) => toggleSelection(pane, entry.path));
  }

  async function runCommand(button: CommandButton) {
    if (!button.enabled) return;
    if (button.action === 'refresh') return refreshPane(activePane.id);
    if (button.action === 'swap') return swapPanes();
    if (button.action === 'config') {
      showConfig = !showConfig;
      return;
    }
    if (button.action === 'info') return showInfo(activePane.path);
    if (button.action === 'mkdir') return createDirectory();
    if (button.action === 'copy') return copySelection();
    if (button.action === 'extract') return extractSelection();
    if (button.action === 'delete') return deleteSelection();
    if (button.action === 'rename') return renameSelection();
  }

  async function copySelection() {
    if (activePane.selected.length === 0) {
      activity = 'Copy needs a selected source entry.';
      return;
    }
    activity = `Copying ${activePane.selected.length} item(s) through hst-imager-go...`;
    try {
      await copyEntries(activePane.selected, passivePane.path);
      await refreshPane(passivePane.id);
      activity = 'Copy completed.';
    } catch (error) {
      activity = `Copy failed: ${engineMessage(error)}`;
    }
  }

  async function extractSelection() {
    const source = activePane.selected[0];
    if (!source) {
      activity = 'Extract needs one selected source entry.';
      return;
    }
    activity = `Extracting ${source} through hst-imager-go...`;
    try {
      await extractEntry(source, passivePane.path);
      await refreshPane(passivePane.id);
      activity = 'Extract completed.';
    } catch (error) {
      activity = `Extract failed: ${engineMessage(error)}`;
    }
  }

  async function createDirectory() {
    const name = window.prompt('Directory name');
    if (!name) return;
    const path = joinPanePath(activePane.path, name);
    try {
      await makeDirectory(path);
      await refreshPane(activePane.id);
      activity = `Created ${name}.`;
    } catch (error) {
      activity = `Mkdir failed: ${engineMessage(error)}`;
    }
  }

  async function deleteSelection() {
    if (activePane.selected.length === 0) {
      activity = 'Delete needs selected entries.';
      return;
    }
    if (!window.confirm(`Delete ${activePane.selected.length} selected item(s)?`)) return;
    try {
      await deleteEntries(activePane.selected);
      await refreshPane(activePane.id);
      activity = 'Delete completed.';
    } catch (error) {
      activity = `Delete failed: ${engineMessage(error)}`;
    }
  }

  async function renameSelection() {
    const source = activePane.selected[0];
    if (!source) {
      activity = 'Rename needs one selected entry.';
      return;
    }
    const currentName = basename(source);
    const newName = window.prompt('New name or full destination path', currentName);
    if (!newName || newName === currentName) return;
    const destination = hasPathSeparator(newName) ? newName : joinPanePath(parentPath(source), newName);
    try {
      await renameEntry(source, destination);
      await refreshPane(activePane.id);
      activity = `Renamed ${currentName}.`;
    } catch (error) {
      activity = `Rename failed: ${engineMessage(error)}`;
    }
  }

  async function showInfo(path: string) {
    try {
      infoText = await inspectPath(path);
      activity = `Info loaded for ${path}.`;
    } catch (error) {
      infoText = '';
      activity = `Info failed: ${engineMessage(error)}`;
    }
  }

  function applyCommandJson() {
    try {
      commandBar = parseCommandBarConfig(commandJson);
      showConfig = false;
      activity = 'Command bank updated in memory.';
    } catch (error) {
      activity = error instanceof Error ? error.message : String(error);
    }
  }

  function sortPane(id: 'left' | 'right', mode: SortMode) {
    replacePane(id, (pane) => setSortMode(pane, mode));
  }

  function swapPanes() {
    const oldLeft = left;
    left = { ...right, id: 'left', active: oldLeft.active };
    right = { ...oldLeft, id: 'right', active: !oldLeft.active };
    activity = 'Source and destination swapped.';
  }

  function replacePane(id: 'left' | 'right', transform: (pane: PaneState) => PaneState) {
    if (id === 'left') {
      left = transform(left);
    } else {
      right = transform(right);
    }
  }

  function updatePane(id: 'left' | 'right', patch: Partial<PaneState>) {
    replacePane(id, (pane) => ({ ...pane, ...patch }));
  }

  function engineMessage(error: unknown): string {
    const engine = error as EngineError;
    return engine?.message ?? (error instanceof Error ? error.message : String(error));
  }

  function homeishPath() {
    return navigator.platform.toLowerCase().includes('win') ? 'C:\\' : '/';
  }

  function joinPanePath(parent: string, child: string) {
    const separator = parent.includes('\\') ? '\\' : '/';
    return `${parent.replace(/[\\/]+$/, '')}${separator}${child}`;
  }

  function basename(path: string) {
    const trimmed = path.replace(/[\\/]+$/, '');
    const slash = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'));
    return slash >= 0 ? trimmed.slice(slash + 1) : trimmed;
  }

  function hasPathSeparator(path: string) {
    return path.includes('/') || path.includes('\\');
  }

  function useSampleData() {
    const sample = normalizeListing('/PiStorm-Hybrid.img', {
      path: '/PiStorm-Hybrid.img',
      entries: [
        { name: 'MBR', type: 'PartitionTable', size: 512 },
        { name: 'RDB', type: 'PartitionTable', size: 512 },
        { name: 'DH0', type: 'Partition', size: 268435456 },
        { name: 'DH1', type: 'Partition', size: 1073741824 },
        { name: 'Workbench.lha', type: 'File', size: 90112 }
      ]
    });
    replacePane(activePane.id, (pane) => setEntries(pane, sample));
    activity = 'Loaded deterministic hybrid-image sample view.';
  }
</script>

<main class="shell">
  <header class="titlebar">
    <div>
      <strong>HST IMAGER DOPUS</strong>
      <span>Dual-pane Amiga filesystem operator</span>
    </div>
    <button class="ghost" on:click={useSampleData}>Hybrid sample</button>
  </header>

  <section class="panes" aria-label="file panes">
    <div class:active={left.active} class="pane">
      <PaneView pane={left} onParent={() => goParent('left')} onRefresh={() => refreshPane('left')} onSort={(mode) => sortPane('left', mode)} onToggle={(entry) => selectEntry('left', entry)} onOpen={(entry) => navigate('left', entry)} />
    </div>
    <div class:active={right.active} class="pane">
      <PaneView pane={right} onParent={() => goParent('right')} onRefresh={() => refreshPane('right')} onSort={(mode) => sortPane('right', mode)} onToggle={(entry) => selectEntry('right', entry)} onOpen={(entry) => navigate('right', entry)} />
    </div>
  </section>

  <footer class="command-bank" aria-label="command bank">
    {#each commandBar as button}
      <button class:disabled={!button.enabled} disabled={!button.enabled} title={button.hint} on:click={() => runCommand(button)}>
        {button.label}
      </button>
    {/each}
  </footer>

  {#if showConfig}
    <section class="config-panel">
      <textarea bind:value={commandJson} spellcheck="false"></textarea>
      <div>
        <button on:click={applyCommandJson}>Apply Buttons</button>
        <button on:click={() => (showConfig = false)}>Close</button>
      </div>
    </section>
  {/if}

  {#if infoText}
    <section class="info-panel">
      <button on:click={() => (infoText = '')}>Close Info</button>
      <pre>{infoText}</pre>
    </section>
  {/if}

  <div class="activity">{activity}</div>
</main>
