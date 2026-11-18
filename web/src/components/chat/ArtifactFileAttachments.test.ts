// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { i18n } from '@/lib/shared/i18n'
import type { ArtifactAttachment } from '@/lib/shared/types'
import ArtifactFileAttachments from './ArtifactFileAttachments.vue'
import FileTypeIcon from './FileTypeIcon.vue'

beforeEach(() => { i18n.global.setLocaleMessage('zh-CN', { ...common, ...pages }) })

const files: ArtifactAttachment[] = [
  { artifactId: 'img', revision: 3, kind: 'image', name: 'Fast-透明背景.png', sizeBytes: 14029, caption: '控制面实拍' },
  { artifactId: 'report', revision: 1, kind: 'markdown', name: '报告.md', sizeBytes: 48 },
  { artifactId: 'vid', revision: 2, kind: 'video', name: '切换演示.mp4', sizeBytes: 1887437 },
  { artifactId: 'pdf', revision: 2, kind: 'pdf', name: '需求说明.pdf', sizeBytes: 500 },
  { artifactId: 'zip', revision: 1, kind: 'file', name: '资料.zip', sizeBytes: 12 },
]

function render(...args: [ArtifactAttachment[] | undefined] | []) {
  return mount(ArtifactFileAttachments, {
    props: { files: args.length ? args[0] : files },
    global: {
      plugins: [i18n],
      stubs: {
        AppModal: { props: ['open', 'title'], template: '<section v-if="open" data-testid="modal" :data-title="title"><slot /></section>' },
        ArtifactFilePreview: { props: ['file'], template: '<div data-testid="preview">{{ file.name }}</div>' },
      },
    },
  })
}
const chip = (w: ReturnType<typeof render>, index: number) => w.findAll('[data-testid="artifact-file-chip"]')[index]

describe('ArtifactFileAttachments', () => {
  it('renders every file as a chip in the given order', () => {
    const w = render()
    const chips = w.findAll('[data-testid="artifact-file-chip"]')
    expect(chips.map((c) => c.attributes('data-kind'))).toEqual(['image', 'markdown', 'video', 'pdf', 'file'])
    expect(chips[1].text()).toContain('报告.md')
    expect(chips[1].text()).toContain('48 B · v1')
    expect(chips[2].text()).toContain('1.8 MB · v2')
  })

  it.each([[[]], [undefined]])('renders nothing for %s', (list) => {
    expect(render(list).find('[data-testid="artifact-file-attachments"]').exists()).toBe(false)
  })

  it('pins the revision on the thumbnail source and download link', () => {
    const w = render()
    const img = chip(w, 0).get('img')
    expect(img.attributes('src')).toMatch(/\/api\/artifacts\/img\/raw\?revision=3$/)
    expect(img.attributes('loading')).toBe('lazy')
    const download = chip(w, 0).get('[data-testid="artifact-file-chip-download"]')
    expect(download.attributes('href')).toMatch(/\/api\/artifacts\/img\/download\?revision=3$/)
    expect(download.attributes('aria-label')).toBe('下载原文件')
  })

  it('lets small images grow to at most twice their natural size', async () => {
    const w = render()
    const img = chip(w, 0).get('img')
    Object.defineProperty(img.element, 'naturalWidth', { value: 12 })
    Object.defineProperty(img.element, 'naturalHeight', { value: 12 })
    await img.trigger('load')
    expect((img.element as HTMLImageElement).style.maxWidth).toBe('24px')
    expect((img.element as HTMLImageElement).style.maxHeight).toBe('24px')
  })

  it('shows a muted, non-autoplaying video first frame with a play badge', () => {
    const video = chip(render(), 2).get('video').element as HTMLVideoElement
    expect(video.getAttribute('src')).toMatch(/\/api\/artifacts\/vid\/raw\?revision=2$/)
    expect(video.getAttribute('preload')).toBe('metadata')
    expect(video.autoplay).toBe(false)
    expect(video.muted).toBe(true)
    expect(chip(render(), 2).find('.artifact-chip__play').exists()).toBe(true)
  })

  it('uses file type icons for other files', () => {
    const w = render()
    expect(chip(w, 1).getComponent(FileTypeIcon).props()).toEqual({ label: 'MD', tone: 'doc' })
    expect(chip(w, 3).getComponent(FileTypeIcon).props()).toEqual({ label: 'PDF', tone: 'pdf' })
    expect(chip(w, 4).getComponent(FileTypeIcon).props()).toEqual({ label: 'ZIP', tone: 'archive' })
  })

  it('falls back to a purple file icon when a thumbnail fails to load', async () => {
    const w = render()
    await chip(w, 0).get('img').trigger('error')
    await chip(w, 2).get('video').trigger('error')
    expect(chip(w, 0).find('img').exists()).toBe(false)
    expect(chip(w, 0).getComponent(FileTypeIcon).props()).toEqual({ label: 'PNG', tone: 'media' })
    expect(chip(w, 2).getComponent(FileTypeIcon).props()).toEqual({ label: 'MP4', tone: 'media' })
    await chip(w, 0).get('[data-testid="artifact-file-chip-preview"]').trigger('click')
    expect(w.get('[data-testid="modal"]').attributes('data-title')).toBe('Fast-透明背景.png')
  })

  it('opens the preview from the chip body but not from the download button', async () => {
    const w = render()
    await chip(w, 1).get('[data-testid="artifact-file-chip-download"]').trigger('click')
    expect(w.find('[data-testid="modal"]').exists()).toBe(false)
    const body = chip(w, 1).get('[data-testid="artifact-file-chip-preview"]')
    expect(body.attributes('aria-label')).toBe('预览 报告.md')
    await body.trigger('click')
    expect(w.get('[data-testid="preview"]').text()).toBe('报告.md')
  })

  it('keeps plain files download-only', async () => {
    const w = render()
    const body = chip(w, 4).get('[data-testid="artifact-file-chip-preview"]')
    expect(body.attributes()).toHaveProperty('disabled')
    expect(chip(w, 4).get('[data-testid="artifact-file-chip-download"]').classes()).toContain('artifact-chip__download--always')
    await body.trigger('click')
    expect(w.find('[data-testid="modal"]').exists()).toBe(false)
  })

  it('puts the caption in the tooltip instead of an extra line', () => {
    const w = render()
    expect(chip(w, 0).attributes('title')).toBe('Fast-透明背景.png\n控制面实拍')
    expect(chip(w, 0).text()).not.toContain('控制面实拍')
    expect(chip(w, 1).attributes('title')).toBe('报告.md')
  })
})
