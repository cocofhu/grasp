#!/usr/bin/env node
// Only explicit, stopped, fresh Compose installations use this wrapper.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';

const digest = value => createHash('sha256').update(value).digest('hex');
const inside = (root, other) => other === root || other.startsWith(root + path.sep);
const overlaps = (a, b) => inside(a, b) || inside(b, a);
const fail = message => { throw new Error(message); };
function writePrivate(file, data, replace = false) {
  const flags = fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_NOFOLLOW |
    (replace ? fs.constants.O_TRUNC : fs.constants.O_EXCL);
  const fd = fs.openSync(file, flags, 0o600);
  try { fs.fchmodSync(fd, 0o600); fs.writeFileSync(fd, data); fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
  const parent = fs.openSync(path.dirname(file), fs.constants.O_RDONLY);
  try { fs.fsyncSync(parent); } finally { fs.closeSync(parent); }
}
const privateJSON = file => {
  const stat = fs.lstatSync(file);
  if (!stat.isFile() || (stat.mode & 0o077)) fail('Initialization receipts must be private regular files.');
  return JSON.parse(fs.readFileSync(file, 'utf8'));
};
function realRoot(value) {
  if (!path.isAbsolute(value) || value.includes('\0')) fail('Initialization requires absolute persistent paths.');
  let current = path.normalize(value);
  const suffix = [];
  while (!fs.existsSync(current)) {
    suffix.unshift(path.basename(current));
    current = path.dirname(current);
  }
  return path.join(fs.realpathSync(current), ...suffix);
}
function receipt(output) {
  for (const line of output.trim().split('\n').reverse()) {
    try { const row = JSON.parse(line); if (row && typeof row === 'object') return row; } catch { /* Non-JSON diagnostics stay private. */ }
  }
  fail('Storage administration returned no verifiable receipt.');
}

export function initializeExecution({ offline = false, source = false, env = process.env, cwd = process.cwd(),
  workdir = '/execution-maintenance/fresh-install', controlMount = '/execution-control', adminCommand,
  run = spawnSync } = {}) {
  if (!offline) fail('Explicit --offline confirmation is required; stop all writers first.');
  if (env.GRASP_DB_DRIVER && env.GRASP_DB_DRIVER !== 'sqlite') fail('Fresh Compose initialization supports SQLite; use UPGRADING.md for existing databases.');
  const sql = realRoot(env.GRASP_DB || '/data/grasp.db');
  const payload = realRoot(path.resolve(cwd, env.GRASP_BLOBS_ROOT || 'data/blobs'));
  const work = realRoot(workdir);
  const driver = env.GRASP_EXECUTION_CONTROL_DRIVER || 'local';
  if (!['local', 's3', 'cos'].includes(driver)) fail('Configure an independent execution control driver.');
  const control = { driver, blobs_root: env.GRASP_EXECUTION_CONTROL_BLOBS_ROOT || '/execution-control', s3: {} };
  for (const [key, field] of Object.entries({ ENDPOINT: 'endpoint', REGION: 'region', BUCKET: 'bucket', PREFIX: 'prefix',
    ACCESS_KEY_ID: 'access_key_id', SECRET_ACCESS_KEY: 'secret_access_key', SESSION_TOKEN: 'session_token' })) {
    control.s3[field] = env['GRASP_EXECUTION_CONTROL_S3_' + key] || '';
  }
  const style = env.GRASP_EXECUTION_CONTROL_S3_PATH_STYLE;
  if (style) {
    if (!/^(true|false|1|0)$/i.test(style)) fail('Invalid execution control path-style setting.');
    control.s3.path_style = /^(true|1)$/i.test(style);
  }
  const roots = [path.dirname(sql), payload, work];
  if (driver === 'local') {
    const local = realRoot(control.blobs_root);
    if (!inside(realRoot(controlMount), local)) fail('Local execution control must use its independent persistent mount.');
    roots.push(local);
  }
  for (let i = 0; i < roots.length; i++) {
    for (let j = i + 1; j < roots.length; j++) if (overlaps(roots[i], roots[j])) fail('SQL, payload, control and maintenance paths must be separate.');
  }
  const originalConfig = source && fs.existsSync(path.join(cwd, 'config.yaml'))
    ? fs.readFileSync(path.join(cwd, 'config.yaml')) : Buffer.from('{}\n');
  const identity = digest(JSON.stringify({ sql, work, source, config: digest(originalConfig),
    environment: Object.fromEntries(Object.entries(env).filter(([key]) => /^(GRASP_(DB|BLOBS_ROOT|STORAGE_DRIVER|S3_|EXECUTION_CONTROL_))/.test(key)).sort()) }));
  const checkpoint = path.join(work, 'fresh-install.json');
  const databaseExists = fs.existsSync(sql);
  const retry = fs.existsSync(checkpoint);
  let ticket;
  let operation;
  if (retry) {
    const prior = privateJSON(checkpoint);
    if (prior.version !== 1 || prior.identity !== identity) fail('Original fresh-install configuration changed; preserve the workdir and follow UPGRADING.md.');
    const originalOperation = path.join(work, 'execution-initialization.json');
    if (databaseExists || fs.existsSync(originalOperation)) operation = privateJSON(originalOperation);
    const originalTicket = path.join(work, 'execution-ticket.json');
    if (databaseExists || fs.existsSync(originalTicket)) {
      ticket = privateJSON(path.join(work, 'execution-ticket.json'));
    }
  } else if (databaseExists || (fs.existsSync(work) && fs.readdirSync(work).length)) {
    fail('Existing SQL or maintenance data is not a fresh installation; follow server/UPGRADING.md.');
  }
  if (databaseExists && !fs.lstatSync(sql).isFile()) fail('SQLite must be a regular file.');
  if (fs.existsSync(work) && (fs.statSync(work).mode & 0o077)) fail('Initialization workdir must be private (0700).');
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'grasp-fresh-init-'));
  fs.chmodSync(temp, 0o700);
  try {
    const configFile = path.join(temp, 'application.json');
    const controlFile = path.join(temp, 'control.json');
    writePrivate(configFile, originalConfig);
    writePrivate(controlFile, JSON.stringify({ storage: control }));
    const command = adminCommand || (source ? ['go', 'run', './cmd/storage-admin'] : ['/app/grasp-storage-admin']);
    const call = mode => {
      const args = [...command.slice(1), '--mode', mode, '--config', configFile, '--execution-control', controlFile, '--workdir', work];
      if (mode === 'execution-init') args.push('--offline');
      const result = run(command[0], args, { cwd, env, encoding: 'utf8', maxBuffer: 16 << 20 });
      if (mode === 'execution-init') writePrivate(path.join(work, 'initialization.log'), (result.stdout || '') + (result.stderr || ''), true);
      return { ...result, view: receipt(result.stdout || '') };
    };
    const before = call('execution-status'); // GET/SELECT only; never creates a missing marker.
    if (ticket) {
      const marker = ticket.marker;
      if (!marker || !['active', 'inactive'].includes(before.view.status) ||
          ['namespaceId', 'datasetId', 'lineage'].some(key => before.view[key] !== marker[key])) {
        fail('Original independent control marker is missing or changed; initialization was not replayed.');
      }
    } else if (operation) {
      // The original UUID is already durable, but its first marker ACK may
      // have been lost before a ticket/SQL was written. The admin verifies
      // that exact UUID against the existing inactive marker; never create a
      // replacement marker if it disappeared.
      if (databaseExists || before.view.status !== 'inactive') fail('Original initialization marker cannot be confirmed; preserve its workdir.');
    } else if (before.view.status !== 'uninitialized') {
      fail('Independent control is not an unused fresh location; preserve the original initialization workdir.');
    }
    if (!retry) {
      fs.mkdirSync(work, { recursive: true, mode: 0o700 });
      writePrivate(checkpoint, JSON.stringify({ version: 1, identity }));
    }
    const initialized = call('execution-init');
    if (initialized.error || initialized.status !== 0 || initialized.view.active !== true) fail('Initialization is incomplete; preserve and retry the original workdir.');
    const checked = call('execution-status');
    if (checked.status !== 0 || !checked.view.active || !checked.view.sqlMatches || checked.view.maintenanceActive) {
      fail('Initialization is not confirmed; preserve and retry the original workdir.');
    }
    return { active: true, sqlMatches: true, maintenanceActive: false, datasetId: checked.view.datasetId, lineage: checked.view.lineage };
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const args = process.argv.slice(2);
    if (!args.includes('--offline') || args.some(arg => !['--offline', '--source'].includes(arg))) fail('usage: init-execution.mjs --offline [--source]');
    console.log(JSON.stringify(initializeExecution({ offline: true, source: args.includes('--source') })));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
