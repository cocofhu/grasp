#!/usr/bin/env node
// CI double for CLIs that cannot be pointed at the localhost model without a
// vendor credential: Cursor, Trae, and Kimi Code. This is never a claim of
// vendor inference. The real Grasp server and runtime still load the selected
// config, negotiate their native CLI wire protocol, call the actual scoped MCP
// server, and execute commands issued by the localhost model fixture.
import fs from 'node:fs'
import path from 'node:path'
import readline from 'node:readline'
import {randomUUID} from 'node:crypto'
import {pathToFileURL} from 'node:url'

const provider = process.argv[2]
const args = process.argv.slice(3)
let selectedModel = args.includes('--model') ? args[args.indexOf('--model') + 1] : 'gpt-5'
let selectedEffort = ''
let selectedWireModel = ''
let selectedThinking = true
const sid = randomUUID()
const send = (value) => process.stdout.write(`${JSON.stringify(value)}\n`)
let mcp, mcpSession, messages = []

// These proprietary-platform doubles exercise only the matrix's fixed
// checkpoint operations. Model output is data, never a host shell command.
// Configurable native CLIs separately exercise their real command tools.
export function checkpointOperation(command, cwd = process.cwd()) {
  const file = path.join(cwd, 'checkpoint.txt')
  if (command === 'cat checkpoint.txt') return fs.readFileSync(file, 'utf8')
  const append = typeof command === 'string' && command.match(/^printf "%s\\n" (task-[a-f0-9]{24}:step=(?:0|[1-9][0-9]{0,2})) >> checkpoint\.txt; cat checkpoint\.txt$/)
  if (!append) throw new Error('unsupported platform fixture checkpoint operation')
  fs.appendFileSync(file, append[1]+'\n', {encoding: 'utf8', mode: 0o600})
  return fs.readFileSync(file, 'utf8')
}

const kimiEfforts = ['low', 'high', 'max']
const kimiModels = ['k3.1-pro', 'k3.1-pro-highspeed', 'k3', 'k3-256k', 'kimi-for-coding', 'kimi-for-coding-highspeed']
const kimiAliases = {'kimi-k3.1-pro': 'k3.1-pro', 'kimi-k3.1-pro-highspeed': 'k3.1-pro-highspeed', 'kimi-k3': 'k3'}
const kimiWireModel = (model) => kimiAliases[model] || model
const kimiSupportsEffort = (model) => kimiModels.slice(0, -1).includes(kimiWireModel(model))
const kimiDefaultEffort = (model) => kimiWireModel(model) === 'kimi-for-coding' ? 'max' : 'high'

// Read only Grasp's generated TOML fields. This deliberately consumes the
// native CLI config, never Grasp's desired session-effort environment variable.
export function kimiConfiguration(config, secret) {
  if (!secret && process.env.GRASP_LOCAL_AUTH !== '1') throw new Error('KIMI_CODE_API_KEY was not set')
  if (!config.includes('api_key_env = "KIMI_CODE_API_KEY"') || (secret && config.includes(secret))) throw new Error('kimi config.toml must name the key variable and must not contain the key')
  if (!/base_url = "https:\/\/api\.kimi\.(?:com|ai)\/coding\/v1"/.test(config)) throw new Error('kimi config.toml site is not an official Kimi Code subscription endpoint')
  const tables = new Map([['', {}]])
  let table = tables.get('')
  for (const raw of config.split('\n')) {
    const line = raw.trim()
    const section = line.match(/^\[(.+)\]$/)
    if (section) {
      table = {}
      tables.set(section[1], table)
    } else {
      const field = line.match(/^([a-z_]+)\s*=\s*(.+)$/)
      if (field) table[field[1]] = JSON.parse(field[2])
    }
  }
  const model = tables.get('').default_model
  if (!kimiModels.includes(kimiWireModel(model))) throw new Error('unknown fixture Kimi model')
  const nativeModel = tables.get(`models."${model}"`) || tables.get(`models.${model}`) || {}
  if (nativeModel.model !== kimiWireModel(model)) throw new Error('Kimi model alias did not map to its exact native wire ID')
  if (!nativeModel.capabilities?.includes('thinking')) throw new Error('Kimi model thinking capabilities were not written to native config')
  const supportsEffort = kimiSupportsEffort(model)
  if (supportsEffort && (JSON.stringify(nativeModel.support_efforts) !== JSON.stringify(kimiEfforts) || nativeModel.default_effort !== kimiDefaultEffort(model))) throw new Error('Kimi model thinking capabilities were not written to native config')
  const thinking = tables.get('thinking')
  if (thinking && (!supportsEffort || thinking.enabled !== true || !kimiEfforts.includes(thinking.effort))) throw new Error('invalid native Kimi thinking effort')
  return {model, wireModel: nativeModel.model, effort: supportsEffort ? thinking?.effort || nativeModel.default_effort : ''}
}

