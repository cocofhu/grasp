import assert from 'node:assert/strict'
import {copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync} from 'node:fs'
import {createInterface} from 'node:readline'
import http from 'node:http'
import {tmpdir} from 'node:os'
import path from 'node:path'
import {spawn} from 'node:child_process'
import {once} from 'node:events'
import {fileURLToPath} from 'node:url'
import {test} from 'node:test'
import {checkpointOperation, cursorSelection, kimiConfiguration} from './agent-platform-wire-fixture.mjs'

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
      {id: 2, method: 'session/new', params: {mcpServers: [{name: 'grasp-tool', url: base+'/mcp'}]}},
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

test('Cursor SDK stand-in accepts only offered variants and forwards their reasoning', () => {
  const pick = (reasoning, fast = 'false') => ({id: 'gpt-5.5', params: [{id: 'reasoning', value: reasoning}, {id: 'fast', value: fast}]})
  assert.deepEqual(cursorSelection(pick('extra-high', 'true')), {model: 'gpt-5.5', effort: 'xhigh', fast: true})
  assert.equal(cursorSelection(pick('none')).effort, '')
  assert.throws(() => cursorSelection(pick('max')), /not an offered variant/)
  assert.throws(() => cursorSelection({id: 'gpt-5.5'}), /not an offered variant/)
  assert.throws(() => cursorSelection({id: 'gpt-5.5-high'}), /unknown fixture Cursor model/)
})

async function cursorBridge(env, commands) {
  const child = spawn(process.execPath, [fileURLToPath(new URL('../sandbox/internal/provider/cursorsdk/bridge.mjs', import.meta.url))], {env, stdio: ['pipe', 'pipe', 'pipe']})
  let errors = ''
  child.stderr.on('data', (chunk) => { errors += chunk })
  const frames = []
  const waiters = []
  createInterface({input: child.stdout}).on('line', (line) => {
    const frame = JSON.parse(line)
    frames.push(frame)
    for (const waiter of waiters.splice(0)) waiter()
  })
  const until = async (match) => {
    while (!frames.some(match)) await new Promise((resolve) => waiters.push(resolve))
    return frames.find(match)
  }
  try {
    for (const [index, command] of commands.entries()) {
      child.stdin.write(JSON.stringify({id: index+1, ...command})+'\n')
      const response = await until((frame) => frame.type === 'response' && frame.id === index+1)
      assert.ok(response.ok, response.error+errors)
      if (command.type === 'send') await until((frame) => frame.type === 'run_end' && frame.runId === response.data.runId)
    }
  } finally {
    child.stdin.end()
    await once(child, 'close')
  }
  return frames
}

test('Cursor bridge drives the SDK stand-in: MCP, reasoning, usage and resume', async () => {
  const root = mkdtempSync(path.join(tmpdir(), 'grasp-cursor-sdk-'))
  const modelRequests = []
  const server = http.createServer(async (req, res) => {
    let raw = ''
    for await (const chunk of req) raw += chunk
    const body = JSON.parse(raw)
    let reply
    if (req.url === '/mcp') {
      assert.equal(req.headers.authorization, 'Bearer scoped')
      reply = {jsonrpc: '2.0', id: body.id, result: body.method === 'tools/list' ? {tools: [{name: 'get_history', inputSchema: {type: 'object'}}]} : body.method === 'tools/call' ? {content: [{type: 'text', text: 'history page'}]} : {}}
    } else {
      modelRequests.push(body)
      const called = body.messages.some((message) => message.role === 'tool')
      reply = {choices: [{message: called || modelRequests.length > 2 ? {role: 'assistant', content: 'fixture completion'} : {role: 'assistant', content: '', tool_calls: [{id: 'call-1', type: 'function', function: {name: 'mcp__grasp-tool__get_history', arguments: '{}'}}]}}]}
    }
    res.writeHead(200, {'content-type': 'application/json'})
    res.end(JSON.stringify(reply))
  })
  try {
    const pkg = path.join(root, 'sdk', 'node_modules', '@cursor', 'sdk')
    mkdirSync(pkg, {recursive: true})
    copyFileSync(fileURLToPath(new URL('./agent-cursor-sdk-fixture.cjs', import.meta.url)), path.join(pkg, 'index.js'))
    writeFileSync(path.join(pkg, 'package.json'), '{"name":"@cursor/sdk","main":"index.js"}')
    writeFileSync(path.join(root, 'sdk', 'package.json'), '{}')
    server.listen(0, '127.0.0.1')
    await once(server, 'listening')
    const base = `http://127.0.0.1:${server.address().port}`
    const env = {PATH: process.env.PATH, GRASP_CURSOR_SDK_DIR: path.join(root, 'sdk'), GRASP_CURSOR_SDK_FIXTURE: new URL('./agent-platform-wire-fixture.mjs', import.meta.url).href, GRASP_CURSOR_FIXTURE_STORE: path.join(root, 'store'), GRASP_CI_MOCK_URL: base}
    const open = (effort, agentId) => ({type: 'open', cwd: root, apiKey: 'synthetic', ...(agentId ? {agentId} : {}), model: {id: 'gpt-5.5', params: [{id: 'reasoning', value: effort}, {id: 'fast', value: 'false'}]}, mcpServers: {'grasp-tool': {type: 'http', url: base+'/mcp', headers: {Authorization: 'Bearer scoped'}}}})
    const first = await cursorBridge(env, [{type: 'models', apiKey: 'synthetic'}, open('high'), {type: 'send', text: 'remember the nonce'}])
    assert.equal(first.find((frame) => frame.id === 1).data[0].id, 'gpt-5.5')
    const agentId = first.find((frame) => frame.id === 2).data.agentId
    const updates = first.filter((frame) => frame.type === 'delta').map((frame) => frame.update)
    assert.deepEqual(updates.map((update) => update.type), ['tool-call-started', 'tool-call-completed', 'text-delta'])
    assert.equal(updates[0].toolCall.args.toolName, 'get_history')
    assert.equal(updates[1].toolCall.result.value.content[0].text, '{"content":[{"type":"text","text":"history page"}]}')
    assert.deepEqual(first.find((frame) => frame.type === 'run_end').usage, {inputTokens: 128, outputTokens: 32, cacheReadTokens: 0, cacheWriteTokens: 0})

    const second = await cursorBridge(env, [open('extra-high', agentId), {type: 'send', text: 'continue'}])
    assert.deepEqual(second.find((frame) => frame.id === 1).data, {agentId, resumed: true})
    assert.equal(second.find((frame) => frame.type === 'run_end').status, 'finished')
    assert.deepEqual(modelRequests.map((request) => [request.model, request.reasoning_effort]), [['gpt-5.5', 'high'], ['gpt-5.5', 'high'], ['gpt-5.5', 'xhigh']])
    assert.ok(modelRequests[2].messages.some((message) => message.content === 'remember the nonce'))

    const lost = await cursorBridge(env, [open('high', 'agent-unknown')])
    assert.match(lost.find((frame) => frame.id === 1).data.resumeError, /was not found/)
  } finally {
    await new Promise((resolve) => server.close(resolve))
    rmSync(root, {recursive: true, force: true})
  }
})
