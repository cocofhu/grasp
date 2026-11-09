import test, { after } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { initializeExecution } from './init-execution.mjs';

const repo = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const root = fs.mkdtempSync(path.join(os.tmpdir(), 'grasp-install-test-'));
after(() => fs.rmSync(root, { recursive: true, force: true }));
const binary = path.join(root, 'storage-admin');
const built = spawnSync('go', ['build', '-o', binary, './cmd/storage-admin'], {
  cwd: path.join(repo, 'server'), encoding: 'utf8', timeout: 180000,
});
assert.equal(built.status, 0, built.stderr);
function fixture() {
  const dir = fs.mkdtempSync(path.join(root, 'case-'));
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('GRASP_')));
  Object.assign(env, { GRASP_DB_DRIVER: 'sqlite', GRASP_DB: path.join(dir, 'sql', 'grasp.db'),
    GRASP_BLOBS_ROOT: path.join(dir, 'payloads'), GRASP_EXECUTION_CONTROL_DRIVER: 'local',
    GRASP_EXECUTION_CONTROL_BLOBS_ROOT: path.join(dir, 'control') });
  return { dir, offline: true, env, cwd: dir, workdir: path.join(dir, 'maintenance', 'fresh-install'),
    controlMount: path.join(dir, 'control'), adminCommand: [binary] };
}
function marker(f) { return path.join(f.controlMount, 'execution-namespace.json'); }

test('real fresh SQLite initialization and original-ticket retry retain external identity', () => {
  const f = fixture();
  const first = initializeExecution(f);
  assert.equal(first.active, true);
  assert.equal(first.sqlMatches, true);
  const before = fs.readFileSync(marker(f));
  const ticket = fs.readFileSync(path.join(f.workdir, 'execution-ticket.json'));
  const retried = initializeExecution(f);
  assert.deepEqual(retried, first);
  assert.deepEqual(fs.readFileSync(marker(f)), before);
  assert.deepEqual(fs.readFileSync(path.join(f.workdir, 'execution-ticket.json')), ticket);
  assert.equal(fs.statSync(f.workdir).mode & 0o077, 0);
  assert.equal(fs.statSync(path.join(f.workdir, 'fresh-install.json')).mode & 0o077, 0);
});

test('missing external marker never reinitializes an existing SQL database', () => {
  const f = fixture();
  initializeExecution(f);
  const originalSQL = fs.readFileSync(f.env.GRASP_DB);
  fs.unlinkSync(marker(f));
  assert.throws(() => initializeExecution(f), /marker is missing or changed/);
  assert.equal(fs.existsSync(marker(f)), false);
  assert.deepEqual(fs.readFileSync(f.env.GRASP_DB), originalSQL);
});

test('lost successful init ACK is recovered from the original real ticket', () => {
  const f = fixture();
  assert.throws(() => initializeExecution({ ...f, run(command, args, options) {
    const result = spawnSync(command, args, options);
    return args[args.indexOf('--mode') + 1] === 'execution-init' ? { ...result, status: 1 } : result;
  } }), /incomplete/);
  const before = fs.readFileSync(marker(f));
  const originalTicket = fs.readFileSync(path.join(f.workdir, 'execution-ticket.json'));
  assert.equal(initializeExecution(f).active, true);
  assert.deepEqual(fs.readFileSync(marker(f)), before);
  assert.deepEqual(fs.readFileSync(path.join(f.workdir, 'execution-ticket.json')), originalTicket);
});

test('foreign external marker and payload-only credentials never grant initialization', () => {
  const f = fixture();
  const foreign = fixture();
  initializeExecution(f);
  initializeExecution(foreign);
  const foreignMarker = fs.readFileSync(marker(foreign));
  fs.writeFileSync(marker(f), foreignMarker);
  const beforeSQL = fs.readFileSync(f.env.GRASP_DB);
  assert.throws(() => initializeExecution(f), /marker is missing or changed/);
  assert.deepEqual(fs.readFileSync(marker(f)), foreignMarker);
  assert.deepEqual(fs.readFileSync(f.env.GRASP_DB), beforeSQL);
  const missing = fixture();
  Object.assign(missing.env, { GRASP_EXECUTION_CONTROL_DRIVER: 's3',
    GRASP_EXECUTION_CONTROL_S3_PREFIX: 'control', GRASP_S3_PREFIX: 'payload',
    GRASP_S3_ACCESS_KEY_ID: 'payload-only-key', GRASP_S3_SECRET_ACCESS_KEY: 'payload-only-secret' });
  const calls = [];
  assert.throws(() => initializeExecution({ ...missing, run(command, args, options) {
    calls.push(args[args.indexOf('--mode') + 1]);
    return spawnSync(command, args, options);
  } }), /not an unused fresh location/);
  assert.deepEqual(calls, ['execution-status']);
  assert.equal(fs.existsSync(missing.env.GRASP_DB), false);
});