async function rpc(method, params) {
  if (!mcp?.url) throw new Error('selected artifact-store MCP configuration is missing')
  const headers = Array.isArray(mcp.headers) ? Object.fromEntries(mcp.headers.map(({name, value}) => [name, value])) : mcp.headers || {}
  const response = await fetch(mcp.url, {method: 'POST', headers: {...headers, 'content-type': 'application/json', accept: 'application/json, text/event-stream', ...(mcpSession ? {'mcp-session-id': mcpSession} : {})}, body: JSON.stringify({jsonrpc: '2.0', id: randomUUID(), method, params})})
  mcpSession = response.headers.get('mcp-session-id') || mcpSession
  const text = await response.text()
  if (!response.ok) throw new Error(`MCP ${method} returned HTTP ${response.status}`)
  const data = response.headers.get('content-type')?.includes('text/event-stream') ? JSON.parse(text.split('\n').find((line) => line.startsWith('data: '))?.slice(6) || '{}') : JSON.parse(text)
  if (data.error) throw new Error(`MCP ${method} failed: ${data.error.code}`)
  return data.result
}

async function run(prompt, notify) {
  await rpc('initialize', {protocolVersion: '2024-11-05', capabilities: {}, clientInfo: {name: 'grasp-platform-wire-fixture', version: '1'}})
  const catalog = await rpc('tools/list', {})
  const tools = (catalog.tools || []).map((tool) => ({type: 'function', function: {name: `mcp__artifact-store__${tool.name}`, description: tool.description, parameters: tool.inputSchema}}))
  tools.push({type: 'function', function: {name: 'exec_command', parameters: {type: 'object', properties: {cmd: {type: 'string'}}, required: ['cmd']}}})
  messages.push({role: 'user', content: prompt})
  for (let turn = 0; turn < 20; turn++) {
    const thinking = provider === 'kimi_code' ? {thinking: {type: selectedThinking ? 'enabled' : 'disabled', ...(selectedThinking && selectedEffort ? {effort: selectedEffort} : {}), keep: 'all'}} : {}
    const response = await fetch(`${process.env.GRASP_CI_MOCK_URL}/v1/chat/completions`, {method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify({model: selectedWireModel || selectedModel, messages, tools, stream: false, ...thinking})})
    const body = await response.json()
    if (!response.ok) throw new Error(`mock model rejected the fixture request: ${JSON.stringify(body)}`)
    const message = body.choices?.[0]?.message
    if (!message) throw new Error('model response has no assistant message')
    messages.push(message)
    if (!message.tool_calls?.length) return message.content || ''
    for (const call of message.tool_calls) {
      const input = JSON.parse(call.function.arguments)
      notify({sessionUpdate: 'tool_call', toolCallId: call.id, title: call.function.name, status: 'in_progress', rawInput: input})
      let result
      if (call.function.name.startsWith('mcp__artifact-store__')) {
        result = await rpc('tools/call', {name: call.function.name.slice('mcp__artifact-store__'.length), arguments: input})
        if (result?.isError) throw new Error('the scoped MCP tool returned an error')
      } else if (call.function.name === 'exec_command') {
        result = checkpointOperation(input.cmd)
      } else throw new Error('fixture received an unadvertised tool')
      const text = typeof result === 'string' ? result : JSON.stringify(result)
      notify({sessionUpdate: 'tool_call_update', toolCallId: call.id, status: 'completed', content: [{type: 'content', content: {type: 'text', text}}]})
      messages.push({role: 'tool', tool_call_id: call.id, content: text})
    }
  }
  throw new Error('model/tool loop exceeded its bounded turn budget')
}

