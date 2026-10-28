/** Whether a member can change the model on this visit's composer. */
export function visitModelSelectVisible(input: {
  modelSwitch?: boolean
  observe?: boolean
  followAuto?: boolean
  done?: boolean
  active?: boolean
  coldSession?: boolean
  composerLocked?: boolean
}): boolean {
  return !!input.modelSwitch
    && !input.observe
    && !input.followAuto
    && !input.done
    && input.active !== false
    && !input.coldSession
    && !input.composerLocked
}