test('existing real SQL without original fresh workdir is refused before any admin call', () => {
  const f = fixture();
  initializeExecution(f);
  const originalSQL = fs.readFileSync(f.env.GRASP_DB);
  let calls = 0;
  assert.throws(() => initializeExecution({ ...f, workdir: path.join(f.dir, 'other-maintenance'),
    run() { calls++; throw new Error('must not run'); } }), /not a fresh installation/);
  assert.equal(calls, 0);
  assert.deepEqual(fs.readFileSync(f.env.GRASP_DB), originalSQL);
});

test('explicit offline, separate roots and unchanged retry inputs are required', () => {
  const f = fixture();
  assert.throws(() => initializeExecution({ ...f, offline: false }), /--offline/);
  assert.equal(fs.existsSync(f.workdir), false);
  assert.throws(() => initializeExecution({ ...f, workdir: path.dirname(f.env.GRASP_DB) }), /must be separate/);
  fs.mkdirSync(f.env.GRASP_BLOBS_ROOT);
  fs.symlinkSync(f.env.GRASP_BLOBS_ROOT, f.controlMount);
  assert.throws(() => initializeExecution(f), /must be separate/);
  fs.unlinkSync(f.controlMount);
  initializeExecution(f);
  assert.throws(() => initializeExecution({ ...f, env: { ...f.env, GRASP_S3_PREFIX: 'different-request' } }), /configuration changed/);
});

test('source/release commands receive private config paths and independent control credentials only', () => {
  for (const source of [false, true]) {
    const f = fixture();
    delete f.adminCommand;
    Object.assign(f.env, { GRASP_EXECUTION_CONTROL_DRIVER: 's3',
      GRASP_EXECUTION_CONTROL_S3_ACCESS_KEY_ID: 'independent-key',
      GRASP_EXECUTION_CONTROL_S3_SECRET_ACCESS_KEY: 'independent-secret',
      GRASP_S3_ACCESS_KEY_ID: 'payload-key', GRASP_S3_SECRET_ACCESS_KEY: 'payload-secret' });
    const sourceConfig = 'database:\n  driver: sqlite\n';
    fs.writeFileSync(path.join(f.cwd, 'config.yaml'), sourceConfig);
    const seen = [];
    initializeExecution({ ...f, source, run(command, args) {
      const config = args[args.indexOf('--config') + 1];
      const control = args[args.indexOf('--execution-control') + 1];
      assert.equal(fs.statSync(config).mode & 0o077, 0);
      assert.equal(fs.statSync(control).mode & 0o077, 0);
      assert.equal(fs.readFileSync(config, 'utf8'), source ? sourceConfig : '{}\n');
      const data = JSON.parse(fs.readFileSync(control));
      assert.equal(data.storage.s3.access_key_id, 'independent-key');
      assert.equal(data.storage.s3.secret_access_key, 'independent-secret');
      assert.doesNotMatch(JSON.stringify(args), /independent-secret|payload-secret|payload-key/);
      const mode = args[args.indexOf('--mode') + 1];
      seen.push({ command, args, mode });
      return { status: 0, stdout: JSON.stringify(mode === 'execution-init' ? { active: true } :
        seen.length === 1 ? { status: 'uninitialized' } : { active: true, sqlMatches: true, maintenanceActive: false }) };
    } });
    assert.equal(seen[0].command, source ? 'go' : '/app/grasp-storage-admin');
    assert.deepEqual(seen[1].args.slice(0, source ? 2 : 0), source ? ['run', './cmd/storage-admin'] : []);
    assert.equal(seen[0].args.includes('--offline'), false);
    assert.equal(seen[1].args.includes('--offline'), true);
  }
});