async function main() {
if (args.includes('--version')) {
  console.log(`grasp-${provider}-platform-wire-fixture 1`)
} else if (provider === 'cursor') {
  // Match the native CLI's actual config root so a broken worktree mapping
  // fails; CONFIG_ROOT alone is not a native Cursor setting.
  const root = process.env.CURSOR_CONFIG_DIR
  if (!root) throw new Error('native CURSOR_CONFIG_DIR was not set')
  mcp = JSON.parse(fs.readFileSync(path.join(root, 'mcp.json'), 'utf8')).mcpServers?.['artifact-store']
  let prompt = ''
  for await (const chunk of process.stdin) prompt += chunk
  send({type: 'system', subtype: 'init', session_id: sid})
  const calls = new Map()
  const reply = await run(prompt, (update) => {
    if (update.sessionUpdate === 'tool_call') calls.set(update.toolCallId, update)
    const start = calls.get(update.toolCallId)
    const isMcp = start.title.startsWith('mcp__artifact-store__')
    const key = isMcp ? 'mcpToolCall' : 'shellToolCall'
    const toolArgs = isMcp ? {toolName: start.title, args: start.rawInput} : {command: start.rawInput.cmd}
    const result = update.content ? {success: {content: update.content.map((entry) => ({type: 'text', text: entry.content.text}))}} : undefined
    send({type: 'tool_call', subtype: update.sessionUpdate === 'tool_call' ? 'started' : 'completed', call_id: update.toolCallId, tool_call: {[key]: {args: toolArgs, ...(result ? {result} : {})}}})
  })
  send({type: 'assistant', message: {content: [{type: 'text', text: reply}]}})
  send({type: 'result', subtype: 'success', result: reply, session_id: sid, usage: {input_tokens: 128, output_tokens: 32}})
} else if (provider === 'trae' || provider === 'kimi_code') {
  if (provider === 'kimi_code') {
    const root = process.env.KIMI_CODE_HOME
    if (!root) throw new Error('native KIMI_CODE_HOME was not set')
    const config = fs.readFileSync(path.join(root, 'config.toml'), 'utf8')
    const configured = kimiConfiguration(config, process.env.KIMI_CODE_API_KEY || '')
    selectedModel = configured.model
    selectedWireModel = configured.wireModel
    selectedEffort = configured.effort
  }
  const lines = readline.createInterface({input: process.stdin})
  const options = () => {
    const models = provider === 'kimi_code' ? [...kimiModels, ...Object.keys(kimiAliases)] : ['gpt-5', 'e2e-auxiliary']
    const result = [{id: 'model', category: 'model', currentValue: selectedModel, options: models.map((value) => ({value, name: value}))}]
    if (provider === 'kimi_code') {
      const efforts = kimiSupportsEffort(selectedModel) ? ['off', ...kimiEfforts] : ['off', 'on']
      result.push({id: 'thinking', category: 'thought_level', currentValue: selectedThinking ? selectedEffort || 'on' : 'off', options: efforts.map((value) => ({value, name: value}))})
    }
    return result
  }
  for await (const line of lines) {
    const request = JSON.parse(line)
    if (request.id === undefined) continue
    try {
      let result = {}
      if (request.method === 'initialize') result = {protocolVersion: 1, agentCapabilities: {mcpCapabilities: {http: true}}, agentInfo: {name: `grasp-${provider}-platform-wire-fixture`, version: '1'}}
      else if (request.method === 'session/new') {
        mcp = request.params.mcpServers.find((server) => server.name === 'artifact-store')
        result = {sessionId: sid, configOptions: options()}
      } else if (request.method === 'session/set_config_option') {
        const option = options().find((option) => option.id === request.params.configId)
        if (!option?.options.some((option) => option.value === request.params.value)) throw new Error('unknown native ACP config selection')
        if (request.params.configId === 'model') {
          selectedModel = request.params.value
          selectedWireModel = provider === 'kimi_code' ? kimiWireModel(selectedModel) : ''
          selectedEffort = provider === 'kimi_code' && kimiSupportsEffort(selectedModel) ? kimiDefaultEffort(selectedModel) : ''
          selectedThinking = true
        } else if (request.params.configId === 'thinking') {
          selectedEffort = kimiEfforts.includes(request.params.value) ? request.params.value : ''
          selectedThinking = request.params.value !== 'off'
        }
        result = {configOptions: options()}
      } else if (request.method === 'session/prompt') {
        const reply = await run(request.params.prompt.filter((part) => part.type === 'text').map((part) => part.text).join('\n'), (update) => send({jsonrpc: '2.0', method: 'session/update', params: {sessionId: sid, update}}))
        send({jsonrpc: '2.0', method: 'session/update', params: {sessionId: sid, update: {sessionUpdate: 'agent_message_chunk', content: {type: 'text', text: reply}}}})
        result = {stopReason: 'end_turn'}
      } else {
        send({jsonrpc: '2.0', id: request.id, error: {code: -32601, message: 'unsupported fixture ACP method'}})
        continue
      }
      send({jsonrpc: '2.0', id: request.id, result})
    } catch (error) {
      send({jsonrpc: '2.0', id: request.id, error: {code: -32603, message: error.message}})
    }
  }
} else throw new Error('only proprietary platform wire fixtures are permitted here')
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) await main()
