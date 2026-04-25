import { describe, expect, it } from 'vitest';
import { canNavigate, createPane, normalizeListing, setEntries, toggleSelection } from './pane';

describe('pane model', () => {
  it('normalizes local fs dir entries with missing names from rawPath', () => {
    const listing = normalizeListing('/tmp/src', {
      path: '/tmp/src',
      entries: [{ rawPath: '/tmp/src/file.txt', size: 5, type: 'File' }]
    });

    expect(listing.entries[0]).toMatchObject({
      name: 'file.txt',
      path: '/tmp/src/file.txt',
      kind: 'file',
      size: 5
    });
  });

  it('marks archive, image, and partition entries navigable', () => {
    const listing = normalizeListing('/images', {
      entries: [
        { name: 'Workbench.lha', type: 'File' },
        { name: 'pistorm.img', type: 'File' },
        { name: 'RDB', type: 'PartitionTable' }
      ]
    });

    expect(listing.entries.map((entry) => [entry.name, entry.kind, canNavigate(entry)])).toEqual([
      ['pistorm.img', 'image', true],
      ['RDB', 'container', true],
      ['Workbench.lha', 'archive', true]
    ]);
  });

  it('normalizes partition container entries from hst-imager-go', () => {
    const listing = normalizeListing('/tmp/pistorm.img\\mbr', {
      path: '/tmp/pistorm.img\\mbr',
      entries: [{ name: '1', type: 'part', size: 1024 }]
    });

    expect(listing.entries[0]).toMatchObject({
      name: '1',
      path: '/tmp/pistorm.img\\mbr\\1',
      kind: 'partition'
    });
    expect(canNavigate(listing.entries[0])).toBe(true);
  });

  it('displays named partition labels while keeping raw navigation paths', () => {
    const listing = normalizeListing('/tmp/pistorm.img\\rdb', {
      path: '/tmp/pistorm.img\\rdb',
      entries: [{ name: '1', formattedName: 'DH0', partitionName: 'DH0', selector: 'DH0', rawPath: '/tmp/pistorm.img\\rdb\\1', type: 'part', size: 1024 }]
    });

    expect(listing.entries[0]).toMatchObject({
      name: 'DH0',
      path: '/tmp/pistorm.img\\rdb\\1',
      kind: 'partition'
    });
    expect(canNavigate(listing.entries[0])).toBe(true);
  });

  it('uses hst-imager virtual paths for image container entries without raw paths', () => {
    const listing = normalizeListing('/Volumes/AmigaShare/YoutubeTutorial/Amikit.dmg', {
      path: '/Volumes/AmigaShare/YoutubeTutorial/Amikit.dmg',
      entries: [{ name: 'MBR', formattedName: 'MBR', rawPath: null, size: 31927042048, type: 0 }]
    });

    expect(listing.entries[0]).toMatchObject({
      name: 'MBR',
      path: '/Volumes/AmigaShare/YoutubeTutorial/Amikit.dmg\\mbr',
      kind: 'container'
    });
  });

  it('normalizes host and archive symlink entries as non-navigable links', () => {
    const listing = normalizeListing('/tmp/archive.zip', {
      path: '/tmp/archive.zip',
      entries: [
        { name: 'host-link', type: 'softlink', link: 'target.txt' },
        { name: 'archive-link', kind: 'softlink', isSymlink: true, link: 'target.txt' }
      ]
    });

    expect(listing.entries.map((entry) => [entry.name, entry.kind, entry.linkTarget, canNavigate(entry)])).toEqual([
      ['archive-link', 'link', 'target.txt', false],
      ['host-link', 'link', 'target.txt', false]
    ]);
  });

  it('updates pane entries and clears selection after navigation', () => {
    const pane = createPane('left', '/old', true);
    const listing = normalizeListing('/new', { path: '/new', entries: [{ name: 'A', type: 'Directory' }] });
    const selected = toggleSelection(setEntries(pane, listing), '/new/A');
    const navigated = setEntries(selected, normalizeListing('/other', { path: '/other', entries: [] }));

    expect(selected.selected).toEqual(['/new/A']);
    expect(navigated.selected).toEqual([]);
    expect(navigated.path).toBe('/other');
  });
});
