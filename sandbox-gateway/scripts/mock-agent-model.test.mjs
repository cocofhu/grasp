import {strict as assert} from 'node:assert'
import {spawn} from 'node:child_process'
import {after, before, describe, it} from 'node:test'
import {fileURLToPath} from 'node:url'

import {createMockAgentServer, normalizeRequest} from './mock-agent-model.mjs'

const nonce = 'original-context-4ac397'
const progress = 'checkpoint-built-81f2'
const marker = (turn) => `[GRASP_MOCK run=fixture turn=${turn}]`
const history = {name: 'mcp__artifact-store__get_history', parameters: {type: 'object', properties: {view: {type: 'string'}, limit: {type: 'integer'}}}}
const shell = {name: 'exec_command', parameters: {type: 'object', properties: {cmd: {type: 'string'}}, required: ['cmd']}}
const scenario = {
  id: 'fixture',
  turns: [
    {id: 'seed', assert: [{source: 'current_user', contains: nonce}], actions: [{kind: 'shell', command: `printf %s ${progress} > checkpoint.txt`, expect: ['wrote-checkpoint']}], reply: 'GRASP_SEED_OK'},
    {id: 'verify', assert: [{source: 'prior_context', contains: nonce}], actions: [
      {kind: 'history', arguments: {view: 'messages', limit: 50}, expect: [nonce]},
      {kind: 'shell', command: 'cat checkpoint.txt', expect: [progress]},
    ], reply: 'GRASP_CONTINUITY_OK'},
    {id: 'results', assert: [{source: 'tool_results', contains: 'retrieved-from-tool'}], actions: [{kind: 'history'}], reply: 'GRASP_RESULT_OK'},
  ],
}

function parseEvents(raw) {
  return raw.split('\n').filter((line) => line.startsWith('data: ') && line !== 'data: [DONE]').map((line) => JSON.parse(line.slice(6)))
}

function toolsFor(protocol, tools = [history, shell]) {
  return tools.map((tool) => protocol === 'chat' ? {type: 'function', function: tool} : protocol === 'messages' ? {name: tool.name, input_schema: tool.parameters} : {type: 'function', ...tool})
}

async function invoke(server, protocol, messages, extra = {}) {
  const body = {model: 'mock-agent', tools: toolsFor(protocol), ...extra}
  if (protocol === 'responses') body.input = messages
  else body.messages = messages
  const endpoint = {chat: 'chat/completions', responses: 'responses', messages: 'messages'}[protocol]
  const response = await fetch(`${server.url}/v1/${endpoint}`, {method: 'POST', headers: {'content-type': 'application/json', authorization: 'Bearer do-not-log-secret', 'x-api-key': 'do-not-log-other-key'}, body: JSON.stringify(body)})
  assert.equal(response.status, 200)
  if (extra.stream) return parseEvents(await response.text())
  return response.json()
}

function choiceOf(protocol, response) {
  if (protocol === 'chat') {
    const message = response.choices[0].message
    const call = message.tool_calls?.[0]
    return call ? {call: {id: call.id, name: call.function.name, args: JSON.parse(call.function.arguments)}} : {text: message.content}
  }
  if (protocol === 'responses') {
    const item = response.output[0]
    return item.type === 'function_call' ? {call: {id: item.call_id, name: item.name, args: JSON.parse(item.arguments)}} : {text: item.content[0].text}
  }
  const item = response.content[0]
  return item.type === 'tool_use' ? {call: {id: item.id, name: item.name, args: item.input}} : {text: item.text}
}

function addToolRoundtrip(protocol, messages, call, output, isError = false) {
  if (protocol === 'chat') messages.push(
    {role: 'assistant', content: null, tool_calls: [{id: call.id, type: 'function', function: {name: call.name, arguments: JSON.stringify(call.args)}}]},
    {role: 'tool', tool_call_id: call.id, content: output, is_error: isError},
  )
  else if (protocol === 'responses') messages.push(
    {type: 'function_call', call_id: call.id, name: call.name, arguments: JSON.stringify(call.args)},
    {type: 'function_call_output', call_id: call.id, output, is_error: isError},
  )
  else messages.push(
    {role: 'assistant', content: [{type: 'tool_use', id: call.id, name: call.name, input: call.args}]},
    {role: 'user', content: [{type: 'tool_result', tool_use_id: call.id, content: output, is_error: isError}]},
  )
}

