import { test, expect } from '@playwright/test'

test.describe('human_gate 临时审批链接', () => {
  test('Inbox 复制临时链接打开管理面板且桌面按钮不高于花粒', async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: async (text: string) => {
            ;(window as unknown as { __copied?: string }).__copied = text
          },
        },
      })
    })
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/gate-share-link.html?scene=inbox')
    await expect(page.getByTestId('gate-share-e2e-root')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('gate-share-copy-btn')).toBeVisible()
    await expect(page.getByTestId('gate-share-status')).toContainText('尚未创建')
    const btnBox = await page.getByTestId('gate-share-copy-btn').boundingBox()
    const chipBox = await page.getByTestId('gate-share-status').boundingBox()
    const wrapBox = await page.getByTestId('gate-share-hit-wrap').boundingBox()
    expect(btnBox && chipBox && wrapBox).toBeTruthy()
    expect(btnBox!.height).toBeLessThanOrEqual(chipBox!.height + 8)
    // desktop: click box == visible box, not forced to 44px
    expect(Math.abs(wrapBox!.height - btnBox!.height)).toBeLessThanOrEqual(2)
    expect(wrapBox!.height).toBeLessThan(44)
    const shareTb = page.getByTestId('html-preview-share-link').first()
    const inspectTb = page.getByTestId('html-preview-inspect-toggle').first()
    await expect(shareTb).toBeVisible()
    const shareH = (await shareTb.boundingBox())?.height ?? 0
    const inspectH = (await inspectTb.boundingBox())?.height ?? 0
    expect(shareH).toBeGreaterThan(0)
    expect(Math.abs(shareH - inspectH)).toBeLessThanOrEqual(4)
    await page.getByTestId('gate-share-copy-btn').click()
    await expect(page.getByTestId('gate-share-panel-body')).toBeVisible()
    await expect(page.getByTestId('gate-share-panel-body')).toContainText('信任')
    await expect(page.getByTestId('gate-share-panel-body')).toContainText('审批工作台')
    await expect(page.getByTestId('gate-share-panel-body')).toContainText('可取点')
    await page.getByTestId('gate-share-create').click()
    await expect(page.getByTestId('gate-share-url')).toBeVisible()
    await expect(page.getByTestId('gate-share-url')).toHaveValue(/#t=••••/)
    await expect(page.getByTestId('gate-share-origin-hint')).toBeVisible()
    await expect(page.getByTestId('gate-share-loopback-warning')).toHaveCount(0)
    const copied = await page.evaluate(() => (window as unknown as { __copied?: string }).__copied || '')
    // plan g3.1 / g2.3: non-loopback fixture (approving.example.com) auto-copies
    expect(copied).toContain('https://approving.example.com/public/gate-approvals#t=')
    expect(copied).toMatch(/#t=[0-9a-f]{64}$/)
    // plan g2.2: auto-copy success uses distinct toast
    await expect(page.getByTestId('toast-host')).toContainText('已自动复制新链接')
  })

  test('非安全上下文走 legacy 复制且不堆叠失败 toast (plan g4.2 / g1 / g3)', async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(window, 'isSecureContext', {
        configurable: true,
        get: () => false,
      })
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: async () => {
            throw new Error('Clipboard API blocked in insecure context')
          },
        },
      })
      document.execCommand = ((cmd: string) => {
        if (cmd === 'copy') {
          const el = document.activeElement as HTMLTextAreaElement | null
          if (el && typeof el.value === 'string') {
            ;(window as unknown as { __copied?: string }).__copied = el.value
          }
          return true
        }
        return false
      }) as typeof document.execCommand
    })
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/gate-share-link.html?scene=inbox')
    await expect(page.getByTestId('gate-share-e2e-root')).toBeVisible({ timeout: 10_000 })
    await page.getByTestId('gate-share-copy-btn').click()
    await expect(page.getByTestId('gate-share-panel-body')).toBeVisible()
    await page.getByTestId('gate-share-create').click()
    await expect(page.getByTestId('gate-share-url')).toHaveValue(/#t=••••/)
    const copied = await page.evaluate(() => (window as unknown as { __copied?: string }).__copied || '')
    expect(copied).toContain('https://approving.example.com/public/gate-approvals#t=')
    await expect(page.getByTestId('toast-host')).toContainText('已自动复制新链接')

    await page.getByTestId('gate-share-copy').click()
    await expect(page.getByTestId('toast-host')).toContainText('已复制到剪贴板')
  })

  test('复制失败展开全文且同文案 toast 仅一条 (plan g4.2 / g3)', async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(window, 'isSecureContext', {
        configurable: true,
        get: () => false,
      })
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: async () => {
            throw new Error('denied')
          },
        },
      })
      document.execCommand = (() => false) as typeof document.execCommand
    })
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/gate-share-link.html?scene=inbox')
    await expect(page.getByTestId('gate-share-e2e-root')).toBeVisible({ timeout: 10_000 })
    await page.getByTestId('gate-share-copy-btn').click()
    await page.getByTestId('gate-share-create').click()
    await expect(page.getByTestId('gate-share-url')).toHaveValue(/#t=[0-9a-f]{64}/)
    await expect(page.getByTestId('toast-host')).toContainText('无法写入剪贴板，请全选下方链接手动复制')

    await page.getByTestId('gate-share-copy').click()
    await page.getByTestId('gate-share-copy').click()
    await page.getByTestId('gate-share-copy').click()
    // Exclude TransitionGroup leave-active nodes so DOM count matches visible toasts (plan g3.2)
    const fallback = page
      .getByTestId('toast-host')
      .locator('div:not(.toast-leave-active):not(.toast-leave-to)')
      .filter({ hasText: '无法写入剪贴板，请全选下方链接手动复制' })
    await expect(fallback).toHaveCount(1)
  })

  test('重新生成两段反馈：已重新生成 + 已自动复制 (plan g4.2 / g2.3)', async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: async (text: string) => {
            ;(window as unknown as { __copied?: string }).__copied = text
          },
        },
      })
    })
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/gate-share-link.html?scene=inbox')
    await expect(page.getByTestId('gate-share-e2e-root')).toBeVisible({ timeout: 10_000 })
    await page.getByTestId('gate-share-copy-btn').click()
    await page.getByTestId('gate-share-create').click()
    await expect(page.getByTestId('gate-share-url')).toBeVisible()
    await page.getByTestId('gate-share-regen').click()
    await page.getByTestId('gate-share-confirm-ok').click()
    await expect(page.getByTestId('toast-host')).toContainText('链接已重新生成')
    await expect(page.getByTestId('toast-host')).toContainText('已自动复制新链接')
    const copied = await page.evaluate(() => (window as unknown as { __copied?: string }).__copied || '')
    expect(copied).toContain('https://approving.example.com/public/gate-approvals#t=')
    expect(copied).toMatch(/#t=[0-9a-f]{64}$/)
  })

  test('环回铸造告警并禁自动复制', async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: async (text: string) => {
            ;(window as unknown as { __copied?: string }).__copied = text
          },
        },
      })
    })
    await page.setViewportSize({ width: 1280, height: 800 })
    // shareHost=loopback → fixture mints 127.0.0.1 (plan g2.2 / g2.3)
    await page.goto('/gate-share-link.html?scene=inbox&shareHost=loopback')
    await expect(page.getByTestId('gate-share-e2e-root')).toBeVisible({ timeout: 10_000 })
    await page.getByTestId('gate-share-copy-btn').click()
    await expect(page.getByTestId('gate-share-panel-body')).toBeVisible()
    await page.getByTestId('gate-share-create').click()
    await expect(page.getByTestId('gate-share-url')).toBeVisible()
    await expect(page.getByTestId('gate-share-loopback-warning')).toBeVisible()
    await expect(page.getByTestId('gate-share-loopback-warning')).toContainText(/环回|外部不可达/)
    await expect(page.getByTestId('gate-share-copy')).toBeDisabled()
    await expect(page.getByTestId('gate-share-loopback-copy-hint')).toContainText(/不可复制/)
    await expect(page.getByTestId('gate-share-regen')).toBeEnabled()
    await expect(page.getByTestId('gate-share-revoke')).toBeEnabled()
    const copied = await page.evaluate(() => (window as unknown as { __copied?: string }).__copied || '')
    expect(copied).toBe('')
  })

  test('未登录外部页提交中文案且加载不泄露内部标识', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/gate-share-link.html?scene=public&slowPreview=1&slowDecide=1')
    await expect(page.getByTestId('public-gate-root')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('public-gate-loading')).toBeVisible()
    await expect(page.getByTestId('public-gate-root')).not.toContainText('run-e2e')
    await expect(page.getByTestId('public-gate-root')).not.toContainText('确认并流转')
    await page.getByTestId('public-gate-name').fill('Jordan')
    await page.getByTestId('public-gate-comment').fill('可以流转')
    await page.getByTestId('clarify-confirm-flow').click()
    await expect(page.getByTestId('clarify-confirm-flow')).toHaveText(/校验中/)
    await expect(page.getByTestId('clarify-confirm-flow')).not.toHaveText(/提交中/)
    await expect(page.getByTestId('public-gate-reject')).toBeDisabled()
    await expect(page.getByTestId('public-gate-done')).toContainText('已确认', { timeout: 10_000 })
  })

  test('未登录外部页可确认', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/gate-share-link.html?scene=public')
    await expect(page.getByTestId('public-gate-root')).toBeVisible({ timeout: 10_000 })
    await expect(page.locator('html')).not.toHaveClass(/light/)
    await expect(page.getByTestId('public-gate-badge')).toHaveText('外部一次决策')
    await expect(page.getByTestId('review-shell')).toBeVisible()
    await expect(page.getByTestId('public-gate-product-label')).toContainText('视觉网页产物')
    await expect(page.getByTestId('public-gate-upstream')).toContainText('上游上下文')
    await expect(page.getByTestId('public-gate-upstream-enlarge')).toBeVisible()
    await expect(page.getByTestId('public-gate-root')).not.toContainText('run-e2e')
    await expect(page.getByTestId('public-gate-root')).not.toContainText('请确认本次交付')
    await expect(page.getByTestId('clarify-confirm-flow')).toHaveText('确认并流转')
    await expect(page.getByTestId('public-gate-reject')).toHaveText('驳回')
    await page.getByTestId('public-gate-name').fill('Jordan')
    await page.getByTestId('public-gate-comment').fill('可以流转')
    await page.getByTestId('clarify-confirm-flow').click()
    await expect(page.getByTestId('public-gate-done')).toContainText('已确认')
    await expect(page.getByTestId('clarify-confirm-flow')).toHaveCount(0)
    await expect(page.getByTestId('public-gate-reject')).toHaveCount(0)
  })

  test('未登录外部页驳回需意见', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/gate-share-link.html?scene=public')
    await expect(page.getByTestId('public-gate-reject')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('public-gate-reject')).toHaveText('驳回')
    await page.getByTestId('public-gate-reject').click()
    await expect(page.getByTestId('clarify-confirm-error')).toHaveText('请填写姓名与意见后再提交')
    await page.getByTestId('public-gate-name').fill('Jordan')
    await page.getByTestId('public-gate-comment').fill('需要修改文案')
    await page.getByTestId('public-gate-reject').click()
    await expect(page.getByTestId('public-gate-done')).toContainText('已驳回')
  })

  test('移动端 Inbox 复制临时链接贴齐花粒且 hit 区 ≥44', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/gate-share-link.html?scene=inbox')
    await expect(page.getByTestId('gate-share-e2e-root')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('gate-share-copy-btn')).toBeVisible()
    await expect(page.getByTestId('gate-share-status')).toContainText('尚未创建')
    const btnBox = await page.getByTestId('gate-share-copy-btn').boundingBox()
    const chipBox = await page.getByTestId('gate-share-status').boundingBox()
    const wrapBox = await page.getByTestId('gate-share-hit-wrap').boundingBox()
    expect(btnBox && chipBox && wrapBox).toBeTruthy()
    expect(btnBox!.height).toBeLessThanOrEqual(chipBox!.height + 8)
    expect(wrapBox!.width).toBeGreaterThanOrEqual(44)
    expect(wrapBox!.height).toBeGreaterThanOrEqual(44)
    await page.getByTestId('gate-share-status').click()
    await expect(page.getByTestId('gate-share-panel-body')).toHaveCount(0)
    await page.getByTestId('gate-share-copy-btn').click()
    await expect(page.getByTestId('gate-share-panel-body')).toBeVisible()
  })

  test('待复审 Inbox 可创建并复制临时链接', async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: async (text: string) => {
            ;(window as unknown as { __copied?: string }).__copied = text
          },
        },
      })
    })
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/gate-share-link.html?scene=inbox-review')
    await expect(page.getByTestId('gate-share-copy-btn')).toBeVisible({ timeout: 10_000 })
    await page.getByTestId('gate-share-copy-btn').click()
    await expect(page.getByTestId('gate-share-panel-body')).toBeVisible()
    await page.getByTestId('gate-share-create').click()
    await expect(page.getByTestId('gate-share-url')).toBeVisible()
    const copied = await page.evaluate(() => (window as unknown as { __copied?: string }).__copied || '')
    expect(copied).toContain('https://approving.example.com/public/gate-approvals#t=')
  })

  test('未登录复审页三区可发送并底栏确认', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/gate-share-link.html?scene=public-review')
    await expect(page.getByTestId('public-gate-root')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('public-gate-root')).toContainText('外部复审')
    await expect(page.getByTestId('review-shell')).toBeVisible()
    await expect(page.getByTestId('public-gate-react-stage')).toBeVisible()
    await expect(page.getByTestId('react-artifact-tab-grid')).toBeVisible()
    await expect(page.getByTestId('public-gate-sidebar')).toContainText('Agent交互')
    await expect(page.getByTestId('clarify-input')).toBeVisible()
    await expect(page.getByTestId('clarify-confirm-flow')).toBeVisible()
    await expect(page.getByTestId('public-gate-footer')).toHaveCount(0)
    await expect(page.getByTestId('public-gate-reject')).toHaveCount(0)
    await expect(page.getByTestId('public-gate-name')).toHaveCount(0)
    await page.getByTestId('clarify-input').fill('请改标题')
    await page.getByTestId('clarify-send-icon').click()
    await expect(page.getByTestId('clarify-review-queue')).toBeVisible()
    await expect(page.getByTestId('clarify-review-queue')).toContainText('请改标题')
    await expect(page.getByTestId('public-gate-done')).toHaveCount(0)
    await expect(page.getByTestId('review-shell')).toBeVisible()
    await page.evaluate(() => {
      ;(window as unknown as { __idleReview?: () => void }).__idleReview?.()
      window.dispatchEvent(new HashChangeEvent('hashchange'))
    })
    await expect(page.getByTestId('clarify-confirm-flow')).toBeVisible()
    await page.getByTestId('clarify-confirm-flow').click()
    await expect(page.getByTestId('public-gate-done')).toContainText('已确认')
    await expect(page.getByTestId('clarify-confirm-flow')).toHaveCount(0)
  })

  test('应用预览 Inbox 卡片可打开 review 分享面板', async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: async (text: string) => {
            ;(window as unknown as { __copied?: string }).__copied = text
          },
        },
      })
    })
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/gate-share-link.html?scene=inbox-app-preview')
    await expect(page.getByTestId('gate-share-copy-btn')).toBeVisible({ timeout: 10_000 })
    await page.getByTestId('gate-share-copy-btn').click()
    await expect(page.getByTestId('gate-share-panel-body')).toBeVisible()
    await page.getByTestId('gate-share-create').click()
    await expect(page.getByTestId('gate-share-url')).toBeVisible()
    const copied = await page.evaluate(() => (window as unknown as { __copied?: string }).__copied || '')
    expect(copied).toContain('https://approving.example.com/public/gate-approvals#t=')
  })

  test('待澄清 Inbox 两处入口可生成临时链接', async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: async (text: string) => {
            ;(window as unknown as { __copied?: string }).__copied = text
          },
        },
      })
    })
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/gate-share-link.html?scene=inbox-clarify')
    await expect(page.getByTestId('gate-share-copy-btn')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('gate-share-copy-btn-detail')).toBeVisible()
    await expect(page.getByTestId('review-composer-open-share')).toHaveCount(0)
    await expect(page.getByTestId('html-preview-share-link')).toHaveCount(0)
    await page.getByTestId('gate-share-copy-btn').click()
    await expect(page.getByTestId('gate-share-panel-body')).toBeVisible()
    await expect(page.getByTestId('gate-share-ttl')).toHaveCount(5)
    await page.getByTestId('gate-share-create').click()
    await expect(page.getByTestId('gate-share-url')).toBeVisible()
    const copied = await page.evaluate(() => (window as unknown as { __copied?: string }).__copied || '')
    expect(copied).toContain('https://approving.example.com/public/gate-approvals#t=')
  })

  test('未登录澄清页文案分叉、空产物、多轮与未结束确认不燃链', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/gate-share-link.html?scene=public-clarify&unfinishedConfirm=1')
    await expect(page.getByTestId('public-gate-root')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('public-gate-badge')).toHaveText('待澄清')
    await expect(page.getByTestId('public-gate-kind-hint')).toHaveText('外部澄清')
    await expect(page.getByTestId('public-gate-root')).not.toContainText('外部复审')
    await expect(page.getByTestId('public-gate-root')).not.toContainText('不触发 Agent')
    await expect(page.getByTestId('public-gate-root')).not.toContainText('run-e2e')
    await expect(page.getByTestId('public-gate-react-stage')).toBeVisible()
    await expect(page.getByTestId('react-artifact-tab-grid')).toBeVisible()
    await expect(page.getByTestId('react-artifact-grid-empty')).toHaveCount(0)
    await expect(page.getByTestId('react-artifact-card-ide')).toBeVisible()
    await expect(page.getByTestId('react-artifact-card-terminal')).toBeVisible()
    await expect(page.getByTestId('react-artifact-card-browser')).toBeVisible()
    await expect(page.getByTestId('react-artifact-card-log')).toBeVisible()
    await expect(page.getByTestId('public-gate-react-stage')).not.toContainText('本次运行还没有产物')
    await expect(page.getByTestId('public-gate-name')).toHaveCount(0)
    await expect(page.getByTestId('public-gate-reject')).toHaveCount(0)
    await page.getByTestId('clarify-input').fill('验收标准是可生成临时链接')
    await page.getByTestId('clarify-send-label').click()
    await expect(page.getByTestId('clarify-review-queue')).toContainText('验收标准是可生成临时链接')
    await page.getByTestId('clarify-review-cancel').click()
    await expect(page.getByTestId('clarify-review-queue')).toContainText('验收标准是可生成临时链接')
    await page.evaluate(() => {
      ;(window as unknown as { __idleReview?: () => void }).__idleReview?.()
      window.dispatchEvent(new HashChangeEvent('hashchange'))
    })
    await page.getByTestId('clarify-confirm-flow').click()
    await expect(page.getByTestId('clarify-confirm-error')).toContainText('尚未结束')
    await expect(page.getByTestId('public-gate-done')).toHaveCount(0)
    await expect(page.getByTestId('public-gate-workbench')).toBeVisible()
    await page.getByTestId('clarify-confirm-flow').click()
    await expect(page.getByTestId('public-gate-done')).toContainText('已确认')
  })

  test('两位访客打开同一链接各自对话互不可见', async ({ browser }) => {
    type Turn = { role: string; text: string; at: string }
    const seed: Turn = { role: 'agent', text: '请复审 research.json', at: '2026-08-01T00:00:00Z' }
    const turnsByVisitor = new Map<string, Turn[]>()
    const turnsFor = (visitor: string) => turnsByVisitor.get(visitor) ?? [seed]

    const open = async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 800 } })
      await context.route(/\/public\/gate-approvals\/preview$/, async (route) => {
        const visitor = route.request().headers()['x-gate-share-visitor'] || ''
        await route.fulfill({
          json: {
            status: 'active',
            kind: 'review',
            title: '调研',
            description: '待复审脱敏摘要',
            remainingSec: 3600,
            nonce: 'nonce-e2e-visitors',
            reactSessionAlive: true,
            sessionBusy: false,
            waiting: 0,
            productKind: 'structured',
            productName: 'research.json',
            actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
            structured: { name: 'research.json', title: '调研摘要', doc: { title: '调研摘要' } },
            turns: turnsFor(visitor),
          },
        })
      })
      await context.route(/\/public\/gate-approvals\/reply$/, async (route) => {
        const visitor = route.request().headers()['x-gate-share-visitor'] || ''
        const text = String((route.request().postDataJSON() as { text?: string }).text || '')
        const at = new Date().toISOString()
        turnsByVisitor.set(visitor, [
          ...turnsFor(visitor),
          { role: 'human', text, at },
          { role: 'agent', text: `收到：${text}`, at },
        ])
        await route.fulfill({ json: { status: 'accepted', kind: 'review' } })
      })
      const page = await context.newPage()
      await page.goto('/gate-share-link.html?scene=public-review&sharedBackend=1')
      await expect(page.getByTestId('clarify-input')).toBeVisible({ timeout: 10_000 })
      return { context, page }
    }

    const a = await open()
    const b = await open()
    try {
      await a.page.getByTestId('clarify-input').fill('访客A的问题')
      await a.page.getByTestId('clarify-send-icon').click()
      await expect(a.page.getByTestId('public-gate-root')).toContainText('收到：访客A的问题', { timeout: 10_000 })

      await b.page.getByTestId('clarify-input').fill('访客B的问题')
      await b.page.getByTestId('clarify-send-icon').click()
      await expect(b.page.getByTestId('public-gate-root')).toContainText('收到：访客B的问题', { timeout: 10_000 })
      await expect(b.page.getByTestId('public-gate-root')).not.toContainText('访客A的问题')

      await a.page.reload()
      await expect(a.page.getByTestId('public-gate-root')).toContainText('收到：访客A的问题', { timeout: 10_000 })
      await expect(a.page.getByTestId('public-gate-root')).not.toContainText('访客B的问题')

      const visitors = [...turnsByVisitor.keys()]
      expect(visitors).toHaveLength(2)
      for (const v of visitors) expect(v).toMatch(/^[0-9a-f]{32}$/)
    } finally {
      await a.context.close()
      await b.context.close()
    }
  })

  test('未登录复审页展示 agent 工具调用（仅名称与状态）', async ({ page }) => {
    await page.route(/\/public\/gate-approvals\/preview$/, async (route) => {
      await route.fulfill({
        json: {
          status: 'active',
          kind: 'review',
          title: '调研',
          description: '待复审脱敏摘要',
          remainingSec: 3600,
          nonce: 'nonce-e2e-tools',
          reactSessionAlive: true,
          sessionBusy: false,
          waiting: 0,
          productKind: 'structured',
          productName: 'research.json',
          actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
          structured: { name: 'research.json', title: '调研摘要', doc: { title: '调研摘要' } },
          turns: [
            {
              role: 'agent',
              text: '已核对 research.json',
              at: '2026-08-01T00:00:00Z',
              tools: [
                { title: 'read_file', status: 'completed' },
                { title: 'Shell', status: 'failed' },
              ],
            },
          ],
        },
      })
    })
    await page.goto('/gate-share-link.html?scene=public-review&sharedBackend=1')
    const group = page.getByTestId('agent-tool-group')
    await expect(group).toBeVisible({ timeout: 10_000 })
    await expect(group).toHaveAttribute('data-state', 'failed')
    await expect(group).toContainText('使用了 2 个工具')
    await expect(page.getByTestId('agent-tool-group-names')).toHaveText('读取文件 · 运行命令')
    await page.getByTestId('agent-tool-group-head').click()
    const rows = page.getByTestId('agent-tool-row')
    await expect(rows).toHaveCount(2)
    await expect(rows.nth(0)).toHaveAttribute('data-state', 'done')
    await expect(rows.nth(1)).toHaveAttribute('data-state', 'failed')
    await expect(page.getByTestId('public-gate-root')).toContainText('已核对 research.json')
  })

  test('未登录复审页按时间轴交替展示思考、工具与回复', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.route(/\/public\/gate-approvals\/preview$/, async (route) => {
      await route.fulfill({
        json: {
          status: 'active',
          kind: 'review',
          title: '调研',
          remainingSec: 3600,
          nonce: 'nonce-e2e-timeline',
          reactSessionAlive: true,
          sessionBusy: false,
          waiting: 0,
          productKind: 'structured',
          productName: 'research.json',
          actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
          structured: { name: 'research.json', title: '调研摘要', doc: { title: '调研摘要' } },
          turns: [
            { role: 'human', text: '核对一下数据来源', at: '2026-08-01T00:00:00Z' },
            {
              role: 'agent',
              text: '来源已补齐',
              at: '2026-08-01T00:00:01Z',
              tools: [{ title: 'read_file', status: 'completed' }, { title: 'Shell', status: 'completed' }, { title: 'write', status: 'completed' }],
              parts: [
                { kind: 'thought', text: '先读 research.json 看现有来源' },
                { kind: 'tool', title: 'read_file', status: 'completed' },
                { kind: 'tool', title: 'Shell', status: 'completed' },
                { kind: 'message', text: '发现两条来源缺少链接，我来补上。' },
                { kind: 'tool', title: 'write', status: 'completed' },
                { kind: 'message', text: '来源已补齐' },
              ],
            },
          ],
        },
      })
    })
    await page.goto('/gate-share-link.html?scene=public-review&sharedBackend=1')
    const timeline = page.getByTestId('agent-timeline')
    await expect(timeline).toBeVisible({ timeout: 10_000 })
    const order = await timeline.evaluate((el) => Array.from(el.children).map((c) => c.getAttribute('data-testid')))
    expect(order).toEqual(['agent-timeline-thought', 'agent-tool-group', 'clarify-agent-message', 'agent-tool-group', 'clarify-agent-message'])
    await expect(timeline.getByTestId('agent-tool-group').first()).toContainText('使用了 2 个工具')
    await expect(timeline.getByTestId('clarify-agent-message').first()).toHaveText('发现两条来源缺少链接，我来补上。')
    await expect(page.getByTestId('agent-tool-summary')).toHaveCount(0)
    await page.getByTestId('public-gate-sidebar').screenshot({ path: 'test-results/screenshots/public-review-timeline.png' })
  })

  test('公开应用预览页远程壳可确认且支持多端口', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/gate-share-link.html?scene=public-app-preview')
    await expect(page.getByTestId('public-gate-root')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('public-gate-react-stage')).toBeVisible()
    await expect(page.getByTestId('react-artifact-tab-grid')).toBeVisible()
    await expect(page.getByTestId('react-artifact-tab-novnc')).toBeVisible()
    await expect(page.getByTestId('public-gate-app-preview')).toBeVisible()
    await expect(page.getByTestId('public-gate-app-preview')).not.toContainText('只读')
    await expect(page.getByTestId('public-gate-app-preview')).not.toContainText('不提供远程桌面')
    await expect(page.getByTestId('public-gate-app-preview-port-5173')).toBeVisible()
    await expect(page.getByTestId('public-gate-app-preview-port-8080')).toBeVisible()
    await expect(page.getByTestId('novnc-inspect-toggle').or(page.getByTestId('public-gate-app-preview-retry')).or(page.getByTestId('public-gate-app-preview-connecting'))).toBeVisible({
      timeout: 10_000,
    })
    await page.getByTestId('public-gate-app-preview-port-8080').click()
    await expect(page.getByTestId('public-gate-app-preview-api')).toHaveCount(0)
    await expect(page.getByTestId('novnc-inspect-toggle').or(page.getByTestId('public-gate-app-preview-retry')).or(page.getByTestId('public-gate-app-preview-connecting'))).toBeVisible({
      timeout: 10_000,
    })
    await expect(page.getByTestId('clarify-input')).toBeVisible()
    await expect(page.getByTestId('clarify-confirm-flow')).toBeVisible()
    await page.getByTestId('clarify-confirm-flow').click()
    await expect(page.getByTestId('public-gate-done')).toContainText('已确认')
  })
})
