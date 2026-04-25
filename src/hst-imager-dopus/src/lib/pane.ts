import type { EntryKind, PaneEntry, PaneListing, PaneState, SortMode } from './models';

type RawEntry = Record<string, unknown>;

const DIRECTORY_TYPES = new Set(['dir', 'directory', 'folder']);
const LINK_TYPES = new Set(['link', 'softlink', 'symlink', 'linkfile', 'linkdir']);
const ARCHIVE_EXTENSIONS = ['.lha', '.lzx', '.zip', '.rar', '.tar', '.gz', '.xz', '.z', '.lzw'];
const IMAGE_EXTENSIONS = ['.adf', '.dmg', '.hdf', '.img', '.vhd', '.vhdx', '.iso'];

export function createPane(id: 'left' | 'right', path: string, active = false): PaneState {
  return {
    id,
    path,
    entries: [],
    selected: [],
    sortMode: 'name',
    active,
    loading: false,
    error: null
  };
}

export function normalizeListing(requestedPath: string, raw: unknown): PaneListing {
  const root = asRecord(raw);
  const entries = Array.isArray(root.entries) ? root.entries : [];
  const listingPath = stringField(root, ['path', 'rawPath']) ?? requestedPath;

  return {
    path: listingPath,
    entries: entries.map((entry) => normalizeEntry(listingPath, asRecord(entry))).sort(compareEntries('name')),
    raw
  };
}

export function normalizeEntry(parentPath: string, raw: RawEntry): PaneEntry {
  const rawPath = stringField(raw, ['rawPath', 'path', 'fullPath']);
  const displayName =
    stringField(raw, ['formattedName', 'name', 'label']) ??
    basename(rawPath ?? '') ??
    stringField(raw, ['type']) ??
    '<unnamed>';
  const path = rawPath ?? joinVirtualPath(parentPath, displayName, raw);

  return {
    name: displayName,
    path,
    kind: inferKind(displayName, raw),
    size: numberField(raw, ['size', 'bytes', 'length']),
    date: stringField(raw, ['date', 'modified', 'lastModified']) ?? null,
    attributes: attributes(raw),
    linkTarget: stringField(raw, ['link', 'linkPath', 'target', 'targetPath']) ?? null,
    raw
  };
}

export function compareEntries(sortMode: SortMode): (a: PaneEntry, b: PaneEntry) => number {
  return (a, b) => {
    if (a.kind === 'directory' && b.kind !== 'directory') return -1;
    if (a.kind !== 'directory' && b.kind === 'directory') return 1;

    if (sortMode === 'size') {
      return (a.size ?? -1) - (b.size ?? -1) || a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
    }

    if (sortMode === 'date') {
      return (a.date ?? '').localeCompare(b.date ?? '') || a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
    }

    if (sortMode === 'kind') {
      return a.kind.localeCompare(b.kind) || a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
    }

    return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
  };
}

export function setEntries(state: PaneState, listing: PaneListing): PaneState {
  return {
    ...state,
    path: listing.path,
    entries: listing.entries.slice().sort(compareEntries(state.sortMode)),
    selected: [],
    loading: false,
    error: null
  };
}

export function toggleSelection(state: PaneState, entryPath: string): PaneState {
  const selected = new Set(state.selected);
  if (selected.has(entryPath)) {
    selected.delete(entryPath);
  } else {
    selected.add(entryPath);
  }
  return { ...state, selected: [...selected] };
}

export function setSortMode(state: PaneState, sortMode: SortMode): PaneState {
  return {
    ...state,
    sortMode,
    entries: state.entries.slice().sort(compareEntries(sortMode))
  };
}

export function canNavigate(entry: PaneEntry): boolean {
  return entry.kind === 'directory' || entry.kind === 'archive' || entry.kind === 'image' || entry.kind === 'container' || entry.kind === 'partition';
}

export function parentPath(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, '');
  const slash = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'));
  if (slash <= 0) return path;
  return trimmed.slice(0, slash);
}

function inferKind(name: string, raw: RawEntry): EntryKind {
  const type = (stringField(raw, ['type', 'kind']) ?? '').toLowerCase();
  const lowerName = name.toLowerCase();

  if (LINK_TYPES.has(type) || booleanField(raw, ['isSymlink', 'symlink'])) return 'link';
  if (DIRECTORY_TYPES.has(type) || booleanField(raw, ['isDirectory', 'directory'])) return 'directory';
  if (lowerName === 'mbr' || lowerName === 'gpt' || lowerName === 'rdb') return 'container';
  if (type.includes('partition') || type === 'part' || type === 'mbr' || type === 'gpt' || type === 'rdb') return 'partition';
  if (type.includes('archive')) return 'archive';
  if (type.includes('image') || type.includes('disk') || type.includes('media')) return 'image';
  if (type.includes('container')) return 'container';
  if (ARCHIVE_EXTENSIONS.some((ext) => lowerName.endsWith(ext))) return 'archive';
  if (IMAGE_EXTENSIONS.some((ext) => lowerName.endsWith(ext))) return 'image';
  if (type === 'file') return 'file';
  return 'unknown';
}

function attributes(raw: RawEntry): string[] {
  const values = raw.attributes ?? raw.flags ?? raw.protectionBits;
  if (Array.isArray(values)) return values.map(String);
  if (typeof values === 'string' && values.length > 0) return values.split(/[,\s]+/).filter(Boolean);
  return [];
}

function asRecord(value: unknown): RawEntry {
  return value && typeof value === 'object' ? (value as RawEntry) : {};
}

function stringField(raw: RawEntry, keys: string[]): string | null {
  for (const key of keys) {
    const value = raw[key];
    if (typeof value === 'string' && value.length > 0) return value;
    if (typeof value === 'number') return String(value);
  }
  return null;
}

function numberField(raw: RawEntry, keys: string[]): number | null {
  for (const key of keys) {
    const value = raw[key];
    if (typeof value === 'number' && Number.isFinite(value)) return value;
    if (typeof value === 'string' && value.trim() !== '') {
      const parsed = Number(value);
      if (Number.isFinite(parsed)) return parsed;
    }
  }
  return null;
}

function booleanField(raw: RawEntry, keys: string[]): boolean {
  return keys.some((key) => raw[key] === true);
}

function basename(path: string): string | null {
  const trimmed = path.replace(/[\\/]+$/, '');
  if (!trimmed) return null;
  const slash = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'));
  return slash >= 0 ? trimmed.slice(slash + 1) : trimmed;
}

function joinVirtualPath(parent: string, child: string, raw: RawEntry): string {
  const type = (stringField(raw, ['type', 'kind']) ?? '').toLowerCase();
  const lowerChild = child.toLowerCase();
  if (lowerChild === 'mbr' || lowerChild === 'gpt' || lowerChild === 'rdb') {
    return `${parent.replace(/[\\/]+$/, '')}\\${lowerChild}`;
  }
  if (type === 'part') {
    return `${parent.replace(/[\\/]+$/, '')}\\${child}`;
  }
  const separator = parent.includes('\\') ? '\\' : '/';
  return `${parent.replace(/[\\/]+$/, '')}${separator}${child}`;
}
