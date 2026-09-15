/** True for multi-turn ReAct clarify dialogues (ask_question + waiting_human inbox). */
export function isGrasp(type: string | undefined | null): boolean {
  return type === 'grasp' || type === 'approve'
}

export function isClarifyInteractive(type: string | undefined | null): boolean {
  return type === 'react' || isGrasp(type) || type === 'preflight'
}
