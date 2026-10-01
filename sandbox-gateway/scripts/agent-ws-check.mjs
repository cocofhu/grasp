#!/usr/bin/env node
// Drives the sandbox backend's /ws like the platform does: connect, then
// optionally run one chat turn. Exits non-zero with the reason on failure.
//
//   node agent-ws-check.mjs <ws-url> connect [timeoutSec]
//   node agent-ws-check.mjs <ws-url> chat    [timeoutSec]   (needs agent credentials)
const [url, mode = 'connect', timeoutArg = '120'] = process.argv.slice(2)
if (!url || !['connect', 'chat'].includes(mode)) {
  console.error('usage: agent-ws-check.mjs <ws-url> connect|chat [timeoutSec]')
  process.exit(2)
}
const timeoutMs = Number(timeoutArg) * 1000
const marker = 'GRASP_AGENT_E2E_OK'
const opId = `e2e-${Date.now()}`

function fail(msg) {
  console.error(`FAIL: ${msg}`)
  process.exit(1)
}

const ws = new WebSocket(url)
const timer = setTimeout(() => fail(`no ${mode === 'chat' ? 'prompt_done' : 'connected'} within ${timeoutArg}s`), timeoutMs)
let connected = false
let text = ''

ws.addEventListener('open', () => {
  ws.send(JSON.stringify({ op: 'connect', cwd: '/root/workspace', autoPermission: true }))
})
ws.addEventListener('error', () => fail(`websocket error on ${url}`))
ws.addEventListener('close', (ev) => fail(`websocket closed (code ${ev.code})`))
ws.addEventListener('message', (ev) => {
  let msg
  try {
    msg = JSON.parse(String(ev.data))
  } catch {
    return
  }
  if (msg.op === 'error') fail(`backend error: ${msg.message}`)
  if (msg.op === 'connected' && !connected) {
    connected = true
    const agent = msg.agent || {}
    console.log(`connected: agent=${agent.name || '?'} version=${agent.version || '?'} session=${msg.sessionId || '?'}`)
    if (mode === 'connect') return done()
    ws.send(JSON.stringify({ op: 'chat', opId, text: `Reply with exactly ${marker} and nothing else. Do not use any tools.` }))
    return
  }
  if (msg.op !== 'event' || !msg.data) return
  const d = msg.data
  if (d.type === 'error_text') fail(`agent error: ${d.text}`)
  text += JSON.stringify(d)
  if (d.type === 'prompt_done') {
    if (d.stopReason !== 'end_turn') fail(`turn ended with stopReason=${d.stopReason}`)
    if (!text.includes(marker)) fail(`reply did not contain ${marker}`)
    console.log('chat: agent replied and finished the turn')
    done()
  }
})

function done() {
  clearTimeout(timer)
  process.exit(0)
}