function shellFixture() {
  const dir = fs.mkdtempSync(path.join(root, 'shell-'));
  fs.mkdirSync(path.join(dir, 'scripts'));
  fs.mkdirSync(path.join(dir, 'server'));
  for (const rel of ['start.sh', 'release-smoke.sh', '.env.example', 'scripts/ensure-secrets-key.sh', 'scripts/init-execution.sh', 'scripts/init-execution.mjs', 'scripts/local-objectstore.sh']) {
    fs.copyFileSync(path.join(repo, rel), path.join(dir, rel));
  }
  fs.writeFileSync(path.join(dir, 'server', 'config.example.yaml'), '{}\n');
  const bin = path.join(dir, 'bin'); fs.mkdirSync(bin);
  const log = path.join(dir, 'commands.jsonl');
  fs.writeFileSync(path.join(bin, 'docker'), `#!/usr/bin/env node
import fs from 'node:fs';
const args=process.argv.slice(2);fs.appendFileSync(process.env.TEST_DOCKER_LOG,JSON.stringify(args)+'\\n');
if(args[0]==='inspect')console.log(args.some(x=>x.includes('.State.Health'))?'healthy':process.env.TEST_STATE||'false exited');
if(args.includes('ps')&&process.env.TEST_PS_FAIL)process.exit(1);
if(args.includes('ps')){const id=args.includes('objectstore')?process.env.TEST_OBJECTSTORE_CONTAINER:process.env.TEST_CONTAINER;if(id)console.log(id);}
`, { mode: 0o755 });
  // .mjs is needed so the fake CLI is independent of the host Node defaults.
  fs.renameSync(path.join(bin, 'docker'), path.join(bin, 'docker.mjs'));
  fs.writeFileSync(path.join(bin, 'docker'), '#!/bin/sh\nexec node "$(dirname "$0")/docker.mjs" "$@"\n', { mode: 0o755 });
  fs.writeFileSync(path.join(bin, 'curl'), '#!/bin/sh\nexit 0\n', { mode: 0o755 });
  return { dir, log, env: { ...process.env, PATH: bin + ':' + process.env.PATH, TEST_DOCKER_LOG: log },
    commands() { return fs.existsSync(log) ? fs.readFileSync(log, 'utf8').trim().split('\n').filter(Boolean).map(JSON.parse) : []; } };
}
test('launcher proves stopped writer, never auto-stops, and forwards exact source/release mode', () => {
  for (const mode of ['release', 'dev']) {
    const f = shellFixture();
    const args = mode === 'dev' ? ['dev', 'init-execution', '--offline'] : ['init-execution', '--offline'];
    const stopped = spawnSync('bash', ['start.sh', ...args], { cwd: f.dir, env: f.env, encoding: 'utf8' });
    assert.equal(stopped.status, 0, stopped.stderr);
    const commands = f.commands();
    const run = commands.find(args => args.includes('run'));
    assert.ok(run.includes('--no-deps'));
    assert.ok(run.includes('--offline'));
    assert.equal(run.includes('--source'), mode === 'dev');
    assert.ok(commands.findIndex(args => args.includes('ps')) < commands.indexOf(run));
    assert.equal(commands.some(args => args.includes('up') || args.includes('stop') || args.includes('down')), false);
    const busy = shellFixture();
    const refused = spawnSync('bash', ['start.sh', ...args], { cwd: busy.dir,
      env: { ...busy.env, TEST_CONTAINER: 'a'.repeat(64), TEST_STATE: 'true running' }, encoding: 'utf8' });
    assert.notEqual(refused.status, 0);
    assert.equal(busy.commands().some(args => args.includes('run') || args.includes('build') || args.includes('stop')), false);
  }
});
test('ordinary up/restart do not initialize and missing offline never runs admin', () => {
  for (const args of [['up'], ['restart'], ['init-execution']]) {
    const f = shellFixture();
    const result = spawnSync('bash', ['start.sh', ...args], { cwd: f.dir, env: f.env, encoding: 'utf8' });
    assert.equal(result.status === 0, args[0] !== 'init-execution', result.stderr);
    assert.equal(f.commands().some(args => args.includes('/opt/grasp-init-execution.mjs')), false);
  }
});

