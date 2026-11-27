import { expect, test, type Page } from '@playwright/test'

async function mockClipboard(page: Page) {
  const clipboardMessages: Record<string, unknown>[] = []
  const remoteKeys: [number, number][] = []
  const watched = new Map<number, number>()
  let remoteRevision = 0
  await page.routeWebSocket('**/sandbox-vnc/42/ws', socket => {
    socket.onMessage(data => {
      if (typeof data !== 'string') {
        remoteKeys.push([data.readUInt32BE(4), data[1]])
        return
      }
      const msg = JSON.parse(data)
      if (msg.type === 'clipboard-watch') watched.set(msg.id, remoteRevision)
      if (msg.type === 'clipboard-check') {
        const before = watched.get(msg.id)
        const changed = before !== undefined && before !== remoteRevision
        socket.send(JSON.stringify({ type: 'clipboard-check-result', id: msg.id, changed }))
      }
      if (msg.type !== 'clipboard') return
      clipboardMessages.push(msg)
      socket.send(JSON.stringify({ type: 'clipboard-result', id: msg.id, ok: true }))
    })
    socket.send(JSON.stringify({ type: 'ready', url: 'about:blank', clipboard: true, clipboardState: true }))
  })
  return { clipboardMessages, remoteKeys, watched, copyInSandbox: () => { remoteRevision++ } }
}

const remotePaste = [[0xffe3, 1], [0x76, 1], [0x76, 0], [0xffe3, 0]]

test.beforeEach(({ context }) => context.grantPermissions(['clipboard-read', 'clipboard-write']))

test('sandbox clipboard honors takeover, supports manual staging, and resets the paste source on blur', async ({ page }) => {
  const localText = '  中文和 Emoji 🙂\nconst answer = 42;'
  const stagedText = '  沙箱剪贴板 📝\n下一行'
  const { clipboardMessages, remoteKeys } = await mockClipboard(page)
  await page.goto('/console.html?tab=novnc')
  await page.evaluate(value => navigator.clipboard.writeText(value), localText)
  const toggle = page.getByRole('button', { name: '剪贴板', exact: true })
  const canvas = page.locator('.novnc-canvas-host canvas')
  await expect(toggle).toBeDisabled()
  await canvas.focus()
  await page.keyboard.press('ControlOrMeta+V')
  expect(clipboardMessages).toHaveLength(0)

  await page.getByTestId('novnc-control-toggle').click()
  await expect(toggle).toBeEnabled()
  await canvas.focus()
  await page.keyboard.press('ControlOrMeta+V')
  const localPaste = { type: 'clipboard', id: 1, text: localText, paste: true, shift: false }
  await expect.poll(() => clipboardMessages).toEqual([localPaste])

  await toggle.click()
  const input = page.getByTestId('novnc-clipboard-text')
  await expect(input).toBeFocused()
  await page.keyboard.press('ControlOrMeta+V')
  await expect(input).toHaveValue(localText)
  await input.fill(stagedText)
  await page.getByRole('button', { name: '放入沙箱剪贴板' }).click()
  await expect(canvas).toBeFocused()
  expect(clipboardMessages[1]).toEqual({ type: 'clipboard', id: 2, text: stagedText, paste: false, shift: false })

  await page.keyboard.press('ControlOrMeta+V')
  await expect.poll(() => remoteKeys).toEqual(remotePaste)
  expect(clipboardMessages).toHaveLength(2)

  await page.getByPlaceholder('about:blank').focus()
  await canvas.focus()
  await page.keyboard.press('ControlOrMeta+V')
  await expect.poll(() => clipboardMessages[2]).toEqual({ ...localPaste, id: 3 })
})

test('sandbox copy and an empty local clipboard use the remote paste shortcut', async ({ page }) => {
  const { clipboardMessages, remoteKeys, watched, copyInSandbox } = await mockClipboard(page)
  await page.goto('/console.html?tab=novnc')
  await page.getByTestId('novnc-control-toggle').click()
  const canvas = page.locator('.novnc-canvas-host canvas')
  await canvas.focus()
  await page.evaluate(() => navigator.clipboard.writeText('Local text that should stay on the device'))
  await page.keyboard.press('Control+c')
  await expect.poll(() => watched.size).toBe(1)
  copyInSandbox()
  await page.keyboard.press('ControlOrMeta+V')
  await expect.poll(() => remoteKeys).toEqual(remotePaste)
  expect(clipboardMessages).toHaveLength(0)

  await page.getByPlaceholder('about:blank').focus()
  await canvas.focus()
  await page.evaluate(() => navigator.clipboard.writeText(''))
  await page.keyboard.press('ControlOrMeta+V')
  await expect.poll(() => remoteKeys).toEqual([...remotePaste, ...remotePaste])
  expect(clipboardMessages).toHaveLength(0)
})

