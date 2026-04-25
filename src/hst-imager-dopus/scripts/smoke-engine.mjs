import { mkdtemp, mkdir, writeFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const appRoot = resolve(new URL('..', import.meta.url).pathname);
const repoRoot = resolve(appRoot, '../..');
const goRoot = join(repoRoot, 'src', 'hst-imager-go');
const engine = process.env.HST_DOPUS_ENGINE_PATH ?? join(appRoot, '.engine-test', process.platform === 'win32' ? 'hst-imager-go.exe' : 'hst-imager-go');

if (!process.env.HST_DOPUS_ENGINE_PATH) {
  await mkdir(dirname(engine), { recursive: true });
  run('go', ['build', '-o', engine], goRoot);
} else if (!existsSync(engine)) {
  throw new Error(`HST_DOPUS_ENGINE_PATH points to a missing binary: ${engine}`);
}

const work = await mkdtemp(join(tmpdir(), 'hst-dopus-smoke-'));
const src = join(work, 'source');
const dst = join(work, 'destination');
await mkdir(src);
await mkdir(dst);
await writeFile(join(src, 'local.txt'), 'dopus smoke\n');

const checks = [];

checks.push(checkJson('browse-local-folder', ['fs', 'dir', src], (json) => {
  assertEntry(json, 'local.txt');
}));

const archive = findFirst([
  join(repoRoot, 'src', 'Hst.Imager.Core.Tests', 'TestData', 'Zip', 'dirs-files.zip'),
  join(repoRoot, 'src', 'Hst.Imager.Core.Tests', 'TestData', 'Archives', 'dirs-files.zip')
]);
if (archive) {
  checks.push(checkJson('browse-archive-fixture', ['fs', 'dir', archive], (json) => {
    if (!Array.isArray(json.entries)) throw new Error('archive listing has no entries array');
  }));
} else {
  console.log('SKIP: browse-archive-fixture (no fixture found)');
}

const rdbFixture = findFirst([
  join(repoRoot, 'src', 'Hst.Imager.Core.Tests', 'TestData', 'rigid-disk-block.img')
]);
if (rdbFixture) {
  checks.push(checkJson('browse-rdb-image-fixture', ['fs', 'dir', `${rdbFixture}\\rdb`], (json) => {
    assertPartitionRawPath(json, '\\rdb\\1');
    assertPartitionRawPath(json, '\\rdb\\2');
  }));
} else {
  console.log('SKIP: browse-rdb-image-fixture (no fixture found)');
}

checks.push(checkCommand('mkdir-through-engine', ['fs', 'mkdir', join(dst, 'created')]));
checks.push(checkCommand('copy-local-file-through-engine', ['fs', 'copy', join(src, 'local.txt'), dst]));
checks.push(checkJson('verify-copy-result', ['fs', 'dir', dst], (json) => {
  assertEntry(json, 'local.txt');
}));
checks.push(checkCommand('rename-local-file-through-engine', ['fs', 'rename', join(dst, 'local.txt'), join(dst, 'renamed.txt')]));
checks.push(checkJson('verify-rename-result', ['fs', 'dir', dst], (json) => {
  assertEntry(json, 'renamed.txt');
  assertNoEntry(json, 'local.txt');
}));
checks.push(checkCommand('delete-local-file-through-engine', ['fs', 'delete', join(dst, 'renamed.txt')]));
checks.push(checkJson('verify-delete-result', ['fs', 'dir', dst], (json) => {
  assertNoEntry(json, 'renamed.txt');
}));
checks.push(checkFailure('invalid-path-engine-error', ['fs', 'dir', join(work, 'missing')]));

if (process.env.HST_DOPUS_HEAVY_SMOKE === '1') {
  checks.push(await runPiStormHybridSmoke(work));
} else {
  console.log('SKIP: browse-pistorm-hybrid-generated (set HST_DOPUS_HEAVY_SMOKE=1)');
}

const failed = checks.filter((check) => check === false).length;
if (failed > 0) {
  console.error(`Smoke failed: ${failed} check(s) failed.`);
  process.exit(1);
}
console.log(`Smoke passed: ${checks.length} check(s).`);

function checkJson(name, args, verify) {
  try {
    const result = engineJson(args);
    verify(result);
    console.log(`PASS: ${name}`);
    return true;
  } catch (error) {
    console.error(`FAIL: ${name}: ${error.message}`);
    return false;
  }
}

function checkCommand(name, args) {
  try {
    engineJson(args);
    console.log(`PASS: ${name}`);
    return true;
  } catch (error) {
    console.error(`FAIL: ${name}: ${error.message}`);
    return false;
  }
}

function checkFailure(name, args) {
  const result = spawnSync(engine, ['--format', 'json', ...args], { encoding: 'utf8', env: engineEnv() });
  if (result.status === 0) {
    console.error(`FAIL: ${name}: expected non-zero exit`);
    return false;
  }
  console.log(`PASS: ${name}`);
  return true;
}

async function runPiStormHybridSmoke(work) {
  const image = join(work, 'pistorm-hybrid.img');
  const fsBin = join(repoRoot, 'src', 'Hst.Imager.Core.Tests', 'TestData', 'Pfs3', 'pfs3aio');
  if (!existsSync(fsBin)) {
    console.log('SKIP: browse-pistorm-hybrid-generated (missing pfs3aio fixture)');
    return true;
  }

  try {
    engineJson(['blank', image, '3GB']);
    engineJson(['format', image, 'pistorm', 'pds3', '--max-partition-size', '1GB', '--file-system-path', fsBin]);
    const mbrNames = entryNames(engineJson(['fs', 'dir', `${image}\\mbr`])).join(' ');
    if (!/\b1\b/.test(mbrNames) || !/\b2\b/.test(mbrNames)) {
      throw new Error(`expected MBR partition entries 1 and 2, got: ${mbrNames}`);
    }
    const mbr2 = engineJson(['fs', 'dir', `${image}\\mbr\\2`]);
    assertNavigableEntry(mbr2, 'RDB', 'container');

    const rdb = engineJson(['fs', 'dir', `${image}\\mbr\\2\\rdb`]);
    const namedPart = findEntry(rdb, (entry) => entry.type === 'part' && typeof entry.partitionName === 'string' && entry.partitionName.length > 0);
    if (!namedPart) {
      throw new Error(`expected named RDB partition, got: ${JSON.stringify(rdb.entries ?? [])}`);
    }
    if (namedPart.type !== 'part' || typeof namedPart.size !== 'number' || namedPart.size <= 0) {
      throw new Error(`expected named RDB partition metadata with type part and positive size, got: ${JSON.stringify(namedPart)}`);
    }
    if (!String(namedPart.rawPath ?? '').endsWith('\\mbr\\2\\rdb\\1')) {
      throw new Error(`expected stable numeric RDB rawPath, got: ${JSON.stringify(namedPart)}`);
    }
    if (namedPart.formattedName !== namedPart.partitionName || namedPart.selector !== namedPart.partitionName) {
      throw new Error(`expected display selector to match partition name, got: ${JSON.stringify(namedPart)}`);
    }
    console.log('PASS: browse-pistorm-hybrid-generated');
    return true;
  } catch (error) {
    console.error(`FAIL: browse-pistorm-hybrid-generated: ${error.message}`);
    return false;
  }
}

function engineJson(args) {
  const result = spawnSync(engine, ['--format', 'json', ...args], { encoding: 'utf8', env: engineEnv() });
  if (result.status !== 0) {
    throw new Error(result.stderr.trim() || result.stdout.trim() || `exit ${result.status}`);
  }
  try {
    return JSON.parse(result.stdout);
  } catch (error) {
    throw new Error(`invalid JSON: ${error.message}\n${result.stdout}`);
  }
}

function run(command, args, cwd) {
  const result = spawnSync(command, args, { cwd, stdio: 'inherit' });
  if (result.status !== 0) process.exit(result.status ?? 1);
}

function engineEnv() {
  return { ...process.env, HST_IMAGER_LEGACY_MODE: 'off' };
}

function findFirst(paths) {
  return paths.find((path) => existsSync(path));
}

function entryNames(json) {
  return Array.isArray(json.entries)
    ? json.entries.map((entry) => entry.formattedName ?? entry.name ?? basename(entry.rawPath ?? entry.path ?? '')).filter(Boolean)
    : [];
}

function findEntry(json, predicate) {
  return Array.isArray(json.entries) ? json.entries.find(predicate) : undefined;
}

function assertNavigableEntry(json, name, type) {
  const entry = findEntry(json, (item) => item.name === name || item.formattedName === name);
  if (!entry) {
    throw new Error(`entry not found: ${name}`);
  }
  if (entry.type !== type && entry.kind !== type) {
    throw new Error(`expected ${name} to have type ${type}, got: ${JSON.stringify(entry)}`);
  }
  if (!entry.rawPath && !entry.path) {
    throw new Error(`expected ${name} to expose a navigation path, got: ${JSON.stringify(entry)}`);
  }
}

function assertPartitionRawPath(json, suffix) {
  const entry = findEntry(json, (item) => item.type === 'part' && String(item.rawPath ?? item.path ?? '').endsWith(suffix));
  if (!entry) {
    throw new Error(`expected partition raw path suffix ${suffix}, got: ${JSON.stringify(json.entries ?? [])}`);
  }
}

function assertEntry(json, name) {
  if (!entryNames(json).some((entry) => entry === name || entry.endsWith(`/${name}`) || entry.endsWith(`\\${name}`))) {
    throw new Error(`entry not found: ${name}`);
  }
}

function assertNoEntry(json, name) {
  if (entryNames(json).some((entry) => entry === name || entry.endsWith(`/${name}`) || entry.endsWith(`\\${name}`))) {
    throw new Error(`unexpected entry found: ${name}`);
  }
}

function basename(path) {
  return String(path).split(/[\\/]/).pop();
}
