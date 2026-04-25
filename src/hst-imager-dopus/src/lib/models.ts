export type EntryKind = 'directory' | 'file' | 'archive' | 'image' | 'partition' | 'container' | 'link' | 'unknown';

export type SortMode = 'name' | 'kind' | 'size' | 'date';

export interface PaneEntry {
  name: string;
  path: string;
  kind: EntryKind;
  size: number | null;
  date: string | null;
  attributes: string[];
  linkTarget: string | null;
  raw: unknown;
}

export interface PaneListing {
  path: string;
  entries: PaneEntry[];
  raw: unknown;
}

export interface PaneState {
  id: 'left' | 'right';
  path: string;
  entries: PaneEntry[];
  selected: string[];
  sortMode: SortMode;
  active: boolean;
  loading: boolean;
  error: string | null;
}

export interface CommandButton {
  id: string;
  label: string;
  action: 'copy' | 'extract' | 'mkdir' | 'info' | 'delete' | 'rename' | 'refresh' | 'swap' | 'config';
  enabled: boolean;
  hint: string;
}

export interface EngineError {
  message: string;
  stderr?: string;
  stdout?: string;
  code?: number | null;
  command?: string[];
}
