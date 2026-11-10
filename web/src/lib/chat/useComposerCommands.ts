import { computed, ref, type Ref } from 'vue'

export interface ComposerCommand {
  id: string
  label: string
  description: string
}

/** Slash commands are an explicit composer action, separate from chat sends. */
export function useComposerCommands(options: {
  input: Ref<string>
  textarea: Ref<HTMLTextAreaElement | null | undefined>
  composing: Ref<boolean>
  commands: Ref<ComposerCommand[]>
  disabled: Ref<boolean | undefined>
  onSelect: (command: ComposerCommand) => void
}) {
  const focused = ref(false)
  const query = ref<string | null>(null)
  const selected = ref(0)
  let dismissedText: string | null = null
  const matches = computed(() => query.value === null ? [] : options.commands.value.filter(command => command.id.startsWith(query.value!)))
  const open = computed(() => focused.value && !options.disabled.value && !options.composing.value &&
    options.input.value === `/${query.value}` && matches.value.length > 0)

  function updateTrigger() {
    const element = options.textarea.value
    if (dismissedText !== options.input.value) dismissedText = null
    // Only a command prefix occupying the entire input opens the menu. Paths,
    // URLs, multiline drafts and an existing message keep their usual behavior.
    const match = /^\/([a-z]*)$/.exec(options.input.value)
    const next = focused.value && !options.disabled.value && !options.composing.value &&
      element?.selectionStart === options.input.value.length && element.selectionEnd === element.selectionStart &&
      dismissedText === null && match ? match[1]! : null
    if (next !== query.value) selected.value = 0
    query.value = next
  }

  function focus() {
    focused.value = true
    dismissedText = null
    updateTrigger()
  }

  function dismiss() {
    dismissedText = options.input.value
    query.value = null
  }

  function blur() {
    focused.value = false
    dismiss()
  }

  function select(command = matches.value[selected.value]) {
    updateTrigger()
    if (!open.value || !command || !matches.value.includes(command)) return
    dismiss()
    options.onSelect(command)
  }

  function onKeydown(event: KeyboardEvent): boolean {
    updateTrigger()
    if (!open.value || options.composing.value || event.isComposing || event.keyCode === 229 || event.altKey) return false
    // Ctrl/⌘+Enter sends on home; with the menu open it must not send the bare prefix.
    if ((event.ctrlKey || event.metaKey) && event.key !== 'Enter') return false
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      selected.value = (selected.value + (event.key === 'ArrowDown' ? 1 : -1) + matches.value.length) % matches.value.length
    } else if (event.key === 'Escape') dismiss()
    else if (!event.shiftKey && (event.key === 'Enter' || event.key === 'Tab')) select()
    else return false
    event.preventDefault()
    return true
  }

  return { open, matches, selected, updateTrigger, focus, blur, dismiss, select, onKeydown }
}
