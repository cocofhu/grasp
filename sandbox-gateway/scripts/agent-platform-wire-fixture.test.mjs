import assert from 'node:assert/strict'
import {mkdtempSync, readFileSync, rmSync, writeFileSync} from 'node:fs'
import http from 'node:http'
import {tmpdir} from 'node:os'
import path from 'node:path'
import {spawn} from 'node:child_process'
import {once} from 'node:events'
import {fileURLToPath} from 'node:url'
import {test} from 'node:test'
import {checkpointOperation, kimiConfiguration} from './agent-platform-wire-fixture.mjs'

const syntheticKey = 'synthetic-kimi-fixture-key'
const kimiConfig = (effort, model = 'k3.1-pro', wireModel = model) => `default_model = "${model}"
${effort === undefined ? '' : `[thinking]\nenabled = true\neffort = "${effort}"\n`}
[providers.kimi]
type = "kimi"
base_url = "https://api.kimi.ai/coding/v1"
api_key_env = "KIMI_CODE_API_KEY"
[models."${model}"]
provider = "kimi"
model = "${wireModel}"
capabilities = ["thinking"]
${wireModel === 'kimi-for-coding-highspeed' ? '' : `support_efforts = ["low", "high", "max"]\ndefault_effort = "${wireModel === 'kimi-for-coding' ? 'max' : 'high'}"`}
`

async function kimiWire(config, commands) {
  const root = mkdtempSync(path.join(tmpdir(), 'grasp-kimi-wire-'))
  const modelRequests = []
  const server = http.createServer(async (req, res) => {
    let raw = ''
    for await (const chunk of req) raw += chunk
    const body = JSON.parse(raw)
    let reply
    if (req.url === '/mcp') reply = {jsonrpc: '2.0', id: body.id, result: body.method === 'tools/list' ? {tools: []} : {}}
    else {
      modelRequests.push(body)
      reply = {choices: [{message: {role: 'assistant', content: 'fixture completion'}}]}
    }
    res.writeHead(200, {'content-type': 'application/json'})
    res.end(JSON.stringify(reply))
  })
  try {
    writeFileSync(path.join(root, 'config.toml'), config)
    server.listen(0, '127.0.0.1')
    await once(server, 'listening')
    const base = `http://127.0.0.1:${server.address().port}`
    const child = spawn(process.execPath, [fileURLToPath(new URL('./agent-platform-wire-fixture.mjs', import.meta.url)), 'kimi_code', 'acp'], {env: {PATH: process.env.PATH, KIMI_CODE_HOME: root, KIMI_CODE_API_KEY: syntheticKey, GRASP_CI_MOCK_URL: base}, stdio: ['pipe', 'pipe', 'pipe']})
    let output = '', errors = ''
    child.stdout.on('data', (chunk) => { output += chunk })
    child.stderr.on('data', (chunk) => { errors += chunk })
    child.stdin.end([
      {id: 1, method: 'initialize'},
      {id: 2, method: 'session/new', params: {mcpServers: [{name: 'artifact-store', url: base+'/mcp'}]}},
      ...commands,
    ].map((request) => JSON.stringify({jsonrpc: '2.0', ...request})).join('\n')+'\n')
    const [code] = await once(child, 'close')
    assert.equal(code, 0, errors)
    return {responses: output.trim().split('\n').map((line) => JSON.parse(line)), modelRequests}
  } finally {
    await new Promise((resolve) => server.close(resolve))
    rmSync(root, {recursive: true, force: true})
  }
}

test('platform wire fixtures perform real bounded checkpoint reads and appends', () => {
  const root = mkdtempSync(path.join(tmpdir(), 'grasp-platform-checkpoint-'))
  try {
    const first = 'task-0123456789abcdef01234567:step=0'
    const second = 'task-0123456789abcdef01234567:step=1'
    assert.equal(checkpointOperation(`printf "%s\\n" ${first} >> checkpoint.txt; cat checkpoint.txt`, root), first+'\n')
    assert.equal(checkpointOperation(`printf "%s\\n" ${second} >> checkpoint.txt; cat checkpoint.txt`, root), first+'\n'+second+'\n')
    assert.equal(checkpointOperation('cat checkpoint.txt', root), first+'\n'+second+'\n')
  } finally { rmSync(root, {recursive: true, force: true}) }
})

