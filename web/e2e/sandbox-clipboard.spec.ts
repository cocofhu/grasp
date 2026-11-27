import { expect, test } from '@playwright/test'

test('sandbox clipboard honors takeover, supports manual staging, and resets the paste source on blur', async ({ page, context }) => {
  const localText = '  中文和 Emoji 🙂\nconst answer = 42;'
  const stagedText = '  沙箱剪贴板 📝\n下一行'
  const clipboardMessages: Record<string, unknown>[] = []
  const remoteKeys: [number, number][] = []
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.routeWebSocket('**/sandbox-vnc/42/ws', socket => {
    socket.onMessage(data => {
      if (typeof data !== 'string') {
        remoteKeys.push([data.readUInt32BE(4), data[1]])
        return
      }
      const msg = JSON.parse(data)
      if (msg.type !== 'clipboard') return
      clipboardMessages.push(msg)
      socket.send(JSON.stringify({ type: 'clipboard-result', id: msg.id, ok: true }))
    })
    socket.send(JSON.stringify({ type: 'ready', url: 'about:blank' }))
  })
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
  await expect.poll(() => remoteKeys).toEqual([[0xffe3, 1], [0x76, 1], [0x76, 0], [0xffe3, 0]])
  expect(clipboardMessages).toHaveLength(2)

  await page.getByPlaceholder('about:blank').focus()
  await canvas.focus()
  await page.keyboard.press('ControlOrMeta+V')
  await expect.poll(() => clipboardMessages[2]).toEqual({ ...localPaste, id: 3 })
})
