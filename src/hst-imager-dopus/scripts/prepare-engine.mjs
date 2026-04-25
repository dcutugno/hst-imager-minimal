import { copyFile, mkdir, stat } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const appRoot = resolve(new URL('..', import.meta.url).pathname);
const repoRoot = resolve(appRoot, '../..');
const goRoot = join(repoRoot, 'src', 'hst-imager-go');
const binaryDir = join(appRoot, 'src-tauri', 'binaries');

const targetTriple = process.env.HST_DOPUS_TARGET_TRIPLE ?? currentTargetTriple();
const sidecarName = process.platform === 'win32' ? `hst-imager-go-${targetTriple}.exe` : `hst-imager-go-${targetTriple}`;
const output = join(binaryDir, sidecarName);

await mkdir(binaryDir, { recursive: true });

const override = process.env.HST_DOPUS_ENGINE_PATH;
if (override) {
  await ensureFile(override, 'HST_DOPUS_ENGINE_PATH');
  await copyFile(override, output);
  console.log(`Copied engine override to ${output}`);
  process.exit(0);
}

const env = {
  ...process.env,
  GOOS: goos(),
  GOARCH: goarch()
};

const result = spawnSync('go', ['build', '-o', output], {
  cwd: goRoot,
  env,
  stdio: 'inherit'
});

if (result.status !== 0) {
  process.exit(result.status ?? 1);
}

console.log(`Built hst-imager-go sidecar at ${output}`);

function currentTargetTriple() {
  if (process.platform === 'darwin') {
    return process.arch === 'arm64' ? 'aarch64-apple-darwin' : 'x86_64-apple-darwin';
  }
  if (process.platform === 'win32') return 'x86_64-pc-windows-msvc';
  return process.arch === 'arm64' ? 'aarch64-unknown-linux-gnu' : 'x86_64-unknown-linux-gnu';
}

function goos() {
  if (process.platform === 'win32') return 'windows';
  if (process.platform === 'darwin') return 'darwin';
  return 'linux';
}

function goarch() {
  return process.arch === 'arm64' ? 'arm64' : 'amd64';
}

async function ensureFile(path, label) {
  try {
    const info = await stat(path);
    if (!info.isFile()) throw new Error(`${label} is not a file: ${path}`);
  } catch (error) {
    throw new Error(`${label} points to a missing hst-imager-go binary: ${path}. ${error.message}`);
  }
}
