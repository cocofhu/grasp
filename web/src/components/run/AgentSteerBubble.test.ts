// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentSteerBubble from './AgentSteerBubble.vue'

function mountBubble(props: Record<string, unknown>) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(AgentSteerBubble, {
    props: { label: '引导', text: '', ...props },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
      },
    },
  })
}

describe('AgentSteerBubble', () => {
  it('renders markdown blocks inside the shared steer bubble', () => {
    const w = mountBubble({ text: '**加粗**\n\n- 第一项\n- 第二项\n\n```sh\nnpm test\n```' })
    expect(w.get('.agent-steer-markdown strong').text()).toBe('加粗')
    expect(w.findAll('.agent-steer-markdown li').map((item) => item.text())).toEqual(['第一项', '第二项'])
    expect(w.get('.agent-steer-markdown pre').text()).toContain('npm test')
    expect(w.get('.agent-steer-body').classes()).toContain('agent-steer-body-block')
  })

  it('shows image attachments carried by a steer event', () => {
    const w = mountBubble({
      text: '[附件] 图片 ×1',
      imageUrls: ['data:image/png;base64,AAAA'],
    })
    const image = w.get('[data-testid="agent-steer-image-0"] img')
    expect(image.attributes('src')).toBe('data:image/png;base64,AAAA')
    expect(w.get('[data-testid="agent-steer-attachments"]').exists()).toBe(true)
    expect(w.get('.agent-steer-markdown').text()).toBe('')
  })

  it('renders an image embedded in Markdown', () => {
    const w = mountBubble({ text: '![diagram](data:image/png;base64,AAAA)' })
    expect(w.get('.agent-steer-markdown img').attributes('src')).toBe('data:image/png;base64,AAAA')
  })
})
