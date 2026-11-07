import '../src/styles/global.css'
import { createApp, defineComponent, h } from 'vue'
import { i18n } from '../src/lib/shared/i18n'
import { initLocale } from '../src/lib/shared/locale'
import { setTheme } from '../src/lib/shared/theme'
import ArtifactFileAttachments from '../src/components/chat/ArtifactFileAttachments.vue'
import type { ArtifactAttachment } from '../src/lib/shared/types'

initLocale()
setTheme('light')
const files: ArtifactAttachment[] = [
  { artifactId: 'report', revision: 1, kind: 'markdown', name: '报告.md', sizeBytes: 48, caption: '固定发送时的版本' },
  { artifactId: 'image', revision: 1, kind: 'image', name: '图片.png', sizeBytes: 100 },
  { artifactId: 'video', revision: 1, kind: 'video', name: '视频.mp4', sizeBytes: 500000 },
  { artifactId: 'audio', revision: 1, kind: 'audio', name: '音频.wav', sizeBytes: 8044 },
  { artifactId: 'pdf', revision: 1, kind: 'pdf', name: '文档.pdf', sizeBytes: 500 },
  { artifactId: 'zip', revision: 1, kind: 'file', name: '资料.zip', sizeBytes: 12 },
  { artifactId: 'missing', revision: 1, kind: 'text', name: '缺失.txt', sizeBytes: 10 },
]
createApp(defineComponent({ setup: () => () => h('main', { class: 'mx-auto max-w-3xl p-4' }, [h(ArtifactFileAttachments, { files })]) })).use(i18n).mount('#app')
