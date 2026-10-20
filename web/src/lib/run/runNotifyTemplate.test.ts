import { describe, expect, it } from 'vitest'
import {
  defaultEditableRunNotifyTemplate,
  formatDefaultRunNotifyMessage,
  renderRunNotifyMessage,
  replaceRunNotifyPlaceholders,
  runNotifyTitle,
  RUN_NOTIFY_PREVIEW_FAKE,
} from './runNotifyTemplate'

describe('runNotifyTemplate', () => {
  it('formatDefault matches legacy waiting_human with node line', () => {
    const got = formatDefaultRunNotifyMessage('waiting_human', RUN_NOTIFY_PREVIEW_FAKE)
    expect(got).toBe(
      [
        '【Grasp】等待人工处理',
        '工作区：grasp-demo',
        '工作流：gate-main',
        'Run：run-4c9100d0',
        '节点：人工门禁',
        '打开：https://approving.example/runs/run-4c9100d0',
      ].join('\n'),
    )
  })

  it('formatDefault omits node line when empty', () => {
    const got = formatDefaultRunNotifyMessage('failed', {
      ...RUN_NOTIFY_PREVIEW_FAKE,
      node: '',
    })
    expect(got).toBe(
      [
        '【Grasp】运行失败',
        '工作区：grasp-demo',
        '工作流：gate-main',
        'Run：run-4c9100d0',
        '打开：https://approving.example/runs/run-4c9100d0',
      ].join('\n'),
    )
  })

  it('empty / whitespace template falls back to default', () => {
    const want = formatDefaultRunNotifyMessage('waiting_human', RUN_NOTIFY_PREVIEW_FAKE)
    expect(renderRunNotifyMessage('waiting_human', '')).toBe(want)
    expect(renderRunNotifyMessage('waiting_human', '   \n')).toBe(want)
    expect(renderRunNotifyMessage('waiting_human', null)).toBe(want)
  })

  it('custom template replaces six keys; empty node stays empty', () => {
    const tmpl = '【Grasp】{title}\n{workspace}/{workflow}\n{run_id}|{node}|{link}'
    expect(
      renderRunNotifyMessage('failed', tmpl, {
        workspace: 'P',
        workflow: 'W',
        runId: 'r',
        node: '',
        link: '/runs/r',
      }),
    ).toBe('【Grasp】运行失败\nP/W\nr||/runs/r')
  })

  it('unknown placeholders stay as-is', () => {
    expect(
      replaceRunNotifyPlaceholders('x {workspace} {nope} {title}', {
        workspace: 'P',
        workflow: 'W',
        runId: 'r',
        node: 'n',
        link: 'L',
        title: 'T',
      }),
    ).toBe('x P {nope} T')
  })

  it('completed title is 运行完成 not 运行失败', () => {
    expect(runNotifyTitle('completed')).toBe('运行完成')
    expect(formatDefaultRunNotifyMessage('completed', RUN_NOTIFY_PREVIEW_FAKE)).toContain(
      '【Grasp】运行完成',
    )
    expect(formatDefaultRunNotifyMessage('completed', RUN_NOTIFY_PREVIEW_FAKE)).not.toContain(
      '运行失败',
    )
    expect(renderRunNotifyMessage('completed', '')).toContain('【Grasp】运行完成')
  })

  it('default editable skeleton renders to default with fake data', () => {
    const editable = defaultEditableRunNotifyTemplate('waiting_human')
    expect(renderRunNotifyMessage('waiting_human', editable)).toBe(
      formatDefaultRunNotifyMessage('waiting_human', RUN_NOTIFY_PREVIEW_FAKE),
    )
  })
})
