import '../src/styles/global.css'
import { computed, createApp, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { renderChatMarkdownBlocks, renderMarkdown, type MarkdownBlockCache } from '../src/lib/shared/markdown'
import AgentTimeline from '../src/components/run/AgentTimeline.vue'
import StreamMarkdown from '../src/components/run/StreamMarkdown.vue'

const FLOW = `flowchart LR
  subgraph Browser[浏览器]
    UI[凭证页<br/>设备码卡片 / 粘贴 auth.json]
  end
  subgraph Server[Grasp Server 控制面]
    DF[CodexDeviceFlowService<br/>设备码会话]
    LM[CodexLoginManager<br/>唯一持有 refresh token<br/>提前 96h 续期 / 单飞 / CAS]
    DB[(workspace 凭证<br/>完整 auth.json 加密存储)]
    HUB[CodexLoginHub<br/>推送 / 回合前对齐 / 401 恢复]
    OC[codexoauth.Client<br/>直连未公开接口]
  end
  subgraph SB[沙箱 Docker/K8s/E2B]
    AUTH[CODEX_HOME/auth.json<br/>refresh_token 为空串]
    CX[Codex CLI]
  end
  OA[auth.openai.com]
  CG[chatgpt.com/backend-api/codex]

  UI --> DF --> OC
  LM --> OC --> OA
  DF --> DB
  LM <--> DB
  LM --> HUB -->|WriteFileAtomic| AUTH
  AUTH --> CX -->|access token 直连| CG`

const SEQUENCE = `sequenceDiagram
  participant B as 浏览器
  participant S as Grasp Server
  B->>S: 提交设备码
  S-->>B: 推送 auth.json
  Note over B,S: 401 时重新对齐`

const GO = `func getMaxAllowedTTL(now time.Time, startTime time.Time, duration, maxInstanceLength time.Duration) time.Duration {
\ttimeLeft := maxInstanceLength - now.Sub(startTime)
\tif timeLeft <= 0 {
\t\treturn 0
\t}

\treturn min(timeLeft, duration)
}`

const REPLY = `OpenAI 打交道的那一层换成 \`codex app-server\`，沙箱侧和产品层保持不变。

### Grasp 现状

\`\`\`mermaid
${FLOW}
\`\`\`

上图之后的段落保持正常间距。

\`\`\`go
${GO}
\`\`\``

const ARTIFACT = `# 设计文档

\`\`\`mermaid
${SEQUENCE}
\`\`\`

语法错误的图保留源码：

\`\`\`mermaid
flowchart LR
  A -->
\`\`\`

\`\`\`ts
const lessThanEntity = '&lt;'
\`\`\`
`

const STREAM_HEAD = `流式输出中：\n\n\`\`\`mermaid\nflowchart LR\n  A[开始] --> B{判断}\n  B -->|是| C[完成]`

async function bootstrap() {
  await initLocale()
  setTheme(new URLSearchParams(location.search).get('theme') === 'light' ? 'light' : 'dark')
  createApp({
    setup() {
      const streamText = ref(STREAM_HEAD)
      const cache: MarkdownBlockCache = new Map()
      const streamBlocks = computed(() => renderChatMarkdownBlocks(streamText.value, cache))
      return () =>
        h('main', { class: 'mx-auto max-w-[960px] space-y-8 p-5' }, [
          h('div', { class: 'flex gap-2' }, [
            h('button', { type: 'button', class: 'rounded border border-line px-2 py-1', 'data-testid': 'theme-light', onClick: () => setTheme('light') }, 'light'),
            h('button', { type: 'button', class: 'rounded border border-line px-2 py-1', 'data-testid': 'theme-dark', onClick: () => setTheme('dark') }, 'dark'),
          ]),
          h('section', { class: 'chat-thread rounded-lg border border-line p-4', 'data-testid': 'chat' }, [
            h(AgentTimeline, { parts: [{ kind: 'message', text: REPLY }], completed: true }),
          ]),
          h('section', { class: 'chat-thread rounded-lg border border-line p-4', 'data-testid': 'stream' }, [
            h('div', { class: 'chat-prose md agent-live-message' }, [h(StreamMarkdown, { blocks: streamBlocks.value })]),
            h('button', {
              type: 'button',
              class: 'mt-2 rounded border border-line px-2 py-1',
              'data-testid': 'stream-close',
              onClick: () => { streamText.value = `${STREAM_HEAD}\n  B -->|否| A\n\`\`\`\n\n继续输出。` },
            }, 'close fence'),
          ]),
          h('section', { class: 'rounded-lg border border-line p-4', 'data-testid': 'artifact' }, [
            h('div', { class: 'md mx-auto max-w-3xl', innerHTML: renderMarkdown(ARTIFACT) }),
          ]),
        ])
    },
  }).use(i18n).mount('#app')
}

void bootstrap()
