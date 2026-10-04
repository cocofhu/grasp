#!/usr/bin/env node
// Host-side OpenAI-compatible mock chat model for sandbox CI.
// Fixture id ci-e2e replays one assistant reply. The process does not read
// vendor API keys and does not make any outbound request.
//
//   node mock-chat-model.mjs [--listen HOST] [--port PORT]
//   node mock-chat-model.mjs --print-opencode-config --base-url http://host:port/v1
//
// Listens on 0.0.0.0 by default so a sandbox container can reach it through
// host.docker.internal (host-gateway). Prints MOCK_CHAT_PORT=<n> when ready.
// GET /health reports how many completion requests were replayed.

import fs from 'node:fs'
import http from 'node:http'
import {pathToFileURL} from 'node:url'

export const FIXTURE_ID = 'ci-e2e'
export const MARKER = 'GRASP_AGENT_E2E_OK'
export const REPLY_TEXT = MARKER
export const FINISH_REASON = 'stop'
export const INPUT_TOKENS = 16
export const OUTPUT_TOKENS = 8
export const PROVIDER_ID = 'custom'
export const MODEL_ID = 'ci-e2e'
const COMPLETION_ID = 'chatcmpl-ci-e2e'
const BODY_LIMIT = 1_000_000

export function usageReport() {
  return {
    prompt_tokens: INPUT_TOKENS,
    completion_tokens: OUTPUT_TOKENS,
    total_tokens: INPUT_TOKENS + OUTPUT_TOKENS,
  }
}

export function completionBody(model) {
  return {
    id: COMPLETION_ID,
    object: 'chat.completion',
    created: 1,
    model: model || MODEL_ID,
    choices: [
      {
        index: 0,
        message: {role: 'assistant', content: REPLY_TEXT},
        finish_reason: FINISH_REASON,
      },
    ],
    usage: usageReport(),
  }
}

// Same shape the platform writes for GRASP_OPENCODE_PROVIDER=custom plus
// GRASP_OPENCODE_BASE_URL: OpenAI-compatible adapter, placeholder API key,
// and the ci-e2e model declared so the CLI does not look it up remotely.
export function opencodeConfig(baseURL) {
  return {
    $schema: 'https://opencode.ai/config.json',
    model: `${PROVIDER_ID}/${MODEL_ID}`,
    provider: {
      [PROVIDER_ID]: {
        npm: '@ai-sdk/openai-compatible',
        name: 'Custom',
        options: {
          apiKey: '{env:OPENCODE_API_KEY}',
          baseURL,
        },
        models: {
          [MODEL_ID]: {name: MODEL_ID},
        },
      },
    },
  }
}

function json(res, status, body) {
  const payload = JSON.stringify(body)
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': Buffer.byteLength(payload),
  })
  res.end(payload)
}

function wantsStream(body) {
  return Boolean(body) && (body.stream === true || body.stream === 'true')
}

function writeSSE(res, model) {
  res.writeHead(200, {
    'Content-Type': 'text/event-stream; charset=utf-8',
    'Cache-Control': 'no-cache',
    Connection: 'keep-alive',
  })
  const base = {
    id: COMPLETION_ID,
    object: 'chat.completion.chunk',
    created: 1,
    model: model || MODEL_ID,
  }
  const send = (payload) => {
    res.write(`data: ${JSON.stringify(payload)}\n\n`)
  }
  send({
    ...base,
    choices: [{index: 0, delta: {role: 'assistant', content: REPLY_TEXT}, finish_reason: null}],
  })
  send({
    ...base,
    choices: [{index: 0, delta: {}, finish_reason: FINISH_REASON}],
    usage: usageReport(),
  })
  res.write('data: [DONE]\n\n')
  res.end()
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = []
    let size = 0
    req.on('data', (chunk) => {
      size += chunk.length
      if (size > BODY_LIMIT) {
        reject(Object.assign(new Error('body too large'), {status: 413}))
        req.destroy()
        return
      }
      chunks.push(chunk)
    })
    req.on('end', () => resolve(Buffer.concat(chunks)))
    req.on('error', reject)
  })
}

