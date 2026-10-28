#!/usr/bin/env node
// Deterministic, vendor-free model endpoint for real CLI integration tests.
// This service never reads API keys or makes outbound requests. Configure it
// through POST /__mock/scenarios, then put [GRASP_MOCK run=ID turn=ID] at the
// beginning of each current user prompt. Keep context anchors out of subsequent
// prompts: assertions inspect the actual history and native tool results.

import {createHash} from 'node:crypto'
import fs from 'node:fs'
import http from 'node:http'
import {pathToFileURL} from 'node:url'

const MAX_BODY = 4_000_000
const MAX_REPORT = 10_000
const TOKEN_USAGE = {input: 128, output: 32}
const markerPattern = /\[GRASP_MOCK run=([\w.-]+) turn=([\w.-]+)\]/g
const hash = (text) => createHash('sha256').update(text).digest('hex')
const identifier = (value) => String(value ?? '').split(/[?#]/)[0].replace(/\/\/[^/@]*@/g, '//').slice(0, 180)

function json(res, status, body) {
  const data = JSON.stringify(body)
  res.writeHead(status, {'content-type': 'application/json', 'content-length': Buffer.byteLength(data)})
  res.end(data)
}

async function readJSON(req) {
  const chunks = []
  let size = 0
  for await (const chunk of req) {
    size += chunk.length
    if (size > MAX_BODY) throw Object.assign(new Error('request body too large'), {status: 413})
    chunks.push(chunk)
  }
  try {
    return JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}')
  } catch {
    throw Object.assign(new Error('invalid JSON'), {status: 400})
  }
}

function textOf(value) {
  if (typeof value === 'string') return value
  if (Array.isArray(value)) return value.map(textOf).filter(Boolean).join('\n')
  if (!value || typeof value !== 'object') return ''
  if (['image', 'image_url', 'input_image'].includes(value.type)) return ''
  if (typeof value.text === 'string') return value.text
  if ('content' in value) return textOf(value.content)
  return JSON.stringify(value)
}

// Preserve native call/result identities. A user-role Anthropic tool_result is
// not another instruction, and inline image bytes are never context evidence.
export function normalizeRequest(protocol, body) {
  const items = []
  const add = (item) => items.push({...item, position: items.length})
  const message = (entry) => {
    const role = entry.role || 'user'
    if (role === 'tool') {
      add({kind: 'result', id: entry.tool_call_id, text: textOf(entry.content), parts: entry.content, isError: Boolean(entry.is_error)})
      return
    }
    const content = Array.isArray(entry.content) ? entry.content : [{type: 'text', text: textOf(entry.content)}]
    for (const block of content) {
      if (block.type === 'tool_use') {
        add({kind: 'call', id: block.id, name: block.name, arguments: block.input || {}})
      } else if (block.type === 'tool_result') {
        add({kind: 'result', id: block.tool_use_id, text: textOf(block.content), parts: block.content, isError: Boolean(block.is_error)})
      } else {
        const text = textOf(block)
        if (text) add({kind: 'text', role, text})
      }
    }
    for (const call of entry.tool_calls || []) {
      add({kind: 'call', id: call.id, name: call.function?.name, arguments: parseArguments(call.function?.arguments)})
    }
  }
  if (protocol === 'responses') {
    if (body.instructions) add({kind: 'text', role: 'system', text: textOf(body.instructions)})
    const input = typeof body.input === 'string' ? [{role: 'user', content: body.input}] : body.input || []
    for (const entry of input) {
      if (entry.type === 'function_call' || entry.type === 'custom_tool_call') {
        add({kind: 'call', id: entry.call_id || entry.id, name: entry.namespace ? `${entry.namespace}.${entry.name}` : entry.name, arguments: parseArguments(entry.arguments ?? entry.input)})
      } else if (entry.type === 'function_call_output' || entry.type === 'custom_tool_call_output') {
        add({kind: 'result', id: entry.call_id, text: textOf(entry.output), parts: entry.output, isError: Boolean(entry.is_error)})
      } else if (entry.role) message(entry)
    }
  } else {
    if (body.system) add({kind: 'text', role: 'system', text: textOf(body.system)})
    for (const entry of body.messages || []) message(entry)
  }
  const tools = []
  const visitTools = (list, prefix = '') => {
    for (const tool of list || []) {
      if (tool.type === 'namespace' && Array.isArray(tool.tools)) {
        visitTools(tool.tools, `${prefix}${tool.name}.`)
        continue
      }
      const fn = tool.function || tool
      if (typeof fn.name !== 'string') continue
      tools.push({name: prefix + fn.name, wireName: fn.name, namespace: prefix ? prefix.slice(0, -1) : undefined, schema: fn.parameters || fn.input_schema || {}, type: tool.type || 'function'})
    }
  }
  visitTools(body.tools)
  return {items, tools}
}

function parseArguments(raw) {
  if (typeof raw !== 'string') return raw || {}
  try { return JSON.parse(raw) } catch { return raw }
}

function locateMarker(items) {
  let found
  for (const item of items) {
    if (item.kind !== 'text' || item.role !== 'user') continue
    for (const match of item.text.matchAll(markerPattern)) {
      found = {run: match[1], turn: match[2], position: item.position, offset: match.index, length: match[0].length}
    }
  }
  return found
}

function sourceText(items, marker, source) {
  if (source === 'tool_results') return items.filter((item) => item.kind === 'result' && item.position > marker.position).map((item) => item.text).join('\n')
  const current = items[marker.position]
  if (source === 'current_user') return current.text.slice(marker.offset + marker.length)
  return items.filter((item) => item.position < marker.position && item.kind === 'text').map((item) => item.text)
    .concat(current.text.slice(0, marker.offset)).join('\n')
}

function validateScenario(scenario) {
  const validID = (value) => typeof value === 'string' && /^[\w.-]{1,100}$/.test(value)
  if (!validID(scenario.id) || !Array.isArray(scenario.turns) || scenario.turns.length < 1 || scenario.turns.length > 200) throw new Error('scenario requires id and 1..200 turns')
  const ids = new Set()
  for (const turn of scenario.turns) {
    if (!validID(turn.id) || ids.has(turn.id) || typeof turn.reply !== 'string') throw new Error('turn requires unique id and reply')
    ids.add(turn.id)
    for (const assertion of turn.assert || []) {
      if (!['current_user', 'prior_context', 'tool_results'].includes(assertion.source) || typeof assertion.contains !== 'string' || !assertion.contains) throw new Error('invalid context assertion')
    }
    if (!Array.isArray(turn.actions || []) || (turn.actions || []).length > 20) throw new Error('too many actions')
    for (const action of turn.actions || []) {
      if (!['history', 'shell', 'tool'].includes(action.kind)) throw new Error('unsupported action kind')
      if (action.kind === 'shell' && typeof action.command !== 'string') throw new Error('shell action requires command')
      if (action.kind === 'tool' && typeof action.name !== 'string') throw new Error('tool action requires exact name')
      if (action.expect && (!Array.isArray(action.expect) || action.expect.some((value) => typeof value !== 'string' || !value))) throw new Error('invalid tool result expectation')
      if (action.paginate !== undefined && (action.kind !== 'history' || typeof action.paginate !== 'boolean')) throw new Error('pagination is only supported for history actions')
      if (action.maxPages !== undefined && (!Number.isInteger(action.maxPages) || action.maxPages < 1 || action.maxPages > 25)) throw new Error('maxPages must be between 1 and 25')
      if (action.expectRole !== undefined && (action.kind !== 'history' || !['user', 'assistant'].includes(action.expectRole))) throw new Error('history expectRole must be user or assistant')
    }
  }
  return structuredClone(scenario)
}

const isHistory = (name) => /(?:^|[_\-.])get_history$/i.test(name || '')
const isToolSearch = (name) => /(?:^|\.)ToolSearch$/i.test(name || '')
const isDeferred = (name) => /(?:^|\.)DeferExecuteTool$/i.test(name || '')

function nativeCall(tool, id, args) {
  return {id, name: tool.name, wireName: tool.wireName, namespace: tool.namespace, arguments: args}
}

function resolveAction(action, tools, items, callID) {
  if (action.kind === 'tool') {
    const tool = tools.find((candidate) => candidate.name === action.name)
    if (!tool) throw new Error('required explicit tool is unavailable')
    if (tool.type === 'custom') throw new Error('custom grammar tools require an explicit supported adapter')
    return nativeCall(tool, callID, action.arguments || {})
  }
  if (action.kind === 'history') {
    const direct = tools.find((tool) => isHistory(tool.name))
    const args = action.arguments || {view: 'messages', limit: 50}
    if (direct) return nativeCall(direct, callID, args)
    const discoveryID = `${callID}_discover`
    const discovery = items.find((item) => item.kind === 'result' && item.id === discoveryID)
    if (!discovery) {
      const search = tools.find((tool) => isToolSearch(tool.name))
      if (!search) throw new Error('native get_history tool is unavailable')
      const properties = search.schema.properties || {}
      const searchArgs = 'tool_names' in properties ? {tool_names: ['mcp__artifact-store__get_history']}
        : 'queries' in properties ? {queries: ['artifact-store get_history']}
          : 'query' in properties ? {query: '+artifact-store get_history'} : undefined
      if (!searchArgs) throw new Error('native tool discovery schema has no supported query field')
      if ('top_k' in properties) searchArgs.top_k = 5
      if ('max_results' in properties) searchArgs.max_results = 5
      for (const key of search.schema.required || []) if (!(key in searchArgs)) throw new Error('native tool discovery requires an explicit additional argument')
      return nativeCall(search, discoveryID, searchArgs)
    }
    if (discovery.isError || !/get_history/.test(discovery.text)) throw new Error('native history discovery did not return get_history')
    const deferred = tools.find((tool) => isDeferred(tool.name))
    if (!deferred) throw new Error('native deferred tool execution is unavailable')
    const discoveredName = discovery.text.match(/mcp__[\w.-]*__get_history/)?.[0] || 'mcp__artifact-store__get_history'
    return nativeCall(deferred, callID, {toolName: discoveredName, params: args})
  }
  const tool = tools.find((candidate) => /(?:^|\.)(?:bash|exec_command|shell|run_shell_command|execute_bash)$/i.test(candidate.name))
  if (!tool) throw new Error('native shell tool is unavailable')
  if (tool.type === 'custom') throw new Error('custom grammar shell requires an explicit supported adapter')
  const properties = tool.schema.properties || {}
  const commandKey = 'cmd' in properties ? 'cmd' : 'command' in properties ? 'command' : undefined
  if (!commandKey) throw new Error('native shell schema has no supported cmd or command field')
  const args = {[commandKey]: action.command, ...(action.arguments || {})}
  // Command tools occasionally require a descriptive label, never permission
  // overrides. Other required fields must be supplied explicitly by the runner.
  if ((tool.schema.required || []).includes('description') && !('description' in args)) args.description = 'Validate mock CI workspace checkpoint'
  for (const key of tool.schema.required || []) if (!(key in args)) throw new Error('native shell requires an explicit additional argument')
  return nativeCall(tool, callID, args)
}

// Native MCP clients wrap text in different JSON envelopes. Only descend
// through envelope keys; never mistake an older nested page inside a message's
// tool history for the current page. All traversal and JSON decoding is bounded.
function historyPage(text) {
  const find = (value, depth) => {
    if (depth > 10) throw new Error('history result wrapper exceeds depth limit')
    if (typeof value === 'string') {
      try { return find(JSON.parse(value), depth + 1) } catch (error) {
        if (error instanceof SyntaxError) return undefined
        throw error
      }
    }
    if (!value || typeof value !== 'object') return undefined
    if (Array.isArray(value)) {
      for (const entry of value) {
        const page = find(entry, depth + 1)
        if (page) return page
      }
      return undefined
    }
    if (value.isError === true || value.is_error === true || value.error) throw new Error('native history tool returned an error')
    if ('history_version' in value && 'messages' in value) return value
    for (const key of ['result', 'content', 'data', 'output', 'text']) {
      if (!(key in value)) continue
      const page = find(value[key], depth + 1)
      if (page) return page
    }
    return undefined
  }
  const page = find(text, 0)
  if (!page || typeof page.history_version !== 'string' || !page.history_version || !Array.isArray(page.messages) || typeof page.has_more !== 'boolean') throw new Error('native history result has no valid snapshot page')
  if (page.has_more && (typeof page.next_cursor !== 'string' || !page.next_cursor)) throw new Error('history page has_more requires next_cursor')
  if (!page.has_more && page.next_cursor) throw new Error('terminal history page has a stale next_cursor')
  return page
}

function pagedHistoryAction(action, tools, items, callID) {
  const cursors = new Set()
  let args = {...(action.arguments || {view: 'messages', limit: 50})}
  if (args.view && args.view !== 'messages') throw new Error('paginated history action requires messages view')
  args.view = 'messages'
  if (args.cursor) cursors.add(args.cursor)
  let version, conversation
  const evidence = []
  for (let index = 0; index < (action.maxPages || 10); index++) {
    const pageID = index === 0 ? callID : `${callID}_p${index}`
    const response = items.find((item) => item.kind === 'result' && item.id === pageID)
    if (!response) return {call: resolveAction({...action, arguments: args}, tools, items, pageID)}
    if (response.isError) throw new Error('native history tool returned an error')
    const page = historyPage(response.parts ?? response.text)
    if (index === 0) {
      version = page.history_version
      conversation = page.conversationId
    } else if (page.history_version !== version || page.conversationId !== conversation) throw new Error('history snapshot changed during pagination')
    if (page.has_more && cursors.has(page.next_cursor)) throw new Error('history pagination repeated a cursor')
    if (page.has_more) cursors.add(page.next_cursor)
    // Role-qualified checks inspect the direct message content only. Earlier
    // get_history tool output inside an assistant's toolResults cannot serve
    // as evidence that the original user requirement was actually retrieved.
    const messages = action.expectRole ? page.messages.filter((message) => message.role === action.expectRole) : page.messages
    evidence.push(messages.map((message) => textOf(message.content ?? message.text)).join('\n'))
    if ((action.expect || []).every((expected) => evidence.join('\n').includes(expected))) return {pages: index + 1}
    if (!page.has_more) throw new Error('history ended before required context was found')
    args = {...args, cursor: page.next_cursor}
  }
  throw new Error('history pagination exceeded maxPages')
}

function decide(normalized, state) {
  const {items, tools} = normalized
  const marker = locateMarker(items)
  // Real CLIs can request titles/compaction outside the user's marked turn.
  if (!marker) return {text: 'Mock auxiliary response.', auxiliary: true, assertions: []}
  const scenario = state.scenarios.get(marker.run)
  const turn = scenario?.turns.find((candidate) => candidate.id === marker.turn)
  const result = {run: marker.run, turn: marker.turn, assertions: []}
  if (!turn) return {...result, failure: 'unknown scenario or turn'}
  for (const [index, assertion] of (turn.assert || []).entries()) {
    if (assertion.source === 'tool_results') continue
    const passed = sourceText(items, marker, assertion.source).includes(assertion.contains)
    result.assertions.push({index, source: assertion.source, passed})
    if (!passed) return {...result, failure: `missing ${assertion.source} assertion ${index}`}
  }
  for (const [index, action] of (turn.actions || []).entries()) {
    // Keep native call IDs below providers' usual 64-character limit even
    // when descriptive scenario/turn IDs are long.
    const callID = `call_mock_${hash(`${marker.run}:${marker.turn}:${index}`).slice(0, 20)}`
    if (action.kind === 'history' && action.paginate) {
      try {
        const next = pagedHistoryAction(action, tools, items.filter((item) => item.position > marker.position), callID)
        if (next.call) return {...result, call: next.call}
        result.assertions.push({index, source: 'action_result', passed: true, pages: next.pages})
        continue
      } catch (error) {
        return {...result, failure: `${error.message} at action ${index}`}
      }
    }
    const response = items.find((item) => item.kind === 'result' && item.position > marker.position && item.id === callID)
    if (response) {
      if (response.isError || (action.expect || []).some((expected) => !response.text.includes(expected))) return {...result, failure: `tool result assertion failed at action ${index}`}
      result.assertions.push({index, source: 'action_result', passed: true})
      continue
    }
    try {
      return {...result, call: resolveAction(action, tools, items.filter((item) => item.position > marker.position), callID)}
    } catch (error) {
      return {...result, failure: `${error.message} at action ${index}`}
    }
  }
  for (const [index, assertion] of (turn.assert || []).entries()) {
    if (assertion.source !== 'tool_results') continue
    const passed = sourceText(items, marker, assertion.source).includes(assertion.contains)
    result.assertions.push({index, source: assertion.source, passed})
    if (!passed) return {...result, failure: `missing tool_results assertion ${index}`}
  }
  return {...result, text: turn.reply, completed: true}
}

function usageChat() {
  return {prompt_tokens: TOKEN_USAGE.input, completion_tokens: TOKEN_USAGE.output, total_tokens: TOKEN_USAGE.input + TOKEN_USAGE.output}
}

function sse(res) {
  res.writeHead(200, {'content-type': 'text/event-stream', 'cache-control': 'no-cache', connection: 'keep-alive'})
  return (payload, event) => res.write(`${event ? `event: ${event}\n` : ''}data: ${JSON.stringify(payload)}\n\n`)
}

function respondChat(res, body, choice, id) {
  const message = choice.call
    ? {role: 'assistant', content: null, tool_calls: [{id: choice.call.id, type: 'function', function: {name: choice.call.name, arguments: JSON.stringify(choice.call.arguments)}}]}
    : {role: 'assistant', content: choice.text}
  const finish = choice.call ? 'tool_calls' : 'stop'
  const base = {id: `chatcmpl_mock_${id}`, created: 1, model: body.model || 'mock-agent'}
  if (!body.stream) return json(res, 200, {...base, object: 'chat.completion', choices: [{index: 0, message, finish_reason: finish}], usage: usageChat()})
  const send = sse(res)
  const delta = choice.call
    ? {role: 'assistant', tool_calls: message.tool_calls.map((call) => ({index: 0, ...call}))}
    : message
  send({...base, object: 'chat.completion.chunk', choices: [{index: 0, delta, finish_reason: null}]})
  send({...base, object: 'chat.completion.chunk', choices: [{index: 0, delta: {}, finish_reason: finish}], usage: usageChat()})
  res.end('data: [DONE]\n\n')
}

function responseBody(body, choice, id) {
  const output = choice.call
    ? [{id: `fc_mock_${id}`, type: 'function_call', status: 'completed', call_id: choice.call.id, name: choice.call.wireName || choice.call.name, ...(choice.call.namespace ? {namespace: choice.call.namespace} : {}), arguments: JSON.stringify(choice.call.arguments)}]
    : [{id: `msg_mock_${id}`, type: 'message', status: 'completed', role: 'assistant', content: [{type: 'output_text', text: choice.text, annotations: [], logprobs: []}]}]
  return {id: `resp_mock_${id}`, object: 'response', created_at: 1, status: 'completed', error: null, incomplete_details: null, model: body.model || 'mock-agent', output, parallel_tool_calls: false, tool_choice: 'auto', tools: body.tools || [], usage: {input_tokens: TOKEN_USAGE.input, input_tokens_details: {cached_tokens: 0}, output_tokens: TOKEN_USAGE.output, output_tokens_details: {reasoning_tokens: 0}, total_tokens: TOKEN_USAGE.input + TOKEN_USAGE.output}}
}

function respondResponses(res, body, choice, id) {
  const response = responseBody(body, choice, id)
  if (!body.stream) return json(res, 200, response)
  const send = sse(res)
  let sequence = 0
  const event = (type, data) => send({type, sequence_number: sequence++, ...data}, type)
  event('response.created', {response: {...response, status: 'in_progress', output: [], usage: null}})
  event('response.in_progress', {response: {...response, status: 'in_progress', output: [], usage: null}})
  const item = response.output[0]
  const itemID = item.id
  event('response.output_item.added', {output_index: 0, item: {...item, status: 'in_progress', ...(choice.call ? {arguments: ''} : {content: []})}})
  if (choice.call) {
    event('response.function_call_arguments.delta', {item_id: itemID, output_index: 0, delta: item.arguments})
    event('response.function_call_arguments.done', {item_id: itemID, output_index: 0, arguments: item.arguments})
  } else {
    event('response.content_part.added', {item_id: itemID, output_index: 0, content_index: 0, part: {type: 'output_text', text: '', annotations: [], logprobs: []}})
    event('response.output_text.delta', {item_id: itemID, output_index: 0, content_index: 0, delta: choice.text, logprobs: []})
    event('response.output_text.done', {item_id: itemID, output_index: 0, content_index: 0, text: choice.text, logprobs: []})
    event('response.content_part.done', {item_id: itemID, output_index: 0, content_index: 0, part: item.content[0]})
  }
  event('response.output_item.done', {output_index: 0, item})
  event('response.completed', {response})
  res.end()
}

function respondMessages(res, body, choice, id) {
  const content = choice.call
    ? [{type: 'tool_use', id: choice.call.id, name: choice.call.name, input: choice.call.arguments}]
    : [{type: 'text', text: choice.text}]
  const usage = {input_tokens: TOKEN_USAGE.input, output_tokens: TOKEN_USAGE.output, cache_creation_input_tokens: 0, cache_read_input_tokens: 0}
  const message = {id: `msg_mock_${id}`, type: 'message', role: 'assistant', model: body.model || 'mock-agent', content, stop_reason: choice.call ? 'tool_use' : 'end_turn', stop_sequence: null, usage}
  if (!body.stream) return json(res, 200, message)
  const send = sse(res)
  const event = (type, data) => send({type, ...data}, type)
  event('message_start', {message: {...message, content: [], stop_reason: null, usage: {...usage, output_tokens: 0}}})
  event('content_block_start', {index: 0, content_block: choice.call ? {...content[0], input: {}} : {type: 'text', text: ''}})
  event('content_block_delta', {index: 0, delta: choice.call ? {type: 'input_json_delta', partial_json: JSON.stringify(choice.call.arguments)} : {type: 'text_delta', text: choice.text}})
  event('content_block_stop', {index: 0})
  event('message_delta', {delta: {stop_reason: message.stop_reason, stop_sequence: null}, usage: {output_tokens: TOKEN_USAGE.output}})
  event('message_stop', {})
  res.end()
}

function recordRequest(body, protocol, normalized, decision, sequence) {
  const scalar = (value) => ['string', 'number', 'boolean'].includes(typeof value) ? identifier(value) : undefined
  const reasoning = {}
  for (const [key, value] of Object.entries({reasoning_effort: body.reasoning_effort, effort: body.reasoning?.effort, thinking_type: body.thinking?.type, thinking_budget: body.thinking?.budget_tokens, output_effort: body.output_config?.effort})) {
    if (value !== undefined) reasoning[key] = scalar(value)
  }
  return {sequence, protocol, model: identifier(body.model), stream: Boolean(body.stream), reasoning,
    run: decision.run, turn: decision.turn, auxiliary: Boolean(decision.auxiliary), completed: Boolean(decision.completed), failure: decision.failure,
    assertions: decision.assertions, availableTools: normalized.tools.map((tool) => identifier(tool.name)),
    // Schema shapes aid native-CLI adapter failures without retaining tool
    // descriptions, argument values, prompts, credentials or API headers.
    toolSchemaShapes: normalized.tools.filter((tool) => isToolSearch(tool.name) || isDeferred(tool.name) || /DeferredToolPlaceholder$/.test(tool.name)).map((tool) => ({name: identifier(tool.name), required: tool.schema.required || [], properties: Object.fromEntries(Object.entries(tool.schema.properties || {}).map(([key, value]) => [key, {type: value.type, minimum: value.minimum, maximum: value.maximum}]))})),
    emittedTool: decision.call ? {id: decision.call.id, name: identifier(decision.call.name), namespace: decision.call.namespace, argumentKeys: Object.keys(decision.call.arguments)} : undefined,
    toolResults: normalized.items.filter((item) => item.kind === 'result').map((item) => ({id: identifier(item.id), isError: item.isError, bytes: Buffer.byteLength(item.text), sha256: hash(item.text)}))}
}

export async function createMockAgentServer(options = {}) {
  const state = {scenarios: new Map(), requests: [], sequence: 0}
  for (const scenario of options.scenarios || []) state.scenarios.set(scenario.id, validateScenario(scenario))
  const server = http.createServer(async (req, res) => {
    try {
      const path = new URL(req.url, 'http://localhost').pathname.replace(/\/$/, '') || '/'
      if (req.method === 'GET' && path === '/health') return json(res, 200, {ok: true, fixture: 'agent-switch', hits: state.sequence, scenarios: state.scenarios.size})
      if (req.method === 'GET' && path === '/__mock/report') return json(res, 200, {requests: state.requests, hits: state.sequence})
      if (req.method === 'POST' && path === '/__mock/scenarios') {
        const body = await readJSON(req)
        const scenarios = (Array.isArray(body) ? body : [body]).map(validateScenario)
        for (const scenario of scenarios) state.scenarios.set(scenario.id, scenario)
        return json(res, 201, {ids: scenarios.map((scenario) => scenario.id)})
      }
      if (req.method === 'GET' && /\/(?:v1\/)?models$/.test(path)) return json(res, 200, {object: 'list', data: (options.models || ['mock-agent', 'gpt-5', 'claude-sonnet-4-5']).map((id) => ({id, object: 'model', owned_by: 'mock-agent'}))})
      if (req.method !== 'POST') return json(res, 404, {error: {message: 'unknown mock endpoint'}})
      if (/\/(?:v1\/)?messages\/count_tokens$/.test(path)) {
        await readJSON(req)
        return json(res, 200, {input_tokens: TOKEN_USAGE.input})
      }
      const protocol = /\/chat\/completions$/.test(path) ? 'chat' : /\/responses$/.test(path) ? 'responses' : /\/messages$/.test(path) ? 'messages' : undefined
      if (!protocol) return json(res, 404, {error: {message: 'unknown mock endpoint'}})
      const body = await readJSON(req)
      const normalized = normalizeRequest(protocol, body)
      const decision = decide(normalized, state)
      const sequence = ++state.sequence
      state.requests.push(recordRequest(body, protocol, normalized, decision, sequence))
      if (state.requests.length > MAX_REPORT) state.requests.shift()
      if (options.reportFile) fs.writeFileSync(options.reportFile, JSON.stringify({requests: state.requests, hits: state.sequence}, null, 2))
      // Return an ordinary final model response on assertion failures so a real
      // CLI finishes and the runner can report evidence, rather than retrying.
      const choice = decision.failure ? {...decision, text: `GRASP_MOCK_FAILED: ${decision.failure}`} : decision
      if (protocol === 'chat') respondChat(res, body, choice, sequence)
      else if (protocol === 'responses') respondResponses(res, body, choice, sequence)
      else respondMessages(res, body, choice, sequence)
    } catch (error) {
      if (!res.headersSent) json(res, error.status || 400, {error: {message: error.message}})
      else res.end()
    }
  })
  await new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(options.port ?? 0, options.listen ?? '127.0.0.1', resolve)
  })
  const port = server.address().port
  return {port, url: `http://127.0.0.1:${port}`, report: () => structuredClone({requests: state.requests, hits: state.sequence}), close: () => new Promise((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))}
}

