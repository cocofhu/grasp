#!/usr/bin/env node
// CI double for the proprietary Cursor/Trae CLIs, never a claim of vendor
// inference. The real Grasp server and runtime still load the selected config,
// negotiate their native CLI wire protocol, call the actual scoped MCP server,
// and execute commands issued by the localhost model fixture.
import fs from 'node:fs'
import path from 'node:path'
import readline from 'node:readline'
import {randomUUID} from 'node:crypto'
import {pathToFileURL} from 'node:url'

const provider = process.argv[2]
const args = process.argv.slice(3)
let selectedModel = args.includes('--model') ? args[args.indexOf('--model') + 1] : 'gpt-5'
const sid = randomUUID()
const send = (value) => process.stdout.write(`${JSON.stringify(value)}\n`)
let mcp, mcpSession, messages = []

// These two proprietary-platform doubles exercise only the matrix's fixed
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
    const response = await fetch(`${process.env.GRASP_CI_MOCK_URL}/v1/chat/completions`, {method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify({model: selectedModel, messages, tools, stream: false})})
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
} else if (provider === 'trae') {
  const lines = readline.createInterface({input: process.stdin})
  const options = () => [{id: 'model', category: 'model', currentValue: selectedModel, options: [{value: 'gpt-5', name: 'gpt-5'}, {value: 'e2e-auxiliary', name: 'e2e-auxiliary'}]}]
  for await (const line of lines) {
    const request = JSON.parse(line)
    if (request.id === undefined) continue
    try {
      let result = {}
      if (request.method === 'initialize') result = {protocolVersion: 1, agentCapabilities: {mcpCapabilities: {http: true}}, agentInfo: {name: 'grasp-trae-platform-wire-fixture', version: '1'}}
      else if (request.method === 'session/new') {
        mcp = request.params.mcpServers.find((server) => server.name === 'artifact-store')
        result = {sessionId: sid, configOptions: options()}
      } else if (request.method === 'session/set_config_option') {
        if (request.params.configId !== 'model' || !options()[0].options.some((option) => option.value === request.params.value)) throw new Error('unknown native ACP model selection')
        selectedModel = request.params.value
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
