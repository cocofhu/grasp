import { inject, provide, type InjectionKey, type Ref } from 'vue'

export type ConversationArtifactTarget = {
  id?: string
  name?: string
  runId?: string
  preview?: boolean
}
export type ConversationArtifacts = { open: (target: ConversationArtifactTarget) => void | Promise<void> }
export const conversationPanelVisibleKey: InjectionKey<Readonly<Ref<boolean>>> = Symbol('conversation-panel-visible')

/** Fired on window when the conversation artifact drawer opens, so a preview mounted while hidden can repaint. */
export const CONVERSATION_PANEL_SHOWN_EVENT = 'grasp-conversation-panel-shown'

const key: InjectionKey<ConversationArtifacts> = Symbol('conversation-artifacts')

export function provideConversationArtifacts(controller: ConversationArtifacts): void {
  provide(key, controller)
}

export function useConversationArtifacts(): ConversationArtifacts | null {
  return inject(key, null)
}
