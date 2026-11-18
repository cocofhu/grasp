#!/usr/bin/env node
// Runs inside the real-CLI E2E image against a local model endpoint. The
// declared catalog makes the native CLI's tier filtering part of the check.
import {strict as assert} from 'node:assert'
import {spawn} from 'node:child_process'
import {once} from 'node:events'
import {appendFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync} from 'node:fs'
import {createServer} from 'node:net'
import {tmpdir} from 'node:os'
import {join, resolve} from 'node:path'
import {setTimeout as delay} from 'node:timers/promises'
import {createMockAgentServer} from './mock-agent-model.mjs'

const runtime = resolve(process.argv[2])
const output = resolve(process.argv[3])
const primary = 'gpt-6.2-sol'
const standard = 'standard-fixture-model'
const labels = ['boot-standard', 'fast-high', 'fast-low', 'isolated-standard', 'standard-low', 'unsupported-target', 'restored-fast', 'restored-off']
const run = 'codex-fast-probe'
mkdirSync(output, {recursive: true})
// Native Codex creates private directories and files as the container user.
// Keep its home/session state out of the host's published artifact directory.
const scratch = mkdtempSync(join(tmpdir(), 'grasp-codex-fast-'))
const home = join(scratch, 'codex-home')
const cwd = join(scratch, 'workspace')
mkdirSync(home, {recursive: true})
mkdirSync(cwd, {recursive: true})

const catalogModel = (id, fast) => ({
  slug: id, display_name: id, description: 'Local integration fixture',
  supported_reasoning_levels: ['low', 'high'].map(effort => ({effort, description: effort})),
  default_reasoning_level: 'high', shell_type: 'unified_exec', visibility: 'list', supported_in_api: true, priority: 1,
  service_tiers: fast ? [{id: 'priority', name: 'Fast', description: 'Local Fast request fixture'}] : [],
  model_messages: {instructions_template: 'Return the exact response supplied by the local fixture.'},
  support_verbosity: false, truncation_policy: {mode: 'tokens', limit: 10000}, experimental_supported_tools: [], context_window: 500000,
})
const catalogPath = join(home, 'catalog.json')
writeFileSync(catalogPath, JSON.stringify({models: [catalogModel(primary, true), catalogModel(standard, false)]}))
// An inherited Fast default must not defeat an explicit session Standard choice.
writeFileSync(join(home, 'config.toml'), `model_catalog_json = ${JSON.stringify(catalogPath)}\nservice_tier = "fast"\n[features]\nfast_mode = true\n`)
const model = await createMockAgentServer({scenarios: [{id: run, turns: labels.map(id => ({id, reply: `FAST_PROBE_PASS_${id}`}))}]})
const portServer = createServer()
await new Promise(resolve => portServer.listen(0, '127.0.0.1', resolve))
const port = portServer.address().port
await new Promise(resolve => portServer.close(resolve))
const base = `http://127.0.0.1:${port}`
const cases = []
let backend

async function request(path, body) {
  const response = await fetch(base + path, {method: body === undefined ? 'GET' : 'POST', headers: {'content-type': 'application/json'}, body: body === undefined ? undefined : JSON.stringify(body)})
  const json = await response.json()
  assert.equal(response.ok, true, `${path}: ${response.status} ${JSON.stringify(json)}`)
  return json
}

async function waitFor(condition, label) {
  const deadline = Date.now() + 20000
  while (Date.now() < deadline) {
    if (backend.exitCode !== null || backend.signalCode !== null) throw new Error(`runtime exited during ${label}; see codex-fast-runtime.log`)
    try { if (await condition()) return } catch (error) { if (error.code !== 'ECONNREFUSED' && error.cause?.code !== 'ECONNREFUSED') throw error }
    await delay(100)
  }
  throw new Error(`timed out waiting for ${label}`)
}

const tier = fast => fast ? 'fast' : ''

async function start(restoredFast) {
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => ['PATH', 'HOME', 'LANG', 'LC_ALL', 'TMPDIR', 'TZ'].includes(key)))
  Object.assign(env, {AGENT_PROVIDER: 'codex', CONFIG_ROOT: home, CODEX_HOME: home,
    GRASP_MODEL_API_KEY: 'synthetic-local-fast-key', ACP_BRIDGE_MODEL: primary, ACP_BRIDGE_REASONING_EFFORT: 'high', ACP_BRIDGE_SERVICE_TIER: restoredFast ? 'fast' : 'default',
    GRASP_CONFIGURED_MODELS: JSON.stringify([{id: primary, reasoningEfforts: ['low', 'high'], serviceTiers: ['fast']}, {id: standard, reasoningEfforts: ['low', 'high']}]),
    GRASP_MODEL_CONNECTION: JSON.stringify({connectionId: 'fast-native-fixture', providerId: 'fixture', protocol: 'openai-responses', baseUrl: model.url + '/v1', auth: {scheme: 'bearer'}, selectedModelId: primary, models: [primary, standard].map(id => ({id, capabilities: {reasoning: true, toolCalls: true}}))}),
  })
  backend = spawn(join(runtime, 'bin/backend'), ['-listen', `127.0.0.1:${port}`, '-gin-mode', 'release', '-web', join(runtime, 'share/backend/web')], {cwd, env, stdio: ['ignore', 'pipe', 'pipe']})
  for (const stream of [backend.stdout, backend.stderr]) stream.on('data', data => appendFileSync(join(output, 'codex-fast-runtime.log'), data))
  await waitFor(async () => (await request('/api/models')).serviceTier === tier(restoredFast), 'runtime readiness and restored speed')
}

