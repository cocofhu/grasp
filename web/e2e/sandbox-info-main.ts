import '../src/styles/global.css'
import { createApp, h, ref } from 'vue'
import { createRouter, createMemoryHistory } from 'vue-router'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import SandboxInfoButton from '../src/components/sandbox/SandboxInfoButton.vue'
import type { SoloSession, SoloTaskInfo } from '../src/lib/api/apiTypes'

const params = new URLSearchParams(location.search)
const gateway = params.get('gateway') === 'off'
  ? { enabled: false }
  : { enabled: true, host: 'ssh.grasp.example.com', port: 2222, allowed: true, hasKey: params.get('key') !== 'none', hostKeyFingerprint: 'SHA256:3B+YKnDLjNEQamN/JIa9z2c3crfl7CbnImMw5flKR5k' }
const realFetch = window.fetch.bind(window)
const taskInfo: SoloTaskInfo = {
  usage: {
    total: 1_268_330, inputTokens: 750_000, outputTokens: 125_000, cacheReadTokens: 350_000, cacheWriteTokens: 43_330,
    reported: true, incomplete: false,
    byModel: {
      'claude-sonnet-4-6': { inputTokens: 550_000, outputTokens: 100_000, cacheReadTokens: 270_000, cacheWriteTokens: 33_330 },
      'gpt-5.2': { inputTokens: 175_000, outputTokens: 25_000, cacheReadTokens: 80_000, cacheWriteTokens: 10_000 },
      '未知/未分桶': { inputTokens: 25_000, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 },
    },
  },
  workflowRuns: [{ id: 'verify-sandbox', workflowName: '发布检查' }],
}
window.fetch = async (input, init) => {
  if (String(input).endsWith('/api/ssh-gateway')) {
    return new Response(JSON.stringify(gateway), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }
  if (String(input).endsWith('/api/solo/sessions/s1/task-info')) return Response.json(taskInfo)
  return realFetch(input, init)
}

const now = Date.now()
function session(patch: Partial<SoloSession> = {}, sandbox: Partial<NonNullable<SoloSession['sandbox']>> = {}): SoloSession {
  return {
    id: 's1', userId: 'alice', title: '能否支持ssh 连接到沙箱', agentName: 'claude-code', status: 'running', restarts: 0,
    createdAt: '2026-11-25T10:00:00Z', updatedAt: '2026-11-25T10:00:00Z', busy: false, waiting: 0, sandboxId: 123, branch: 'main',
    repos: [{ position: 0, projectId: 'p', name: 'cocofhu/grasp', repoUrl: 'https://github.com/cocofhu/grasp', repoDirName: 'grasp' }],
    backupAt: new Date(now - 3 * 3600_000).toISOString(), backupSize: 182 * 1024 * 1024,
    sandbox: {
      id: 123, name: 'grasp-solo-7f3a', status: 'running', connected: true, hasCodeServer: true, recoveryKind: 'snapshot',
      destroyAt: new Date(now + 5 * 3600_000 + 42 * 60_000).toISOString(), modelActiveAt: new Date(now - 3 * 60_000).toISOString(),
      createdAt: '2026-11-25T10:02:00Z', ...sandbox,
    },
    ...patch,
  }
}

const variants: Record<string, { session: SoloSession; busy?: boolean; paused?: boolean; waking?: boolean }> = {
  running: { session: session() },
  busy: { session: session(), busy: true },
  connected: { session: session({}, { externalSessions: 2, destroyAt: undefined }) },
  paused: { session: session(), paused: true },
  docker: { session: session({ idleReclaimExempt: true }, { recoveryKind: 'files', destroyAt: undefined }) },
  stopped: { session: session({ status: 'stopped' }) },
  error: { session: session({ status: 'error', error: 'git clone 失败：权限不足' }) },
  starting: { session: session({ status: 'starting' }, { status: 'pulling' }) },
}

async function bootstrap() {
  await initLocale()
  await setLocale(params.get('lang') === 'en' ? 'en' : 'zh-CN')
  setTheme(params.get('theme') === 'light' ? 'light' : 'dark')
  const variant = variants[params.get('state') || 'running'] || variants.running!
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { render: () => null } }] })
  createApp({
    setup() {
      const keepAlive = ref(!!variant.session.keepAlive)
      const events = ref<string[]>([])
      return () => h('div', { class: 'min-h-screen' }, [
        h('header', { class: 'flex h-12 items-center gap-2 border-b border-line px-4', 'data-testid': 'toolbar' }, [
          h('span', { class: 'text-[13px] text-txt2' }, variant.session.title),
          h('div', { class: 'ml-auto flex items-center gap-0.5' }, [
            h(SandboxInfoButton, {
              session: { ...variant.session, keepAlive: keepAlive.value },
              busy: !!variant.busy, paused: !!variant.paused, waking: !!variant.waking,
              onAction: (a: string) => events.value.push(`action:${a}`),
              onKeepAlive: (v: boolean) => { keepAlive.value = v; events.value.push(`keepAlive:${v}`) },
            }),
            h('span', { class: 'inline-flex h-8 w-8 items-center justify-center text-txt3' }, '▦'),
            h('span', { class: 'inline-flex h-8 w-8 items-center justify-center text-txt3' }, '▤'),
          ]),
        ]),
        h('pre', { class: 'p-4 text-xs text-txt3', 'data-testid': 'events' }, events.value.join('\n')),
      ])
    },
  }).use(i18n).use(router).mount('#app')
}

void bootstrap()