test('bundled storage is ready before explicit fresh init, with stopped writers and retained credentials', () => {
  for (const mode of ['release', 'dev']) {
    const f = shellFixture();
    const args = mode === 'dev' ? ['dev', 'init-execution', '--offline'] : ['init-execution', '--offline'];
    const env = { ...f.env, GRASP_STORAGE_DRIVER: 's3', GRASP_S3_ENDPOINT: '',
      GRASP_S3_ACCESS_KEY_ID: '', GRASP_S3_SECRET_ACCESS_KEY: '', GRASP_S3_SESSION_TOKEN: '',
      TEST_OBJECTSTORE_CONTAINER: 'b'.repeat(64) };
    const result = spawnSync('bash', ['start.sh', ...args], { cwd: f.dir, env, encoding: 'utf8' });
    assert.equal(result.status, 0, result.stderr);
    const commands = f.commands();
    const stopped = commands.findIndex(command => command.includes('ps') && !command.includes('objectstore'));
    const storage = commands.findIndex(command => command.includes('up') && command.includes('objectstore'));
    const init = commands.findIndex(command => command.includes('/opt/grasp-init-execution.mjs'));
    assert.ok(stopped >= 0 && storage > stopped && init > storage);
    const identity = path.join(f.dir, mode === 'dev' ? '.devdata' : '.localdata', 'objectstore', 'client.env');
    const bytes = fs.readFileSync(identity);
    assert.equal(fs.statSync(identity).mode & 0o077, 0);
    assert.equal(spawnSync('bash', ['start.sh', ...args], { cwd: f.dir, env, encoding: 'utf8' }).status, 0);
    assert.deepEqual(fs.readFileSync(identity), bytes);

    const busy = shellFixture();
    const refused = spawnSync('bash', ['start.sh', ...args], { cwd: busy.dir,
      env: { ...env, ...busy.env, TEST_CONTAINER: 'a'.repeat(64), TEST_STATE: 'true running' }, encoding: 'utf8' });
    assert.notEqual(refused.status, 0);
    assert.equal(busy.commands().some(command => command.includes('up') || command.includes('run')), false);
    assert.equal(fs.existsSync(path.join(busy.dir, mode === 'dev' ? '.devdata' : '.localdata', 'objectstore')), false);
  }
});

test('unknown or failed writer inspection cannot proceed to initialization', () => {
  for (const state of ['false restarting', 'true paused', 'false unknown', 'inspect-failed']) {
    const f = shellFixture();
    const result = spawnSync('bash', ['start.sh', 'init-execution', '--offline'], { cwd: f.dir,
      env: { ...f.env, TEST_CONTAINER: 'a'.repeat(64), TEST_STATE: state }, encoding: 'utf8' });
    assert.notEqual(result.status, 0);
    assert.equal(f.commands().some(args => args.includes('run') || args.includes('down')), false);
  }
  const f = shellFixture();
  assert.notEqual(spawnSync('bash', ['start.sh', 'init-execution', '--offline'], { cwd: f.dir,
    env: { ...f.env, TEST_PS_FAIL: '1' } }).status, 0);
  assert.equal(f.commands().some(args => args.includes('run')), false);
});

test('release smoke initializes before up and never deletes or stops an existing installation', () => {
  for (const existing of [false, true]) {
    const f = shellFixture();
    if (existing) {
      fs.mkdirSync(path.join(f.dir, '.localdata'));
      fs.writeFileSync(path.join(f.dir, '.localdata', 'original'), 'retained user bytes');
    }
    const image = 'example.invalid/image@sha256:' + 'a'.repeat(64);
    const result = spawnSync('sh', ['release-smoke.sh'], { cwd: f.dir, encoding: 'utf8',
      env: { ...f.env, GRASP_IMAGE: image, SANDBOX_GATEWAY_IMAGE: image, SANDBOX_IMAGE: image } });
    const commands = f.commands();
    if (existing) {
      assert.notEqual(result.status, 0);
      assert.equal(commands.length, 0);
      assert.equal(fs.readFileSync(path.join(f.dir, '.localdata', 'original'), 'utf8'), 'retained user bytes');
    } else {
      assert.equal(result.status, 0, result.stderr);
      const init = commands.findIndex(args => args.includes('/opt/grasp-init-execution.mjs'));
      const up = commands.findIndex(args => args.includes('up'));
      assert.ok(init >= 0 && up > init);
      assert.equal(fs.existsSync(path.join(f.dir, '.localdata')), false);
    }
  }
});
