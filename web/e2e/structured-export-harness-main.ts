import '../src/styles/global.css'
import { createApp, defineComponent, h, ref } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale, setLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import { installIdleScrollbar } from '../src/lib/shared/idleScrollbar'
import ArtifactPreview from '../src/components/run/ArtifactPreview.vue'
import type { Artifact } from '../src/lib/shared/types'
import { api } from '../src/lib/api/api'

const params = new URLSearchParams(location.search)
const scenario = params.get('scenario') || 'structured'
const theme = params.get('theme') || 'dark'
const locale = params.get('locale') || 'zh-CN'

const clarifiedDoc = {
  title: '结构化 JSON 产物预览导出 PNG / PDF',
  summary: '在 ArtifactPreview 对结构化预览新增样式化导出；保留复制与原始 JSON 下载。',
  background: '用户需要按预览样式下载 PNG/PDF。',
  goals: ['导出 PNG', '导出 PDF', '保留原始 JSON 下载'],
  in_scope: ['inline 与 Zoom 并列文字按钮', '所见 DOM 完整捕获'],
  out_of_scope: ['服务端渲染', '替换原始下载'],
  personas: [
    { id: 'u1', name: '工作流审查者', description: '导出带样式文档' },
    { id: 'u2', name: '实现/测试工程师', description: '验证入口与文件名' },
  ],
  functional_requirements: [
    {
      title: '样式化导出',
      detail: '下载图片与下载 PDF',
      acceptance_criteria: ['视觉接近预览'],
    },
  ],
  assumptions: ['无额外假设(已与用户确认)'],
  dependencies: ['无额外依赖(已与用户确认)'],
  constraints: ['无额外约束(已与用户确认)'],
}

const structuredArtifact: Artifact = {
  id: 'art-structured-1',
  name: 'clarified_requirement.json',
  kind: 'json',
  nodeId: 'react',
  runId: 'run-1',
  workflowName: '测试工作流',
  sizeBytes: 2048,
  createdAt: '2026-07-26T01:00:00Z',
}

const plainJsonArtifact: Artifact = {
  ...structuredArtifact,
  id: 'art-json-1',
  name: 'notes.json',
}

const artifact = scenario === 'plain-json' ? plainJsonArtifact : structuredArtifact
const content =
  scenario === 'plain-json'
    ? JSON.stringify({ note: '普通 JSON，不应出现样式化导出' }, null, 2)
    : JSON.stringify(clarifiedDoc)

;(api as any).artifactContent = async (id: string) => ({
  ...(id === plainJsonArtifact.id ? plainJsonArtifact : structuredArtifact),
  content,
})
;(api as any).artifactDownloadUrl = (id: string) => `http://127.0.0.1:9/api/artifacts/${id}/download`

const App = defineComponent({
  setup() {
    const current = ref(artifact)
    return () =>
      h(
        'div',
        {
          class: 'mx-auto flex h-[720px] max-w-3xl flex-col border border-line',
          'data-testid': 'export-harness-root',
        },
        [h(ArtifactPreview, { artifact: current.value, scope: 'run' })],
      )
  },
})

installIdleScrollbar()
void (async () => {
  await initLocale()
  await setLocale(locale as 'zh-CN' | 'en')
  setTheme(theme === 'light' ? 'light' : 'dark')
  createApp(App).use(i18n).mount('#app')
})()