describe('deterministic multi-protocol native tool fixture', () => {
  let server
  before(async () => { server = await createMockAgentServer({scenarios: [scenario]}) })
  after(async () => { await server.close() })

  for (const protocol of ['chat', 'responses', 'messages']) {
    it(`${protocol} requires old context plus actual native history and workspace tool results`, async () => {
      // A fresh CLI receives prior context and the current prompt flattened into
      // one message. Only the old portion contains the original nonce.
      const messages = [{role: 'user', content: `Previous request: ${nonce}\n${marker('verify')} Continue the existing task.`}]
      let next = choiceOf(protocol, await invoke(server, protocol, messages))
      assert.equal(next.call.name, history.name)
      assert.deepEqual(next.call.args, {view: 'messages', limit: 50})
      addToolRoundtrip(protocol, messages, next.call, JSON.stringify({history_version: 'v1', messages: [{role: 'user', text: nonce}]}))
      next = choiceOf(protocol, await invoke(server, protocol, messages))
      assert.equal(next.call.name, shell.name)
      assert.equal(next.call.args.cmd, 'cat checkpoint.txt')
      addToolRoundtrip(protocol, messages, next.call, progress)
      next = choiceOf(protocol, await invoke(server, protocol, messages))
      assert.equal(next.text, 'GRASP_CONTINUITY_OK')
      const report = server.report().requests.at(-1)
      assert.equal(report.completed, true)
      assert.equal(report.assertions.every((item) => item.passed), true)
      assert.equal(report.toolResults.length, 2)
    })

    it(`${protocol} emits valid streaming tool and final-text events`, async () => {
      const messages = [{role: 'user', content: `${marker('seed')} Seed ${nonce}`}]
      const events = await invoke(server, protocol, messages, {stream: true})
      let call
      if (protocol === 'chat') {
        call = events[0].choices[0].delta.tool_calls[0]
        assert.equal(events.at(-1).choices[0].finish_reason, 'tool_calls')
        call = {id: call.id, name: call.function.name, args: JSON.parse(call.function.arguments)}
      } else if (protocol === 'responses') {
        assert.equal(events[0].type, 'response.created')
        assert.equal(events.at(-1).type, 'response.completed')
        const item = events.at(-1).response.output[0]
        assert.equal(events.find((event) => event.type === 'response.function_call_arguments.delta').delta, item.arguments)
        call = {id: item.call_id, name: item.name, args: JSON.parse(item.arguments)}
      } else {
        assert.equal(events[0].type, 'message_start')
        assert.equal(events.at(-1).type, 'message_stop')
        const item = events.find((event) => event.type === 'content_block_start').content_block
        call = {id: item.id, name: item.name, args: JSON.parse(events.find((event) => event.type === 'content_block_delta').delta.partial_json)}
      }
      assert.equal(call.name, shell.name)
      addToolRoundtrip(protocol, messages, call, 'wrote-checkpoint')
      const finalEvents = await invoke(server, protocol, messages, {stream: true})
      assert.equal(JSON.stringify(finalEvents).includes('GRASP_SEED_OK'), true)
    })
  }

  it('rejects nonce placed only in the new current prompt, even after earlier successful runs', async () => {
    const response = choiceOf('chat', await invoke(server, 'chat', [{role: 'user', content: `${marker('verify')} Repeat ${nonce}.`}]))
    assert.match(response.text, /GRASP_MOCK_FAILED: missing prior_context/)
    assert.equal(server.report().requests.at(-1).completed, false)
  })

  it('picks the latest user marker when old markers occur in handoff history', async () => {
    const messages = [{role: 'user', content: `${marker('seed')} ${nonce}\nEarlier response and progress.\n${marker('verify')} Continue.`}]
    const response = choiceOf('chat', await invoke(server, 'chat', messages))
    assert.equal(response.call.name, history.name)
    assert.equal(server.report().requests.at(-1).turn, 'verify')
  })

  it('does not mistake an old marker returned by history for the current user request', async () => {
    const messages = [{role: 'user', content: `${nonce}\n${marker('verify')} Continue.`}]
    const response = choiceOf('messages', await invoke(server, 'messages', messages))
    addToolRoundtrip('messages', messages, response.call, `${marker('seed')} ${nonce}`)
    const next = choiceOf('messages', await invoke(server, 'messages', messages))
    assert.equal(next.call.name, shell.name)
    assert.equal(server.report().requests.at(-1).turn, 'verify')
  })

  it('fails on an actual tool result missing required evidence instead of replying with the configured nonce', async () => {
    const messages = [{role: 'user', content: `${nonce}\n${marker('verify')} Continue.`}]
    const response = choiceOf('chat', await invoke(server, 'chat', messages))
    addToolRoundtrip('chat', messages, response.call, '{"messages":[]}')
    const next = choiceOf('chat', await invoke(server, 'chat', messages))
    assert.match(next.text, /tool result assertion failed at action 0/)
    assert.equal(next.text.includes(nonce), false)
  })

  it('rejects native error results even if their error text contains the expected evidence', async () => {
    const messages = [{role: 'user', content: `${nonce}\n${marker('verify')} Continue.`}]
    const response = choiceOf('messages', await invoke(server, 'messages', messages))
    addToolRoundtrip('messages', messages, response.call, `failed to read ${nonce}`, true)
    const next = choiceOf('messages', await invoke(server, 'messages', messages))
    assert.match(next.text, /tool result assertion failed/)
  })

  it('requires matching tool call IDs and does not accept unrelated outputs', async () => {
    const messages = [{role: 'user', content: `${nonce}\n${marker('verify')} Continue.`}, {role: 'tool', tool_call_id: 'unrelated', content: nonce}]
    const response = choiceOf('chat', await invoke(server, 'chat', messages))
    assert.equal(response.call.name, history.name)
  })

  it('validates tool_results assertions only after tool execution', async () => {
    const messages = [{role: 'user', content: `${marker('results')} Retrieve history.`}]
    const response = choiceOf('responses', await invoke(server, 'responses', messages))
    addToolRoundtrip('responses', messages, response.call, 'retrieved-from-tool')
    const next = choiceOf('responses', await invoke(server, 'responses', messages))
    assert.equal(next.text, 'GRASP_RESULT_OK')
  })

  it('paginates actual MCP-wrapped history results with a fixed snapshot until the original user requirement is found', async () => {
    await fetch(`${server.url}/__mock/scenarios`, {method: 'POST', body: JSON.stringify({id: 'paged', turns: [{id: 'verify', actions: [{kind: 'history', paginate: true, maxPages: 3, arguments: {view: 'messages', limit: 50}, expect: ['Original requirement: keep React and cancel payments.']}], reply: 'GRASP_PAGING_OK'}]})})
    const messages = [{role: 'user', content: '[GRASP_MOCK run=paged turn=verify] Continue.'}]
    let response = choiceOf('chat', await invoke(server, 'chat', messages))
    assert.deepEqual(response.call.args, {view: 'messages', limit: 50})
    const first = {conversationId: 'thread-1', history_version: 'fixed-snapshot', messages: [{id: 'm5', role: 'user', content: 'Recent message only.'}], has_more: true, next_cursor: 'cursor-older'}
    addToolRoundtrip('chat', messages, response.call, JSON.stringify({result: {content: [{type: 'text', text: JSON.stringify(first)}]}}))
    response = choiceOf('chat', await invoke(server, 'chat', messages))
    assert.equal(response.call.name, history.name)
    assert.equal(response.call.args.cursor, 'cursor-older')
    assert.equal(response.call.id.length < 64, true)
    const second = {conversationId: 'thread-1', history_version: 'fixed-snapshot', messages: [{id: 'm1', role: 'user', content: 'Original requirement: keep React and cancel payments.'}], has_more: false}
    // CodeBuddy can JSON-encode a JSON string inside the deferred result.
    addToolRoundtrip('chat', messages, response.call, JSON.stringify(JSON.stringify({content: [{type: 'text', text: JSON.stringify(second)}]})))
    response = choiceOf('chat', await invoke(server, 'chat', messages))
    assert.equal(response.text, 'GRASP_PAGING_OK')
    assert.equal(server.report().requests.at(-1).assertions[0].pages, 2)
  })

  it('fails paginated history on missing or repeated cursors, changed snapshots, errors and bounded exhaustion', async () => {
    const target = 'historical-only-requirement'
    const badCases = [
      {id: 'missing', first: {has_more: true}, want: /requires next_cursor/},
      {id: 'repeated', first: {has_more: true, next_cursor: 'same'}, second: {has_more: true, next_cursor: 'same'}, want: /repeated a cursor/},
      {id: 'snapshot', first: {has_more: true, next_cursor: 'older'}, second: {history_version: 'changed', has_more: false}, want: /snapshot changed/},
      {id: 'conversation', first: {has_more: true, next_cursor: 'older'}, second: {conversationId: 'other-thread', has_more: false}, want: /snapshot changed/},
      {id: 'error', first: {isError: true, content: [{type: 'text', text: target}]}, want: /returned an error/},
      {id: 'notjson', raw: 'get_history failed: unauthorized', want: /no valid snapshot page/},
      {id: 'ended', first: {has_more: false}, want: /ended before required context/},
      {id: 'exhausted', maxPages: 1, first: {has_more: true, next_cursor: 'older'}, want: /exceeded maxPages/},
      {id: 'stale', first: {has_more: false, next_cursor: 'stale-cursor'}, want: /stale next_cursor/},
    ]
    const page = (overrides) => ({conversationId: 'thread', history_version: 'snapshot', messages: [{id: 'm', content: 'no target'}], ...overrides})
    for (const bad of badCases) {
      await fetch(`${server.url}/__mock/scenarios`, {method: 'POST', body: JSON.stringify({id: bad.id, turns: [{id: 'verify', actions: [{kind: 'history', paginate: true, maxPages: bad.maxPages || 3, expect: [target]}], reply: 'must-not-pass'}]})})
      const messages = [{role: 'user', content: `[GRASP_MOCK run=${bad.id} turn=verify] Continue.`}]
      let response = choiceOf('responses', await invoke(server, 'responses', messages))
      addToolRoundtrip('responses', messages, response.call, bad.raw || JSON.stringify(page(bad.first)))
      response = choiceOf('responses', await invoke(server, 'responses', messages))
      if (bad.second) {
        assert.equal(response.call.args.cursor, bad.first.next_cursor)
        addToolRoundtrip('responses', messages, response.call, JSON.stringify(page(bad.second)))
        response = choiceOf('responses', await invoke(server, 'responses', messages))
      }
      assert.match(response.text, bad.want, bad.id)
      assert.equal(server.report().requests.at(-1).completed, false)
    }
  })

  it('does not use configured target text, cursor metadata or earlier requests as history evidence', async () => {
    const target = 'historical-only-requirement'
    const messages = [{role: 'user', content: '[GRASP_MOCK run=ended turn=verify] Continue.'}]
    let response = choiceOf('messages', await invoke(server, 'messages', messages))
    addToolRoundtrip('messages', messages, response.call, JSON.stringify({history_version: target, messages: [{content: 'no target'}], has_more: false}))
    response = choiceOf('messages', await invoke(server, 'messages', messages))
    assert.match(response.text, /ended before required context/)
    assert.equal(response.text.includes(target), false)
  })

  it('role-qualified pagination requires original user content rather than assistant quotes or toolResults', async () => {
    const original = 'Original requirement in first user message.'
    await fetch(`${server.url}/__mock/scenarios`, {method: 'POST', body: JSON.stringify({id: 'user-only', turns: [{id: 'verify', actions: [{kind: 'history', paginate: true, expectRole: 'user', expect: [original]}], reply: 'USER_CONTEXT_OK'}]})})
    const messages = [{role: 'user', content: '[GRASP_MOCK run=user-only turn=verify] Continue.'}]
    let response = choiceOf('chat', await invoke(server, 'chat', messages))
    addToolRoundtrip('chat', messages, response.call, JSON.stringify({conversationId: 'thread', history_version: 'snapshot', messages: [{role: 'assistant', content: original}, {role: 'user', content: 'new request', toolResults: [{text: original}]}], has_more: true, next_cursor: 'older'}))
    response = choiceOf('chat', await invoke(server, 'chat', messages))
    assert.equal(response.call.args.cursor, 'older')
    addToolRoundtrip('chat', messages, response.call, JSON.stringify({conversationId: 'thread', history_version: 'snapshot', messages: [{role: 'user', content: original}], has_more: false}))
    response = choiceOf('chat', await invoke(server, 'chat', messages))
    assert.equal(response.text, 'USER_CONTEXT_OK')
  })

  it('parses native Claude MCP text blocks independently of appended client reminders', async () => {
    const messages = [{role: 'user', content: '[GRASP_MOCK run=user-only turn=verify] Continue.'}]
    let response = choiceOf('messages', await invoke(server, 'messages', messages))
    addToolRoundtrip('messages', messages, response.call, [
      {type: 'text', text: JSON.stringify({history_version: 'snapshot', messages: [{role: 'user', content: 'Original requirement in first user message.'}], has_more: false})},
      {type: 'text', text: '<system-reminder>Tool completed. Follow the current user instructions.</system-reminder>'},
    ])
    response = choiceOf('messages', await invoke(server, 'messages', messages))
    assert.equal(response.text, 'USER_CONTEXT_OK')
  })

  it('performs CodeBuddy native ToolSearch then DeferExecuteTool and checks its actual output', async () => {
    const tools = toolsFor('chat', [
      {name: 'ToolSearch', parameters: {type: 'object', properties: {query: {type: 'string'}}}},
      {name: 'DeferExecuteTool', parameters: {type: 'object', properties: {toolName: {type: 'string'}, params: {type: 'object'}}}},
      shell,
    ])
    const messages = [{role: 'user', content: `${nonce}\n${marker('verify')} Continue.`}]
    let response = choiceOf('chat', await invoke(server, 'chat', messages, {tools}))
    assert.equal(response.call.name, 'ToolSearch')
    assert.match(response.call.args.query, /get_history/)
    addToolRoundtrip('chat', messages, response.call, JSON.stringify({tools: [{name: history.name, inputSchema: history.parameters}]}))
    response = choiceOf('chat', await invoke(server, 'chat', messages, {tools}))
    assert.equal(response.call.name, 'DeferExecuteTool')
    assert.deepEqual(response.call.args, {toolName: history.name, params: {view: 'messages', limit: 50}})
    addToolRoundtrip('chat', messages, response.call, nonce)
    response = choiceOf('chat', await invoke(server, 'chat', messages, {tools}))
    assert.equal(response.call.name, shell.name)
  })

  it('fails discovery if the actual ToolSearch result contains no history tool', async () => {
    const tools = toolsFor('chat', [{name: 'ToolSearch', parameters: {type: 'object', properties: {query: {type: 'string'}}}}])
    const messages = [{role: 'user', content: `${nonce}\n${marker('verify')} Continue.`}]
    const response = choiceOf('chat', await invoke(server, 'chat', messages, {tools}))
    addToolRoundtrip('chat', messages, response.call, '{"tools":[]}')
    const next = choiceOf('chat', await invoke(server, 'chat', messages, {tools}))
    assert.match(next.text, /discovery did not return get_history/)
  })

  it('uses CodeBuddy array discovery schemas and rejects unknown required arguments', async () => {
    const messages = [{role: 'user', content: `${nonce}\n${marker('verify')} Continue.`}]
    const schemas = [
      {properties: {queries: {type: 'array'}, top_k: {type: 'integer'}}, required: ['queries', 'top_k']},
      {properties: {tool_names: {type: 'array'}}, required: ['tool_names']},
      {properties: {query: {type: 'string'}, max_results: {type: 'number'}}, required: ['query', 'max_results']},
    ]
    for (const parameters of schemas) {
      const tools = toolsFor('chat', [{name: 'ToolSearch', parameters: {type: 'object', ...parameters}}])
      const next = choiceOf('chat', await invoke(server, 'chat', messages, {tools}))
      assert.equal(next.call.name, 'ToolSearch')
      assert.deepEqual(Object.keys(next.call.args).sort(), Object.keys(parameters.properties).sort())
      assert.equal(JSON.stringify(next.call.args).includes('get_history'), true)
    }
    const tools = toolsFor('chat', [{name: 'ToolSearch', parameters: {type: 'object', properties: {query: {type: 'string'}}, required: ['unrecognized']}}])
    const next = choiceOf('chat', await invoke(server, 'chat', messages, {tools}))
    assert.match(next.text, /discovery requires an explicit additional argument/)
  })

  it('records reasoning settings, evidence hashes and no credentials or raw conversation', async () => {
    await invoke(server, 'chat', [{role: 'user', content: 'private-prompt-content'}], {reasoning_effort: 'high'})
    await invoke(server, 'responses', [], {reasoning: {effort: 'low'}})
    await invoke(server, 'messages', [], {thinking: {type: 'enabled', budget_tokens: 4096}, output_config: {effort: 'medium'}})
    const report = await (await fetch(`${server.url}/__mock/report?token=do-not-log-query-secret`)).json()
    assert.deepEqual(report.requests.at(-3).reasoning, {reasoning_effort: 'high'})
    assert.deepEqual(report.requests.at(-2).reasoning, {effort: 'low'})
    assert.deepEqual(report.requests.at(-1).reasoning, {thinking_type: 'enabled', thinking_budget: '4096', output_effort: 'medium'})
    const raw = JSON.stringify(report)
    for (const secret of ['do-not-log-secret', 'do-not-log-other-key', 'do-not-log-query-secret', 'private-prompt-content']) assert.equal(raw.includes(secret), false)
    assert.equal(report.requests.some((request) => request.toolResults.some((result) => /^[a-f0-9]{64}$/.test(result.sha256))), true)
  })

  for (const stream of [false, true]) {
    it(`encodes native Responses namespace separately from the function name (stream=${stream})`, async () => {
      const tools = [{type: 'namespace', name: 'mcp__artifact_store', tools: [{type: 'function', name: 'get_history', parameters: history.parameters}]}, ...toolsFor('responses', [shell])]
      const messages = [{role: 'user', content: `${nonce}\n${marker('verify')} Continue.`}]
      let response = await invoke(server, 'responses', messages, {tools, stream})
      const item = stream ? response.at(-1).response.output[0] : response.output[0]
      assert.equal(item.name, 'get_history')
      assert.equal(item.namespace, 'mcp__artifact_store')
      if (stream) {
        const added = response.find((event) => event.type === 'response.output_item.added')
        assert.equal(added.item.name, 'get_history')
        assert.equal(added.item.namespace, 'mcp__artifact_store')
      }
      // Native Codex rejects the old dotted name with "unsupported call".
      // Echo the actual namespaced output item back as the next request does.
      messages.push(item, {type: 'function_call_output', call_id: item.call_id, output: nonce})
      response = await invoke(server, 'responses', messages, {tools})
      assert.equal(response.output[0].name, shell.name)
      assert.equal(response.output[0].namespace, undefined)
      assert.equal(server.report().requests.at(-2).emittedTool.name, 'mcp__artifact_store.get_history')
      assert.equal(server.report().requests.at(-2).emittedTool.namespace, 'mcp__artifact_store')
    })
  }

  it('registers scenarios over HTTP, serves model discovery and counts Anthropic tokens', async () => {
    const response = await fetch(`${server.url}/__mock/scenarios`, {method: 'POST', body: JSON.stringify({id: 'dynamic', turns: [{id: 'ping', reply: 'dynamic-ok'}]})})
    assert.equal(response.status, 201)
    assert.deepEqual(await response.json(), {ids: ['dynamic']})
    const ping = choiceOf('chat', await invoke(server, 'chat', [{role: 'user', content: '[GRASP_MOCK run=dynamic turn=ping] Hello'}]))
    assert.equal(ping.text, 'dynamic-ok')
    const models = await (await fetch(`${server.url}/v1/models`)).json()
    assert.equal(models.data.some((model) => model.id === 'mock-agent'), true)
    const tokens = await fetch(`${server.url}/v1/messages/count_tokens`, {method: 'POST', body: JSON.stringify({messages: []})})
    assert.equal((await tokens.json()).input_tokens > 0, true)
    assert.equal((await (await fetch(`${server.url}/health`)).json()).scenarios >= 2, true)
  })

  it('rejects malformed scenario and request JSON', async () => {
    const badScenario = await fetch(`${server.url}/__mock/scenarios`, {method: 'POST', body: JSON.stringify({id: 'invalid', turns: [{id: 'x', reply: 'x', assert: [{source: 'invented', contains: nonce}]}]})})
    assert.equal(badScenario.status, 400)
    const badJSON = await fetch(`${server.url}/v1/responses`, {method: 'POST', body: '{broken'})
    assert.equal(badJSON.status, 400)
    const missing = await fetch(`${server.url}/unimplemented`)
    assert.equal(missing.status, 404)
  })
})