test('model-provided shell syntax and paths cannot execute in a platform fixture', () => {
  const root = mkdtempSync(path.join(tmpdir(), 'grasp-platform-reject-'))
  try {
    const marker = 'task-0123456789abcdef01234567:step=0'
    checkpointOperation(`printf "%s\\n" ${marker} >> checkpoint.txt; cat checkpoint.txt`, root)
    for (const command of ['cat /etc/passwd', 'cat checkpoint.txt; touch escaped', '$(touch escaped)', `printf "%s\\n" ${marker};touch escaped >> checkpoint.txt; cat checkpoint.txt`, `printf "%s\\n" ${marker} >> ../checkpoint.txt; cat checkpoint.txt`, null]) {
      assert.throws(() => checkpointOperation(command, root), /unsupported platform fixture/)
    }
    assert.equal(readFileSync(path.join(root, 'checkpoint.txt'), 'utf8'), marker+'\n')
  } finally { rmSync(root, {recursive: true, force: true}) }
})

test('Kimi fixture reads native thinking config and resets to the declared native default', () => {
  for (const effort of ['low', 'high', 'max']) assert.deepEqual(kimiConfiguration(kimiConfig(effort), syntheticKey), {model: 'k3.1-pro', wireModel: 'k3.1-pro', effort})
  assert.equal(kimiConfiguration(kimiConfig(undefined), syntheticKey).effort, 'high')
  assert.equal(kimiConfiguration(kimiConfig(undefined, 'kimi-for-coding'), syntheticKey).effort, 'max')
  assert.throws(() => kimiConfiguration(kimiConfig('medium'), syntheticKey), /invalid native Kimi thinking effort/)
  assert.throws(() => kimiConfiguration(kimiConfig('high', 'kimi-for-coding-highspeed'), syntheticKey), /invalid native Kimi thinking effort/)
  assert.equal(kimiConfiguration(kimiConfig(undefined, 'kimi-for-coding-highspeed'), syntheticKey).effort, '')
  assert.throws(() => kimiConfiguration(kimiConfig(undefined, 'kimi-for-coding-highspeed').replace('"thinking"', '"image_in"'), syntheticKey), /thinking capabilities/)
  assert.throws(() => kimiConfiguration(kimiConfig('high').replace('"thinking"', '"image_in"'), syntheticKey), /thinking capabilities/)
  assert.deepEqual(kimiConfiguration(kimiConfig('max', 'kimi-k3', 'k3'), syntheticKey), {model: 'kimi-k3', wireModel: 'k3', effort: 'max'})
  assert.throws(() => kimiConfiguration(kimiConfig('max', 'kimi-k3'), syntheticKey), /exact native wire ID/)
  assert.equal(kimiConfiguration(kimiConfig('low', 'kimi-for-coding'), syntheticKey).effort, 'low')
  assert.throws(() => kimiConfiguration(kimiConfig('high').replace('https://api.kimi.ai/coding/v1', 'https://api.moonshot.ai/v1'), syntheticKey), /subscription endpoint/)
  assert.equal(kimiConfiguration(kimiConfig('high').replace('https://api.kimi.ai/coding/v1', 'https://api.kimi.com/coding/v1'), syntheticKey).effort, 'high')
})

test('local Kimi fixture requires saved login instead of a host environment key', () => {
  const previous = process.env.GRASP_LOCAL_AUTH
  try {
    process.env.GRASP_LOCAL_AUTH = '1'
    const config = kimiConfig('max').replace('api_key_env = "KIMI_CODE_API_KEY"', 'api_key = "synthetic-saved-local-key"')
    assert.equal(kimiConfiguration(config, '').effort, 'max')
    assert.throws(() => kimiConfiguration(kimiConfig('max'), ''), /saved login authentication/)
  } finally {
    if (previous === undefined) delete process.env.GRASP_LOCAL_AUTH
    else process.env.GRASP_LOCAL_AUTH = previous
  }
})

