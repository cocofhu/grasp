/**
 * Run→QQ notify message templates (project-level).
 * Placeholder keys and default/fallback rendering must stay aligned with
 * server/internal/services/run_notify.go (ReplaceRunNotifyPlaceholders /
 * FormatRunNotifyMessage / RenderRunNotifyMessage).
 */

export type RunNotifyKind = 'waiting_human' | 'failed' | 'completed'

export const RUN_NOTIFY_PLACEHOLDERS = [
  '{project}',
  '{workflow}',
  '{run_id}',
  '{node}',
  '{link}',
  '{title}',
] as const

export type RunNotifyPlaceholder = (typeof RUN_NOTIFY_PLACEHOLDERS)[number]

export interface RunNotifyPreviewContext {
  project: string
  workflow: string
  runId: string
  node: string
  link: string
}

/** Fake sample used by the notify Tab live preview (not a real push). */
export const RUN_NOTIFY_PREVIEW_FAKE: RunNotifyPreviewContext = {
  project: 'grasp-demo',
  workflow: 'gate-main',
  runId: 'run-4c9100d0',
  node: '人工门禁',
  link: 'https://approving.example/runs/run-4c9100d0',
}

export function runNotifyTitle(kind: RunNotifyKind): string {
  switch (kind) {
    case 'waiting_human':
      return '等待人工处理'
    case 'completed':
      return '运行完成'
    default:
      return '运行失败'
  }
}

/**
 * Editable skeleton equivalent to the default body (with placeholders).
 * Filling + saving this counts as a custom non-empty template (no auto line-omit).
 */
export function defaultEditableRunNotifyTemplate(_kind: RunNotifyKind): string {
  return [
    '【Grasp】{title}',
    '项目：{project}',
    '工作流：{workflow}',
    'Run：{run_id}',
    '节点：{node}',
    '打开：{link}',
  ].join('\n')
}

/** Literal six-key replace; unknown tokens stay as-is. */
export function replaceRunNotifyPlaceholders(
  tmpl: string,
  ctx: {
    project: string
    workflow: string
    runId: string
    node: string
    link: string
    title: string
  },
): string {
  return tmpl
    .split('{project}')
    .join(ctx.project)
    .split('{workflow}')
    .join(ctx.workflow)
    .split('{run_id}')
    .join(ctx.runId)
    .split('{node}')
    .join(ctx.node)
    .split('{link}')
    .join(ctx.link)
    .split('{title}')
    .join(ctx.title)
}

/**
 * Server FormatRunNotifyMessage equivalent (omits「节点：」line when node is empty).
 */
export function formatDefaultRunNotifyMessage(
  kind: RunNotifyKind,
  ctx: RunNotifyPreviewContext,
): string {
  const title = runNotifyTitle(kind)
  const project = ctx.project.trim() || '—'
  const workflow = ctx.workflow.trim() || '—'
  const lines = [
    `【Grasp】${title}`,
    `项目：${project}`,
    `工作流：${workflow}`,
    `Run：${ctx.runId}`,
  ]
  const node = ctx.node.trim()
  if (node) {
    lines.push(`节点：${node}`)
  }
  lines.push(`打开：${ctx.link}`)
  return lines.join('\n')
}

/**
 * Preview / client-side mirror of RenderRunNotifyMessage:
 * trim-empty template → default formatter; else literal replace (empty node → "").
 */
export function renderRunNotifyMessage(
  kind: RunNotifyKind,
  template: string | undefined | null,
  ctx: RunNotifyPreviewContext = RUN_NOTIFY_PREVIEW_FAKE,
): string {
  if (!template || !String(template).trim()) {
    return formatDefaultRunNotifyMessage(kind, ctx)
  }
  return replaceRunNotifyPlaceholders(String(template), {
    project: ctx.project.trim() || '—',
    workflow: ctx.workflow.trim() || '—',
    runId: ctx.runId,
    node: ctx.node.trim(), // custom path: no line delete
    link: ctx.link,
    title: runNotifyTitle(kind),
  })
}
