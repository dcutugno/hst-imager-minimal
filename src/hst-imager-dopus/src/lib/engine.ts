import { invoke } from '@tauri-apps/api/core';
import { normalizeListing } from './pane';
import type { PaneListing } from './models';

export async function browsePath(path: string): Promise<PaneListing> {
  const raw = await invoke<unknown>('browse_path', { path });
  return normalizeListing(path, raw);
}

export async function copyEntries(sources: string[], destination: string): Promise<void> {
  await invoke('copy_entries', { sources, destination });
}

export async function extractEntry(source: string, destination: string): Promise<void> {
  await invoke('extract_entry', { source, destination });
}

export async function makeDirectory(path: string): Promise<void> {
  await invoke('make_directory', { path });
}

export async function deleteEntries(paths: string[]): Promise<void> {
  await invoke('delete_entries', { paths });
}

export async function renameEntry(source: string, destination: string): Promise<void> {
  await invoke('rename_entry', { source, destination });
}

export async function inspectPath(path: string): Promise<string> {
  return invoke<string>('inspect_path', { path });
}
