/** What the confirm-flow dialog should say will be discarded, if anything. */
export type ConfirmFlowDiscard = 'none' | 'edits' | 'draft' | 'both'

export function confirmFlowDiscardKind(unsavedEdits: boolean, unsentDraft: boolean): ConfirmFlowDiscard {
  if (unsavedEdits && unsentDraft) return 'both'
  if (unsavedEdits) return 'edits'
  if (unsentDraft) return 'draft'
  return 'none'
}