function normalizePath(url) {
  const path = new URL(url || '/', 'http://127.0.0.1').pathname
  if (path.length > 1 && path.endsWith('/')) return path.slice(0, -1)
  return path
}

function handle(req, res, state) {
  const method = req.method || 'GET'
  const path = normalizePath(req.url)
  if (method === 'GET' && path === '/health') {
    json(res, 200, {ok: true, fixture: FIXTURE_ID, hits: state.hits})
    return
  }
  if (method === 'GET' && (path === '/v1/models' || path === '/models')) {
    json(res, 200, {
      object: 'list',
      data: [{id: MODEL_ID, object: 'model', owned_by: FIXTURE_ID}],
    })
    return
  }
  const isCompletion = path === '/v1/chat/completions' || path === '/chat/completions'
  if (!isCompletion) {
    json(res, 404, {error: {message: `unknown path ${path}`, type: 'invalid_request_error'}})
    return
  }
  if (method !== 'POST') {
    json(res, 405, {error: {message: 'method not allowed', type: 'invalid_request_error'}})
    return
  }
  readBody(req)
    .then((buf) => {
      let body = {}
      if (buf.length > 0) {
        try {
          body = JSON.parse(buf.toString('utf8'))
        } catch {
          json(res, 400, {error: {message: 'invalid json', type: 'invalid_request_error'}})
          return
        }
      }
      state.hits += 1
      const model = typeof body.model === 'string' && body.model ? body.model : MODEL_ID
      if (wantsStream(body)) writeSSE(res, model)
      else json(res, 200, completionBody(model))
    })
    .catch((err) => {
      if (res.headersSent || res.writableEnded) return
      const status = err && err.status ? err.status : 400
      json(res, status, {error: {message: 'bad request', type: 'invalid_request_error'}})
    })
}

export function createMockChatServer(options = {}) {
  const state = {hits: 0}
  const server = http.createServer((req, res) => handle(req, res, state))
  const listenHost = options.listen ?? '127.0.0.1'
  const listenPort = options.port ?? 0
  return new Promise((resolve, reject) => {
    const onError = (err) => reject(err)
    server.once('error', onError)
    server.listen(listenPort, listenHost, () => {
      server.off('error', onError)
      const addr = server.address()
      const port = typeof addr === 'object' && addr ? addr.port : listenPort
      resolve({
        port,
        url: `http://127.0.0.1:${port}`,
        hits: () => state.hits,
        close: () =>
          new Promise((res, rej) => {
            server.close((err) => (err ? rej(err) : res()))
          }),
      })
    })
  })
}

function parseArgs(argv) {
  const out = {listen: '0.0.0.0', port: 0, printConfig: false, baseURL: '', help: false}
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]
    if (arg === '--listen') out.listen = argv[++i]
    else if (arg === '--port') out.port = Number(argv[++i])
    else if (arg === '--print-opencode-config') out.printConfig = true
    else if (arg === '--base-url') out.baseURL = argv[++i]
    else if (arg === '--help' || arg === '-h') out.help = true
    else throw new Error(`unknown argument ${arg}`)
  }
  if (out.listen === undefined || Number.isNaN(out.port)) throw new Error('invalid arguments')
  return out
}

async function main() {
  const args = parseArgs(process.argv.slice(2))
  if (args.help) {
    process.stderr.write(
      'usage: mock-chat-model.mjs [--listen HOST] [--port PORT]\n' +
        '       mock-chat-model.mjs --print-opencode-config --base-url URL\n',
    )
    return
  }
  if (args.printConfig) {
    if (!args.baseURL) throw new Error('missing --base-url')
    process.stdout.write(`${JSON.stringify(opencodeConfig(args.baseURL), null, 2)}\n`)
    return
  }
  const started = await createMockChatServer({listen: args.listen, port: args.port})
  fs.writeSync(1, `MOCK_CHAT_PORT=${started.port}\n`)
  fs.writeSync(1, `MOCK_CHAT_URL=http://127.0.0.1:${started.port}\n`)
}

const isMain = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href
if (isMain) {
  main().catch((err) => {
    process.stderr.write(`mock-chat-model: ${err.message}\n`)
    process.exit(1)
  })
}
