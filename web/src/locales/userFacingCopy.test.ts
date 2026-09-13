import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import zhCommon from '@/locales/zh-CN/common.json'
import zhPages from '@/locales/zh-CN/pages.json'
import zhMcp from '@/locales/zh-CN/mcp.json'
import zhNav from '@/locales/zh-CN/nav.json'
import zhRoute from '@/locales/zh-CN/route.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import enMcp from '@/locales/en/mcp.json'
import enNav from '@/locales/en/nav.json'
import enRoute from '@/locales/en/route.json'

describe('user-facing copy remediation keys', () => {
  const zh = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...zhCommon, ...zhPages, ...zhMcp } },
  })
  const en = createI18n({
    legacy: false,
    locale: 'en',
    messages: { en: { ...enCommon, ...enPages, ...enMcp } },
  })

  it('admin list async-state copy matches Demo lock (creating/retry/four-state)', () => {
    expect(zh.global.t('common.buttons.creating')).toBe('创建中…')
    expect(en.global.t('common.buttons.creating')).toBe('Creating…')
    expect(zh.global.t('common.buttons.retry')).toBe('重试')
    expect(en.global.t('common.buttons.retry')).toBe('Retry')
    expect(zh.global.t('common.asyncState.loadFailedTitle')).toBe('加载失败')
    expect(en.global.t('common.asyncState.loadFailedTitle')).toBe('Failed to load')
    expect(zh.global.t('common.asyncState.loadFailedDesc')).toBe('无法获取列表，请稍后重试。')
    expect(en.global.t('common.asyncState.loadFailedDesc')).toBe('Could not fetch the list. Please retry.')
    expect(zh.global.t('common.asyncState.permissionDeniedTitle')).toBe('权限不足')
    expect(en.global.t('common.asyncState.permissionDeniedTitle')).toBe('Permission denied')
    expect(zh.global.t('common.asyncState.permissionDeniedDesc')).toBe(
      '你没有查看此资源的权限，可重试或联系管理员。',
    )
    expect(en.global.t('common.asyncState.permissionDeniedDesc')).toBe(
      'You do not have access to this resource. Retry or contact an admin.',
    )
    expect(zh.global.t('common.buttons.deleting')).toBe('删除中…')
    expect(en.global.t('common.buttons.deleting')).toBe('Deleting…')
    expect(zh.global.t('common.buttons.saving')).toBe('保存中…')
    expect(en.global.t('common.buttons.saving')).toBe('Saving…')
  })

  it('attachment fallback is neutral without 仅* / only', () => {
    expect(zh.global.t('pages.projectDetail.pm.imagesOnly')).toBe('附件')
    expect(zh.global.t('pages.agentChatTester.imagesOnly')).toBe('附件')
    expect(en.global.t('pages.projectDetail.pm.imagesOnly')).toBe('Attachments')
    expect(zh.global.t('pages.projectDetail.pm.imagesOnly')).not.toMatch(/仅/)
    expect(en.global.t('pages.projectDetail.pm.imagesOnly')).not.toMatch(/only/i)
    expect(zh.global.t('pages.projectDetail.pm.inputPh')).toContain('50 MiB')
  })

  it('chat-test entry copy points to Agent Studio Chat test (g2.3)', () => {
    expect(zh.global.t('pages.agentChatTester.missingCreateTest')).toContain('Agent Studio')
    expect(zh.global.t('pages.agentChatTester.missingCreateTest')).toContain('对话测试')
    expect(en.global.t('pages.agentChatTester.missingCreateTest')).toMatch(/Agent Studio/i)
    expect(en.global.t('pages.agentChatTester.missingCreateTest')).toMatch(/Chat test/i)
    expect(zh.global.t('pages.sandboxes.empty')).toContain('Agent Studio「对话测试」')
    expect(en.global.t('pages.sandboxes.empty')).toMatch(/Agent Studio → Chat test/i)
    expect(zh.global.t('common.empty.noSandboxes')).toContain('Agent Studio「对话测试」')
    expect(en.global.t('common.empty.noSandboxes')).toMatch(/Agent Studio → Chat test/i)
    expect(zh.global.t('pages.projectDetail.sharedAgent.extendHint')).toContain('Agent Studio「对话测试」')
    expect(zh.global.t('pages.projectDetail.sharedAgent.extendHint')).not.toContain('仅在此入口')
    expect(en.global.t('pages.projectDetail.sharedAgent.extendHint')).toMatch(/Agent Studio Chat test/i)
    expect(en.global.t('pages.projectDetail.sharedAgent.extendHint')).not.toMatch(/only from this panel/i)
    expect(zh.global.t('pages.agentStudio.tabs.test')).toBe('对话测试')
    expect(en.global.t('pages.agentStudio.tabs.test')).toBe('Chat test')
  })

  it('pm status/error copy drops internal jargon', () => {
    expect(zh.global.t('pages.projectDetail.pm.busyStreaming')).not.toMatch(/Steam/i)
    expect(zh.global.t('pages.projectDetail.pm.failSandboxDesc')).not.toMatch(/自动收场/)
    expect(zh.global.t('pages.projectDetail.pm.failEmptyDesc')).not.toMatch(/未落库|空气泡/)
    expect(zh.global.t('pages.projectDetail.pm.failUnknownDesc')).not.toMatch(/无法归入/)
  })

  // Freeze contract: Approve opening hint stays exact zh「请先描述目标…」(en existing).
  // Scope is this placeholder only — skipInputPlaceholder is intentionally not locked here.
  it('approve empty chat asks the user to state the goal first', () => {
    expect(zh.global.t('pages.clarify.approveInputPlaceholder')).toBe('请先描述目标…')
    expect(en.global.t('pages.clarify.approveInputPlaceholder')).toMatch(/goal first/i)
    expect(zh.global.t('pages.clarify.approveEmptyHint')).toContain('先说明本次要做的目标')
    expect(en.global.t('pages.clarify.approveEmptyHint')).toMatch(/goal first/i)
  })

  it('clarify empty-fail retry copy is user-facing (plan g1.1)', () => {
    expect(zh.global.t('pages.clarify.emptyFailTitle')).toBe('本轮没有输出')
    expect(zh.global.t('pages.clarify.retry')).toBe('重试')
    expect(en.global.t('pages.clarify.emptyFailTitle')).toMatch(/no output/i)
    expect(en.global.t('pages.clarify.retry')).toBe('Retry')
  })

  it('run list page title is 运行记录 not the terse 运行', () => {
    expect(zh.global.t('pages.runList.title')).toBe('运行记录')
    expect(en.global.t('pages.runList.title')).toBe('Run history')
    expect(zh.global.t('pages.runList.subtitle')).toBe('按项目、流水线与状态筛选')
    expect(zh.global.t('common.table.title')).toBe('标题')
    expect(zhRoute.route.runs).toBe('运行记录')
    expect(enRoute.route.runs).toBe('Run history')
  })

  it('uses Start and Needs attention consistently in navigation and page titles', () => {
    expect(zhNav.nav.dashboard).toBe('开始')
    expect(zhRoute.route.dashboard).toBe('开始')
    expect(zhNav.nav.gates).toBe('待办')
    expect(zhRoute.route.gates).toBe('待办')
    expect(zh.global.t('pages.gatesInbox.title')).toBe('待办')
    expect(enNav.nav.dashboard).toBe('Start')
    expect(enRoute.route.dashboard).toBe('Start')
    expect(enNav.nav.gates).toBe('Needs attention')
    expect(enRoute.route.gates).toBe('Needs attention')
    expect(en.global.t('pages.gatesInbox.title')).toBe('Needs attention')
  })

  it('uses 智能体 for zh-CN agent nav/route/page title and keeps body Agent 管理 (g1.1)', () => {
    expect(zhNav.nav.agents).toBe('智能体')
    expect(zhRoute.route.agents).toBe('智能体')
    expect(zh.global.t('pages.agentStudio.title')).toBe('智能体')
    expect(enNav.nav.agents).toBe('Agent studio')
    expect(enRoute.route.agents).toBe('Agent studio')
    expect(zh.global.t('pages.agentStudio.org.manageTitle')).toBe('Agent 管理')
    expect(zh.global.t('pages.agentStudio.org.gotoManage')).toBe('前往 Agent 管理')
  })

  it('human gate canvas subtitle avoids unconditional ReAct promise', () => {
    expect(zh.global.t('pages.workflowEditor.canvas.humanGateSubtitle')).toBe('人工审批')
    expect(zh.global.t('pages.workflowEditor.canvas.appPreviewSubtitle')).toContain('等待人工确认')
    expect(zh.global.t('pages.workflowEditor.canvas.humanGateSubtitle')).not.toMatch(/ReAct/)
  })

  it('inbox first-load hint is distinct from empty and run-loading copy', () => {
    expect(zh.global.t('pages.gatesInbox.listLoadingHint')).toBe('列表加载后可选择一项')
    expect(en.global.t('pages.gatesInbox.listLoadingHint')).toBe('Select an item after the list loads')
    expect(zh.global.t('pages.gatesInbox.listLoadingHint')).not.toBe(zh.global.t('common.empty.noPendingGates'))
    expect(zh.global.t('pages.gatesInbox.listLoadingHint')).not.toBe(zh.global.t('pages.gatesInbox.loadingRun'))
    expect(zh.global.t('pages.gatesInbox.detailPane')).toBe('详情')
    expect(en.global.t('pages.gatesInbox.detailPane')).toBe('Details')
    expect(zh.global.t('common.empty.noPendingGates')).toBe('没有待审批项')
    expect(zh.global.t('common.empty.noPendingGatesForPipeline')).toBe('该流水线没有待审批项')
    expect(zh.global.t('common.empty.noPendingGatesDesc')).toBe(
      '当工作流到达人工门禁或需求澄清节点时会出现在这里',
    )
    expect(zh.global.t('common.empty.noPendingGatesPipelineDesc')).toBe(
      '试试选择其他流水线,或查看全部流水线',
    )
  })

  it('gate share-link copy is bilingual without hardcoded jargon', () => {
    expect(zh.global.t('pages.gatesInbox.share.copyLink')).toBe('复制临时链接')
    expect(en.global.t('pages.gatesInbox.share.copyLink')).toBe('Copy temp link')
    expect(zh.global.t('pages.gatesInbox.share.safetyHint')).toContain('信任')
    expect(zh.global.t('pages.gatesInbox.share.safetyHint')).toContain('审批工作台')
    expect(zh.global.t('pages.gatesInbox.share.safetyHint')).toContain('可取点')
    expect(zh.global.t('pages.gatesInbox.share.safetyHint')).not.toMatch(/不是内部审批工作台|不可取点|外部一次确认页/)
    expect(en.global.t('pages.gatesInbox.share.safetyHint')).toMatch(/trust/i)
    expect(en.global.t('pages.gatesInbox.share.safetyHint')).toMatch(/workbench/i)
    expect(en.global.t('pages.gatesInbox.share.safetyHint')).not.toMatch(/cannot pick elements|one-time external confirm page/i)
    expect(zh.global.t('pages.gatesInbox.share.copied')).toBe('已复制到剪贴板')
    expect(en.global.t('pages.gatesInbox.share.copied')).toBe('Copied to clipboard')
    expect(zh.global.t('pages.gatesInbox.share.autoCopied')).toBe('已自动复制新链接')
    expect(en.global.t('pages.gatesInbox.share.autoCopied')).toBe('New link copied automatically')
    expect(zh.global.t('pages.gatesInbox.share.regenerated')).toBe('链接已重新生成')
    expect(en.global.t('pages.gatesInbox.share.regenerated')).toBe('Link regenerated')
    expect(zh.global.t('pages.gatesInbox.share.clipboardFallback')).toContain('无法写入剪贴板')
    expect(en.global.t('pages.gatesInbox.share.clipboardFallback')).toMatch(/clipboard unavailable/i)
    expect(zh.global.t('pages.gatesInbox.share.copyUnavailable')).toContain('重新生成或撤销')
    expect(en.global.t('pages.gatesInbox.share.copyUnavailable')).toMatch(/regenerate or revoke/i)
    expect(zh.global.t('pages.gatesInbox.share.originFromAccessHint')).toContain('当前访问')
    expect(en.global.t('pages.gatesInbox.share.originFromAccessHint')).toMatch(/admin URL|public base/i)
    expect(zh.global.t('pages.gatesInbox.share.loopbackWarning')).toMatch(/环回|外部不可达/)
    expect(en.global.t('pages.gatesInbox.share.loopbackWarning')).toMatch(/loopback/i)
    expect(zh.global.t('pages.gatesInbox.share.loopbackCopyBlocked')).toContain('不可复制')
    expect(en.global.t('pages.gatesInbox.share.loopbackCopyBlocked')).toMatch(/cannot be copied/i)
    expect(zh.global.t('pages.gatesInbox.share.errors.noStandardAction')).toContain('标准')
    expect(en.global.t('pages.gatesInbox.share.errors.noStandardAction')).toMatch(/approve or reject/i)
    expect(zh.global.t('pages.publicGate.badge')).toBe('外部一次决策')
    expect(en.global.t('pages.publicGate.badge')).toBe('One-time external decision')
    expect(zh.global.t('pages.publicGate.badgeReview')).toBe('外部复审')
    expect(en.global.t('pages.publicGate.badgeReview')).toBe('External review')
    expect(zh.global.t('pages.publicGate.badgeClarify')).toBe('待澄清')
    expect(en.global.t('pages.publicGate.badgeClarify')).toBe('Pending clarification')
    expect(zh.global.t('pages.publicGate.kindHintClarify')).toBe('外部澄清')
    expect(zh.global.t('pages.publicGate.confirmHint')).toContain('不触发 Agent')
    expect(zh.global.t('pages.publicGate.confirmHintClarify')).not.toContain('不触发 Agent')
    expect(zh.global.t('pages.gatesInbox.share.errors.notReviewSession')).toContain('待澄清')
    expect(zh.global.t('pages.publicGate.heading')).toBe('审批工作台')
    expect(en.global.t('pages.publicGate.heading')).toBe('Approval workbench')
    expect(zh.global.t('pages.publicGate.visualProduct')).toBe('视觉网页产物')
    expect(zh.global.t('pages.publicGate.approve')).toBe('确认并流转')
    expect(en.global.t('pages.publicGate.approve')).toMatch(/Confirm and advance/i)
    expect(zh.global.t('pages.publicGate.reject')).toBe('驳回')
    expect(en.global.t('pages.publicGate.reject')).toBe('Reject')
    expect(zh.global.t('pages.publicGate.doneApproved')).toBe('已确认')
    expect(zhRoute.route.publicGateApproval).toBe('外部一次决策')
    expect(enRoute.route.publicGateApproval).toBe('One-time external decision')
    expect(zh.global.t('pages.publicGate.confirm')).toBe('确认并流转')
    expect(en.global.t('pages.publicGate.confirm')).toMatch(/Confirm and advance/i)
    expect(zh.global.t('pages.publicGate.doneConfirmed')).toBe('已确认')
    expect(en.global.t('pages.publicGate.doneConfirmed')).toBe('Confirmed')
    expect(zh.global.t('pages.gatesInbox.share.errors.reviewBusy')).toContain('复审进行中')
    expect(zh.global.t('pages.publicGate.busy')).toContain('复审进行中')
    expect(zh.global.t('pages.publicGate.validationFailed')).toContain('产物校验')
    expect(zh.global.t('pages.publicGate.confirming')).toBe('正在确认…')
    expect(zh.global.t('pages.publicGate.securityCheckFailed')).toBe('安全校验未通过，请再试一次「确认并流转」')
    expect(zh.global.t('pages.publicGate.linkInvalid')).toBe('链接失效，请重新打开复审链接')
    expect(zh.global.t('pages.publicGate.networkFault')).toBe('网络故障，请检查网络后重试')
    expect(zh.global.t('pages.publicGate.rateLimited')).toBe('请求过于频繁，请稍后再试')
    expect(zh.global.t('pages.publicGate.submitting')).toBe('提交中…')
    expect(zh.global.t('pages.projectDetail.audit.callerExternal')).toBe('外部')
    expect(en.global.t('pages.projectDetail.audit.callerExternal')).toBe('External')
  })

  it('integrations subtitle is the live mcp key without env/template pile-up', () => {
    const sub = zh.global.t('mcp.integrations.subtitle')
    expect(sub).toContain('MCP')
    expect(sub).not.toMatch(/GITLAB_/)
    expect(sub).not.toMatch(/\$\{vars/)
    expect(sub).not.toMatch(/作用域注入/)
  })

  it('token empty-state drops Usage/分桶/bridge/回填 jargon', () => {
    expect(zh.global.t('pages.board.tokenStats.emptyTrendHint')).not.toMatch(/Usage|分桶|bridge|回填/)
    expect(zh.global.t('pages.board.tokenStats.emptyRankHint')).not.toMatch(/Usage|分桶|bridge|回填/)
    expect(zh.global.t('pages.board.tokenStats.filledTag')).not.toMatch(/回填/)
  })

  it('unknown model display name settings copy avoids 分桶 jargon', () => {
    const keys = [
      'pages.projectDetail.unknownModelDisplayNameLabel',
      'pages.projectDetail.unknownModelDisplayNamePlaceholder',
      'pages.projectDetail.unknownModelDisplayNameHelp',
      'pages.projectDetail.unknownModelDisplayNameClear',
      'pages.projectDetail.metaHint',
    ]
    for (const k of keys) {
      expect(zh.global.t(k)).not.toMatch(/分桶/)
      expect(en.global.t(k)).not.toMatch(/unbucket/i)
    }
    expect(zh.global.t('pages.projectDetail.unknownModelDisplayNameLabel')).toBe('未知模型显示名')
    expect(zh.global.t('pages.tokenByModel.unknownBadge')).toBe('未知')
  })

  it('model rank card copy has no unknown≠other hint (g1.3)', () => {
    expect(zh.global.te('pages.board.tokenStats.modelRankHint')).toBe(false)
    expect(en.global.te('pages.board.tokenStats.modelRankHint')).toBe(false)

    const zhRank = [
      zh.global.t('pages.board.tokenStats.modelRankTitle'),
      zh.global.t('pages.board.tokenStats.modelRankSub'),
      zh.global.t('pages.board.tokenStats.modelOther'),
      zh.global.t('pages.board.tokenStats.emptyModelRankHint'),
    ].join('\n')
    const enRank = [
      en.global.t('pages.board.tokenStats.modelRankTitle'),
      en.global.t('pages.board.tokenStats.modelRankSub'),
      en.global.t('pages.board.tokenStats.modelOther'),
      en.global.t('pages.board.tokenStats.emptyModelRankHint'),
    ].join('\n')

    expect(zhRank).toContain('模型消耗排行')
    expect(zhRank).toContain('Top10 · 其余 → other')
    expect(zhRank).toContain('other（其余模型）')
    expect(zhRank).not.toMatch(/未知\s*[≠不等].*other|与 other 不同|不是 other/)
    expect(enRank).toContain('Model usage ranking')
    expect(enRank).toContain('Top10 · rest → other')
    expect(enRank).toContain('other (remaining models)')
    expect(enRank).not.toMatch(/Unknown is not the same as other/i)
    expect(enRank).not.toMatch(/Unknown.*≠.*other/i)
  })

  it('user-facing product naming uses 项目管理 / Project Management, not PM', () => {
    const zhKeys = [
      'common.runTrigger.pmMcp',
      'pages.projectDetail.tokenTipPm',
      'pages.board.tokenStats.pm',
      'pages.projectDetail.pm.enabledMcps',
      'pages.agentStudio.dialogs.renameCascadeHint',
      'pages.agentStudio.data.context.hint',
      'mcp.pmProgress.desc',
      'mcp.pmProgress.convention',
      'mcp.pmWorkflowRead.desc',
      'mcp.pmWorkflowRead.convention',
      'mcp.pmWorkflowWrite.desc',
      'mcp.pmWorkflowWrite.convention',
      'mcp.pmAgentFs.desc',
      'mcp.pmAgentFs.convention',
      'mcp.pmPrdManager.desc',
      'mcp.pmPrdManager.convention',
    ] as const
    for (const key of zhKeys) {
      const text = zh.global.t(key)
      expect(text, key).toMatch(/项目管理/)
      expect(text, key).not.toMatch(/(?<![A-Za-z0-9_-])PM(?![A-Za-z0-9_-])/)
    }

    const enKeys = [
      'common.runTrigger.pmMcp',
      'pages.projectDetail.tokenTipPm',
      'pages.board.tokenStats.pm',
      'pages.projectDetail.pm.enabledMcps',
      'pages.projectDetail.pm.gateAutoVar',
      'pages.agentStudio.dialogs.renameCascadeHint',
      'pages.agentStudio.data.context.hint',
      'mcp.pmProgress.desc',
      'mcp.pmProgress.convention',
      'mcp.pmWorkflowRead.desc',
      'mcp.pmWorkflowRead.convention',
      'mcp.pmWorkflowWrite.desc',
      'mcp.pmWorkflowWrite.convention',
      'mcp.pmAgentFs.desc',
      'mcp.pmAgentFs.convention',
      'mcp.pmPrdManager.desc',
      'mcp.pmPrdManager.convention',
    ] as const
    for (const key of enKeys) {
      const text = en.global.t(key)
      expect(text, key).toMatch(/Project Management/)
      expect(text, key).not.toMatch(/(?<![A-Za-z0-9_-])PM(?![A-Za-z0-9_-])/)
      expect(text, key).not.toMatch(/PM-only|project PM/i)
    }

    expect(zh.global.t('common.runTrigger.pmMcp')).toBe('项目管理 MCP')
    expect(en.global.t('common.runTrigger.pmMcp')).toBe('Project Management MCP')
    expect(zh.global.t('pages.projectDetail.tokenTipWorkflow')).toBe('工作流')
    expect(en.global.t('pages.projectDetail.tokenTipWorkflow')).toBe('Workflow')

    // Token source visible copy: 工作流 / Workflow + 项目管理 / Project Management (exact Title Case)
    expect(zh.global.t('pages.board.tokenStats.workflow')).toBe('工作流')
    expect(zh.global.t('pages.projectDetail.tokenTipWorkflow')).toBe('工作流')
    expect(zh.global.t('pages.board.tokenStats.pm')).toBe('项目管理')
    expect(en.global.t('pages.board.tokenStats.workflow')).toBe('Workflow')
    expect(en.global.t('pages.projectDetail.tokenTipWorkflow')).toBe('Workflow')
    expect(en.global.t('pages.board.tokenStats.pm')).toBe('Project Management')
    // Do not verify Title Case with case-insensitive /workflow/ substring
    expect(en.global.t('pages.board.tokenStats.workflow')).not.toBe('workflow')
    expect(en.global.t('pages.projectDetail.tokenTipWorkflow')).not.toBe('workflow')

    expect(zh.global.t('pages.appPreview.novnc.inspect')).toBe('取点标注')
    expect(zh.global.t('pages.appPreview.novnc.cancelInspect')).toBe('取消标注')
    expect(en.global.t('pages.appPreview.novnc.inspect')).toBe('Pick to annotate')
    expect(en.global.t('pages.appPreview.novnc.cancelInspect')).toBe('Cancel annotation')
    expect(zh.global.t('pages.appPreview.novnc.cancelInspect')).not.toMatch(/取消取点/)

    expect(zh.global.t('pages.platformRules.subtitle')).toMatch(/10 个规则文件/)
    expect(en.global.t('pages.platformRules.subtitle')).toMatch(/10 rule files/)
    expect(zh.global.t('pages.platformRules.fileListDesc')).toMatch(/全部 10 个/)
    expect(en.global.t('pages.platformRules.fileListDesc')).toMatch(/All 10/)

    const tokenSourceNoPmKeys = [
      'pages.board.tokenStats.workflow',
      'pages.projectDetail.tokenTipWorkflow',
      'pages.board.tokenStats.pm',
    ] as const
    for (const key of tokenSourceNoPmKeys) {
      expect(zh.global.t(key), key).not.toMatch(/(?<![A-Za-z0-9_-])PM(?![A-Za-z0-9_-])/)
      expect(en.global.t(key), key).not.toMatch(/(?<![A-Za-z0-9_-])PM(?![A-Za-z0-9_-])/)
    }

    // MCP server ids stay as protocol names
    expect(zh.global.t('mcp.pmProgress.name')).toBe('pm-progress')
    expect(en.global.t('mcp.pmProgress.name')).toBe('pm-progress')
  })
})