test('pasting after copying from a remote context menu uses the remote clipboard', async ({ page }) => {
  const { clipboardMessages, remoteKeys, watched, copyInSandbox } = await mockClipboard(page)
  await page.goto('/console.html?tab=novnc')
  await page.getByTestId('novnc-control-toggle').click()
  await page.evaluate(() => navigator.clipboard.writeText('Local text that should stay on the device'))
  const canvas = page.locator('.novnc-canvas-host canvas')
  await canvas.click({ button: 'right' })
  await expect.poll(() => watched.size).toBe(1)
  await canvas.click()
  copyInSandbox()
  await canvas.focus()
  await page.keyboard.press('ControlOrMeta+V')
  await expect.poll(() => remoteKeys).toEqual(remotePaste)
  expect(clipboardMessages).toHaveLength(0)
})

test('copying through a remote menu opened by keyboard preserves the remote clipboard', async ({ page }) => {
  const { clipboardMessages, remoteKeys, watched, copyInSandbox } = await mockClipboard(page)
  await page.goto('/console.html?tab=novnc')
  await page.getByTestId('novnc-control-toggle').click()
  await page.locator('.novnc-canvas-host canvas').focus()
  await page.evaluate(() => navigator.clipboard.writeText('Local text that should stay on the device'))
  await page.keyboard.press('Shift+F10')
  await expect.poll(() => watched.size).toBe(1)
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('Enter')
  copyInSandbox()
  await page.keyboard.press('ControlOrMeta+V')
  await expect.poll(() => remoteKeys).toEqual(remotePaste)
  expect(clipboardMessages).toHaveLength(0)
})

test('interrupting a terminal without changing its clipboard keeps local paste available', async ({ page }) => {
  const { clipboardMessages, remoteKeys, watched } = await mockClipboard(page)
  await page.goto('/console.html?tab=novnc')
  await page.getByTestId('novnc-control-toggle').click()
  await page.locator('.novnc-canvas-host canvas').focus()
  await page.evaluate(() => navigator.clipboard.writeText('New local text after interrupt'))
  await page.keyboard.press('Control+c')
  await page.keyboard.press('ControlOrMeta+Shift+V')
  await expect.poll(() => clipboardMessages).toMatchObject([
    { type: 'clipboard', text: 'New local text after interrupt', paste: true, shift: true },
  ])
  expect(watched.size).toBe(1)
  expect(remoteKeys).toHaveLength(0)
})

test('paste restores held modifiers before forwarding the next key to noVNC', async ({ page }) => {
  const { clipboardMessages } = await mockClipboard(page)
  await page.goto('/console.html?tab=novnc')
  await page.getByTestId('novnc-control-toggle').click()
  const canvas = page.locator('.novnc-canvas-host canvas')
  const forwarded = await canvas.evaluateHandle(el => {
    const events: string[] = []
    for (const type of ['keydown', 'keyup']) {
      el.addEventListener(type, event => {
        if (event instanceof KeyboardEvent) events.push(`${event.code} ${type} ${event.isTrusted ? 'physical' : 'synthetic'}`)
      })
    }
    return events
  })
  await canvas.focus()
  await page.evaluate(() => navigator.clipboard.writeText('Clipboard text'))
  const modifier = process.platform === 'darwin' ? 'Meta' : 'Control'
  await page.keyboard.down(modifier)
  await page.keyboard.down('Shift')
  await page.keyboard.press('V')
  await page.keyboard.press('A')
  await page.keyboard.up('Shift')
  await page.keyboard.up(modifier)
  await expect.poll(() => clipboardMessages).toEqual([
    { type: 'clipboard', id: 1, text: 'Clipboard text', paste: true, shift: true },
  ])
  const keys = await forwarded.jsonValue()
  expect(keys).toContain(`${modifier}Left keyup synthetic`)
  expect(keys).toContain('ShiftLeft keyup synthetic')
  expect(keys.slice(-6)).toEqual([
    `${modifier}Left keydown synthetic`,
    'ShiftLeft keydown synthetic',
    'KeyA keydown physical',
    'KeyA keyup physical',
    'ShiftLeft keyup physical',
    `${modifier}Left keyup physical`,
  ])
})