async function main() {
  const args = process.argv.slice(2)
  const options = {listen: '127.0.0.1', port: 0}
  for (let index = 0; index < args.length; index++) {
    const arg = args[index]
    if (arg === '--listen') options.listen = args[++index]
    else if (arg === '--port') options.port = Number(args[++index])
    else if (arg === '--report-file') options.reportFile = args[++index]
    else if (arg === '--scenario-file') options.scenarios = JSON.parse(fs.readFileSync(args[++index], 'utf8'))
    else if (arg === '--help') {
      process.stdout.write('usage: mock-agent-model.mjs [--listen HOST] [--port PORT] [--scenario-file JSON] [--report-file JSON]\n')
      return
    } else throw new Error('unknown argument')
  }
  if (!Number.isInteger(options.port) || options.port < 0 || options.port > 65535 || !options.listen) throw new Error('invalid listen address')
  if (options.scenarios && !Array.isArray(options.scenarios)) options.scenarios = [options.scenarios]
  const server = await createMockAgentServer(options)
  process.stdout.write(`MOCK_AGENT_PORT=${server.port}\n`)
  const stop = async () => { await server.close(); process.exit(0) }
  process.once('SIGTERM', stop)
  process.once('SIGINT', stop)
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => { process.stderr.write(`mock-agent-model: ${error.message}\n`); process.exitCode = 1 })
}
