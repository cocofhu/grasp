import {strict as assert} from 'node:assert'
import {spawn} from 'node:child_process'
import path from 'node:path'
import {after, before, describe, it} from 'node:test'
import {fileURLToPath} from 'node:url'

import {
  FINISH_REASON,
  FIXTURE_ID,
  INPUT_TOKENS,
  MARKER,
  MODEL_ID,
  OUTPUT_TOKENS,
  PROVIDER_ID,
  createMockChatServer,
  opencodeConfig,
} from './mock-chat-model.mjs'

const script = fileURLToPath(new URL('./mock-chat-model.mjs', import.meta.url))

function assistantText(events) {
  return events
    .flatMap((ev) => (ev.choices || []).map((choice) => choice.delta?.content || choice.message?.content || ''))
    .join('')
}

function parseSSE(raw) {
  const events = []
  let done = false
  for (const line of raw.split('\n')) {
    if (!line.startsWith('data:')) continue
    const data = line.slice(5).trim()
    if (data === '[DONE]') {
      done = true
      continue
    }
    events.push(JSON.parse(data))
  }
  return {events, done}
}

describe('mock chat model fixture ci-e2e', () => {
  let server
  before(async () => {
    server = await createMockChatServer({listen: '127.0.0.1', port: 0})
  })
  after(async () => {
    await server.close()
  })

  it('replays a one-shot completion with the marker, finish_reason stop, and usage', async () => {
    const res = await fetch(`${server.url}/v1/chat/completions`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer secret-token-should-not-echo',
      },
      body: JSON.stringify({
        model: MODEL_ID,
        messages: [{role: 'user', content: 'say hello without the marker'}],
      }),
    })
    assert.equal(res.status, 200)
    const body = await res.json()
    const raw = JSON.stringify(body)
    assert.equal(body.choices[0].message.content, MARKER)
    assert.equal(body.choices[0].finish_reason, FINISH_REASON)
    assert.equal(body.usage.prompt_tokens, INPUT_TOKENS)
    assert.equal(body.usage.completion_tokens, OUTPUT_TOKENS)
    assert.equal(body.usage.total_tokens, INPUT_TOKENS + OUTPUT_TOKENS)
    assert.equal(raw.includes('secret-token-should-not-echo'), false)
    assert.equal(server.hits(), 1)
  })

  it('replays the same marker on an SSE stream', async () => {
    const beforeHits = server.hits()
    const res = await fetch(`${server.url}/v1/chat/completions`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({model: 'other-model', stream: true, messages: [{role: 'user', content: 'stream please'}]}),
    })
    assert.equal(res.status, 200)
    assert.match(res.headers.get('content-type') || '', /text\/event-stream/)
    const {events, done} = parseSSE(await res.text())
    assert.equal(done, true)
    assert.equal(assistantText(events).includes(MARKER), true)
    const finish = events.map((ev) => ev.choices?.[0]?.finish_reason).find((reason) => reason)
    assert.equal(finish, FINISH_REASON)
    const withUsage = events.find((ev) => ev.usage)
    assert.equal(withUsage.usage.prompt_tokens, INPUT_TOKENS)
    assert.equal(withUsage.usage.completion_tokens, OUTPUT_TOKENS)
    assert.equal(server.hits(), beforeHits + 1)
  })

  it('accepts /chat/completions and reports hits on /health', async () => {
    const res = await fetch(`${server.url}/chat/completions`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({stream: 'true', messages: []}),
    })
    assert.equal(res.status, 200)
    const {events, done} = parseSSE(await res.text())
    assert.equal(done, true)
    assert.equal(assistantText(events).includes(MARKER), true)
    const health = await fetch(`${server.url}/health`)
    const body = await health.json()
    assert.equal(body.ok, true)
    assert.equal(body.fixture, FIXTURE_ID)
    assert.equal(body.hits, server.hits())
    assert.equal(body.hits >= 3, true)
  })

  it('does not put the marker on the models list', async () => {
    const res = await fetch(`${server.url}/v1/models`)
    const body = await res.json()
    assert.equal(body.data[0].id, MODEL_ID)
    assert.equal(JSON.stringify(body).includes(MARKER), false)
  })
})

describe('opencode config for the mock', () => {
  it('matches the custom OpenAI-compatible adapter', () => {
    const baseURL = 'http://host.docker.internal:4321/v1'
    const doc = opencodeConfig(baseURL)
    assert.equal(doc.model, `${PROVIDER_ID}/${MODEL_ID}`)
    assert.equal(doc.provider[PROVIDER_ID].npm, '@ai-sdk/openai-compatible')
    assert.equal(doc.provider[PROVIDER_ID].options.baseURL, baseURL)
    assert.equal(doc.provider[PROVIDER_ID].options.apiKey, '{env:OPENCODE_API_KEY}')
    assert.equal(doc.provider[PROVIDER_ID].models[MODEL_ID].name, MODEL_ID)
  })
})

describe('mock chat model CLI', () => {
  it('prints a port and serves the fixture without a vendor key', async () => {
    const child = spawn(process.execPath, [script, '--listen', '127.0.0.1', '--port', '0'], {
      env: {...process.env, CURSOR_API_KEY: 'crsr_should_not_leak'},
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    let stdout = ''
    const port = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('mock did not print a port')), 5000)
      child.stdout.on('data', (chunk) => {
        stdout += chunk
        const match = stdout.match(/MOCK_CHAT_PORT=(\d+)/)
        if (match) {
          clearTimeout(timer)
          resolve(Number(match[1]))
        }
      })
      child.on('exit', (code) => {
        clearTimeout(timer)
        reject(new Error(`mock exited ${code} before ready`))
      })
    })
    try {
      const res = await fetch(`http://127.0.0.1:${port}/v1/chat/completions`, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({messages: [{role: 'user', content: 'ping'}]}),
      })
      const body = await res.json()
      assert.equal(body.choices[0].message.content, MARKER)
      assert.equal(body.choices[0].finish_reason, 'stop')
      assert.equal(JSON.stringify(body).includes('crsr_should_not_leak'), false)
      const health = await fetch(`http://127.0.0.1:${port}/health`)
      assert.equal((await health.json()).hits, 1)
    } finally {
      child.kill('SIGTERM')
      await new Promise((resolve) => child.on('exit', resolve))
    }
  })

  it('prints opencode.json for a base URL', async () => {
    const child = spawn(process.execPath, [script, '--print-opencode-config', '--base-url', 'http://example.test/v1'], {
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    let stdout = ''
    child.stdout.on('data', (chunk) => {
      stdout += chunk
    })
    const code = await new Promise((resolve) => child.on('exit', resolve))
    assert.equal(code, 0)
    const doc = JSON.parse(stdout)
    assert.equal(doc.provider.custom.options.baseURL, 'http://example.test/v1')
    assert.equal(doc.model, 'custom/ci-e2e')
  })
})

describe('script path', () => {
  it('lives next to the sandbox scripts the CI job already syntax-checks', () => {
    assert.equal(path.basename(script), 'mock-chat-model.mjs')
  })
})
