// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import PreflightView from './PreflightView.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: {
    'zh-CN': {
      pages: {
        product: {
          preflight: {
            filename: 'preflight.json',
            confirmed: '已确认',
            unconfirmed: '未确认',
            fields: '环境字段',
            labelCol: '标签',
            valueCol: '值',
            unresolved: '未决项',
            verified: '已核验',
            unverified: '未核验',
            verification: '核验方式',
            source: '来源',
            notes: '备注',
            rawJson: '原始 JSON',
          },
        },
      },
    },
  },
})

describe('PreflightView', () => {
  it('shows filename, confirmed badge, plaintext field values, and expandable raw JSON', async () => {
    const wrapper = mount(PreflightView, {
      global: { plugins: [i18n], stubs: { Icon: true } },
      props: {
        doc: {
          summary: '核对通过',
          confirmed: true,
          fields: [
            {
              name: 'db_password',
              label: '数据库密码',
              value: 's3cret!',
              verified: true,
              verification: 'user_attested',
              source: 'form',
              notes: '当面确认',
            },
          ],
        },
      },
    })

    expect(wrapper.text()).toContain('preflight.json')
    expect(wrapper.get('[data-testid="preflight-confirmed"]').text()).toContain('已确认')
    expect(wrapper.get('[data-testid="preflight-summary"]').text()).toBe('核对通过')
    expect(wrapper.text()).toContain('数据库密码')
    expect(wrapper.text()).toContain('s3cret!')
    expect(wrapper.text()).not.toMatch(/\*{3,}|•{3,}/)

    await wrapper.get('[data-testid="preflight-raw-toggle"]').trigger('click')
    expect(wrapper.get('[data-testid="preflight-raw-json"]').text()).toContain('s3cret!')
  })
})