test('Kimi native ACP thinking selections reach each actual fixture model request', async () => {
  const commands = []
  for (const [index, effort] of ['low', 'high', 'max'].entries()) {
    commands.push({id: 3+index*2, method: 'session/set_config_option', params: {configId: 'thinking', value: effort}})
    commands.push({id: 4+index*2, method: 'session/prompt', params: {prompt: [{type: 'text', text: 'test native thinking option'}]}})
  }
  const {responses, modelRequests} = await kimiWire(kimiConfig('max'), commands)
  assert.equal(responses.some((response) => response.error), false)
  assert.deepEqual(modelRequests.map((request) => request.thinking), ['low', 'high', 'max'].map((effort) => ({type: 'enabled', effort, keep: 'all'})))
  assert.ok(modelRequests.every((request) => request.model === 'k3.1-pro' && request.reasoning_effort === undefined))
})

test('Kimi native ACP rejects unsupported levels and unsupported-model effort selection', async () => {
  const {responses, modelRequests} = await kimiWire(kimiConfig(undefined), [
    {id: 3, method: 'session/set_config_option', params: {configId: 'thinking', value: 'medium'}},
    {id: 4, method: 'session/prompt', params: {prompt: [{type: 'text', text: 'use native default'}]}},
    {id: 5, method: 'session/set_config_option', params: {configId: 'model', value: 'kimi-for-coding-highspeed'}},
    {id: 6, method: 'session/set_config_option', params: {configId: 'thinking', value: 'high'}},
    {id: 7, method: 'session/prompt', params: {prompt: [{type: 'text', text: 'unsupported model has no effort'}]}},
    {id: 8, method: 'session/set_config_option', params: {configId: 'thinking', value: 'off'}},
    {id: 9, method: 'session/prompt', params: {prompt: [{type: 'text', text: 'boolean thinking disabled'}]}},
    {id: 10, method: 'session/set_config_option', params: {configId: 'thinking', value: 'on'}},
    {id: 11, method: 'session/prompt', params: {prompt: [{type: 'text', text: 'boolean thinking enabled'}]}},
  ])
  assert.equal(responses.find((response) => response.id === 3).error.code, -32603)
  assert.equal(responses.find((response) => response.id === 6).error.code, -32603)
  const highspeedOption = responses.find((response) => response.id === 5).result.configOptions.find((option) => option.id === 'thinking')
  assert.deepEqual(highspeedOption.options.map((option) => option.value), ['off', 'on'])
  assert.equal(highspeedOption.currentValue, 'on')
  assert.deepEqual(modelRequests.map((request) => request.thinking.effort), ['high', undefined, undefined, undefined])
  assert.deepEqual(modelRequests.map((request) => request.thinking.type), ['enabled', 'enabled', 'disabled', 'enabled'])
})

test('Kimi K2.8 keeps its native max default after an implicit model reset', async () => {
  const {responses, modelRequests} = await kimiWire(kimiConfig(undefined), [
    {id: 3, method: 'session/set_config_option', params: {configId: 'model', value: 'kimi-for-coding'}},
    {id: 4, method: 'session/prompt', params: {prompt: [{type: 'text', text: 'use K2.8 native default'}]}},
  ])
  assert.equal(responses.some((response) => response.error), false)
  assert.equal(modelRequests[0].thinking.effort, 'max')
})

test('Kimi saved K3 alias remains selectable and reaches the canonical native model', async () => {
  const {responses, modelRequests} = await kimiWire(kimiConfig(undefined, 'kimi-k3', 'k3'), [
    {id: 3, method: 'session/prompt', params: {prompt: [{type: 'text', text: 'use saved K3 alias'}]}},
  ])
  const modelOption = responses.find((response) => response.id === 2).result.configOptions.find((option) => option.id === 'model')
  assert.equal(modelOption.currentValue, 'kimi-k3')
  assert.ok(modelOption.options.some((option) => option.value === 'kimi-k3'))
  assert.equal(modelRequests[0].model, 'k3')
})