async function stop() {
  if (!backend || backend.exitCode !== null || backend.signalCode !== null) return
  const exited = once(backend, 'exit')
  backend.kill('SIGTERM')
  const timer = setTimeout(() => backend.kill('SIGKILL'), 8000)
  try { await exited } finally { clearTimeout(timer) }
}

async function turn(chat, label, fast, effort, expectedModel = primary) {
  const frames = []
  const ws = new WebSocket(base.replace('http:', 'ws:') + '/ws?chat=' + encodeURIComponent(chat))
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => { ws.close(); reject(new Error(`native CLI turn timed out: ${label}`)) }, 45000)
    let sent = false
    ws.onopen = () => ws.send(JSON.stringify({op: 'connect', cwd, fsRoot: cwd, autoPermission: true}))
    ws.onerror = () => { clearTimeout(timer); reject(new Error(`websocket failed: ${label}`)) }
    ws.onmessage = event => {
      try {
        const frame = JSON.parse(event.data)
        frames.push(frame)
        if (frame.op === 'error') throw new Error(frame.message)
        if (frame.op === 'connected' && !sent) {
          sent = true
          assert.equal(frame.serviceTier, tier(fast))
          ws.send(JSON.stringify({op: 'chat', opId: label, text: `[GRASP_MOCK run=${run} turn=${label}] Return the fixture response.`}))
        }
        if (frame.op === 'event' && frame.opId === label && frame.data?.type === 'prompt_done') {
          assert.equal(frame.data.stopReason, 'end_turn', JSON.stringify(frame.data))
          assert.ok(JSON.stringify(frames).includes(`FAST_PROBE_PASS_${label}`), 'native CLI reply missing')
          clearTimeout(timer); ws.close(); resolve()
        }
      } catch (error) { clearTimeout(timer); ws.close(); reject(error) }
    }
  })
  await waitFor(async () => !(await request('/api/runtime/activity')).busy, 'turn settlement')
  const requests = model.report().requests.filter(request => request.run === run && request.turn === label && !request.auxiliary)
  assert.ok(requests.length > 0, `${label}: no actual model request`)
  for (const request of requests) {
    assert.equal(request.model, expectedModel)
    assert.equal(request.serviceTier, fast ? 'priority' : undefined, `${label}: wrong service tier`)
    assert.equal(request.reasoning.effort, effort, `${label}: speed changed reasoning`)
  }
  cases.push({label, tier: tier(fast), reasoningEffort: effort, requestCount: requests.length, wireServiceTier: fast ? 'priority' : null})
}

try {
  await start(false)
  assert.deepEqual((await request('/api/models')).serviceTiers, ['fast'])
  await turn('default', 'boot-standard', false, 'high')
  assert.equal((await request('/api/model', {serviceTier: 'fast'})).reasoningEffort, 'high')
  await turn('default', 'fast-high', true, 'high')
  assert.equal((await request('/api/model', {reasoningEffort: 'low'})).serviceTier, 'fast')
  await turn('default', 'fast-low', true, 'low')
  const other = await request('/api/chats', {title: 'Independent Standard chat', model: primary})
  assert.equal((await request('/api/models?chat=' + other.id)).serviceTier, '')
  await turn(other.id, 'isolated-standard', false, 'high')
  assert.equal((await request('/api/model', {serviceTier: ''})).reasoningEffort, 'low')
  await turn('default', 'standard-low', false, 'low')
  await request('/api/model', {serviceTier: 'fast'})
  const changed = await request('/api/model', {model: standard})
  assert.equal(changed.serviceTier, '')
  assert.equal(changed.serviceTiers, null)
  await turn('default', 'unsupported-target', false, 'low', standard)
  const rejected = await fetch(base + '/api/model', {method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify({serviceTier: 'fast'})})
  assert.equal(rejected.status, 400)
  assert.equal((await rejected.json()).code, 'service_tier_unavailable')
  await stop()
  await start(true)
  await turn('default', 'restored-fast', true, 'high')
  await request('/api/model', {serviceTier: ''})
  await turn('default', 'restored-off', false, 'high')
  writeFileSync(join(output, 'codex-fast-report.json'), JSON.stringify({status: 'passed', mode: 'native-cli', cases, paidModelCalls: 0}, null, 2))
  process.stdout.write(JSON.stringify({codexFastMode: 'passed', cases: cases.length}) + '\n')
} finally {
  try {
    await stop()
    await model.close()
  } finally {
    rmSync(scratch, {recursive: true, force: true})
  }
}