describe('native request normalization', () => {
  it('keeps textual context and excludes inline image bytes', () => {
    const normalized = normalizeRequest('responses', {input: [{role: 'user', content: [{type: 'input_text', text: 'visible request'}, {type: 'input_image', image_url: 'data:image/png;base64,hidden-image-bytes'}]}], tools: []})
    assert.equal(JSON.stringify(normalized).includes('visible request'), true)
    assert.equal(JSON.stringify(normalized).includes('hidden-image-bytes'), false)
  })

  it('recognizes function tools inside a Responses namespace', () => {
    const normalized = normalizeRequest('responses', {input: [], tools: [{type: 'namespace', name: 'functions', tools: [{type: 'function', ...shell}]}]})
    assert.equal(normalized.tools[0].name, 'functions.exec_command')
    assert.deepEqual(normalized.tools[0].schema, shell.parameters)
  })
})

it('starts a CLI process without reading a vendor API key', async () => {
  const script = fileURLToPath(new URL('./mock-agent-model.mjs', import.meta.url))
  const child = spawn(process.execPath, [script, '--listen', '127.0.0.1', '--port', '0'], {env: {...process.env, DEEPSEEK_API_KEY: 'unused-do-not-read'}, stdio: ['ignore', 'pipe', 'pipe']})
  let stdout = ''
  let stderr = ''
  child.stderr.on('data', (chunk) => { stderr += chunk })
  const port = await new Promise((resolve, reject) => {
    const timer = setTimeout(() => { child.kill(); reject(new Error('mock startup timeout')) }, 5000)
    child.stdout.on('data', (chunk) => {
      stdout += chunk
      const match = stdout.match(/MOCK_AGENT_PORT=(\d+)/)
      if (match) { clearTimeout(timer); resolve(Number(match[1])) }
    })
    child.once('exit', (code) => { clearTimeout(timer); reject(new Error(`mock exited ${code}`)) })
  })
  try {
    const health = await (await fetch(`http://127.0.0.1:${port}/health`)).json()
    assert.equal(health.ok, true)
    assert.equal((stdout + stderr).includes('unused-do-not-read'), false)
  } finally {
    const exited = new Promise((resolve) => child.once('exit', resolve))
    child.kill('SIGTERM')
    await exited
  }
})
