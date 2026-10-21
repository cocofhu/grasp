import { flushPromises } from '@vue/test-utils'

/** Click「确认流转」in the teleported dialog. Throws if the dialog is not open. */
export async function acceptConfirmFlowDialog(root: ParentNode = document.body) {
  await flushPromises()
  const btn = root.querySelector('[data-testid="confirm-flow-dialog-accept"]') as HTMLButtonElement | null
  if (!btn) throw new Error('confirm-flow dialog is not open')
  btn.click()
  await flushPromises()
}
