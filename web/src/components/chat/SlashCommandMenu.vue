<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import { chatCommandIcons } from '@/lib/chat/chatCommands'
import { isComposingKey } from '@/lib/chat/composerKeys'

type Option = {
  command: keyof typeof chatCommandIcons
  value: string
  label: string
  description?: string
  opensSkills?: boolean
}

const props = withDefaults(defineProps<{
  textarea?: HTMLTextAreaElement | null
  skills?: readonly string[]
  disabled?: boolean
  composing?: boolean
  allowContext?: boolean
  testPrefix: string
}>(), { allowContext: true })
const text = defineModel<string>({ required: true })
const { t } = useI18n()
const id = useId()
const list = ref<HTMLElement>()
const focused = ref(false)
const dismissed = ref('')
const active = ref(0)
const query = computed(() => text.value.trimStart())
const skillFilter = computed(() => /^\/skill[^\S\r\n]+(\S*)$/.exec(query.value)?.[1])
const showingSkills = computed(() => skillFilter.value !== undefined)
const open = computed(() =>
  focused.value && !props.disabled && !props.composing && query.value.startsWith('/') &&
  query.value !== dismissed.value && !/[\r\n]/.test(query.value),
)
const options = computed<Option[]>(() => {
  if (!open.value) return []
  if (skillFilter.value !== undefined) {
    const filter = skillFilter.value.toLowerCase()
    const names = (props.skills ?? []).filter(name => name.toLowerCase().includes(filter))
    names.sort((a, b) => Number(b.toLowerCase() === filter) - Number(a.toLowerCase() === filter))
    return names
      .map(name => ({ command: 'skill', value: '/skill ' + name + ' ', label: name }))
  }
  const commands = props.allowContext ? (['new', 'compact', 'skill'] as const) : (['skill'] as const)
  return commands.map(command => ({
    command,
    value: '/' + command + (command === 'skill' ? ' ' : ''),
    label: t('pages.chatCommands.' + command + '.title'),
    description: t('pages.chatCommands.' + command + '.description'),
    opensSkills: command === 'skill',
  })).filter(option => option.value.startsWith(query.value))
})
const showSkillHint = computed(() => open.value && skillFilter.value === '' && !props.skills?.length)
watch(options, () => { active.value = 0 })

function handleKeydown(event: KeyboardEvent): boolean {
  if ((!options.value.length && !showSkillHint.value) || isComposingKey(event, props.composing)) return false
  if (event.key === 'Escape') {
    dismissed.value = query.value
  } else if (!options.value.length || event.shiftKey || event.ctrlKey || event.metaKey || event.altKey) {
    return false
  } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    const count = options.value.length
    const next = active.value + (event.key === 'ArrowDown' ? 1 : -1)
    active.value = (next + count) % count
    list.value?.children[active.value]?.scrollIntoView?.({ block: 'nearest' })
  } else if (event.key === 'Enter') {
    const option = options.value[active.value]
    // A fully typed command can be sent with the composer's normal shortcut.
    if (!option || (!option.opensSkills && query.value.trimEnd() === option.value.trimEnd())) return false
    choose(option)
  } else {
    return false
  }
  event.preventDefault()
  return true
}

function updateDraft(value: string, keepOpen: boolean) {
  dismissed.value = keepOpen ? '' : value
  text.value = value
  void nextTick(() => {
    props.textarea?.focus()
    props.textarea?.setSelectionRange(text.value.length, text.value.length)
  })
}

function choose(option: Option) {
  updateDraft(option.value, !!option.opensSkills)
}

watch(() => props.textarea, (el, _, onCleanup) => {
  if (!el) return
  const onFocus = () => { focused.value = true }
  const onBlur = () => { focused.value = false }
  const onInput = () => { dismissed.value = '' }
  focused.value = document.activeElement === el
  el.addEventListener('focus', onFocus)
  el.addEventListener('blur', onBlur)
  el.addEventListener('input', onInput)
  el.setAttribute('aria-autocomplete', 'list')
  onCleanup(() => {
    el.removeEventListener('focus', onFocus)
    el.removeEventListener('blur', onBlur)
    el.removeEventListener('input', onInput)
    el.removeAttribute('aria-autocomplete')
    el.removeAttribute('aria-controls')
    el.removeAttribute('aria-activedescendant')
  })
}, { immediate: true })
watch([() => props.textarea, options, active], ([el, rows, index]) => {
  if (!el) return
  if (rows[index]) {
    el.setAttribute('aria-controls', id)
    el.setAttribute('aria-activedescendant', id + '-' + index)
  } else {
    el.removeAttribute('aria-controls')
    el.removeAttribute('aria-activedescendant')
  }
})
defineExpose({ handleKeydown })
</script>

<template>
  <div v-if="options.length || showSkillHint" class="command-palette">
    <div v-if="showingSkills" class="command-palette-heading">
      <button
        type="button"
        class="command-palette-back"
        :aria-label="t('pages.chatCommands.back')"
        @pointerdown.prevent
        @click="updateDraft('/', true)"
      >
        <Icon name="chevron-left" :size="15" />
      </button>
      <span>{{ t('pages.chatCommands.chooseSkill') }}</span>
    </div>
    <div
      v-if="options.length"
      :id="id"
      ref="list"
      class="command-palette-list"
      role="listbox"
      :aria-label="t(showingSkills ? 'pages.chatCommands.chooseSkill' : 'pages.chatCommands.menu')"
      :data-testid="testPrefix + '-commands'"
    >
      <button
        v-for="(option, index) in options"
        :id="id + '-' + index"
        :key="option.value"
        type="button"
        role="option"
        tabindex="-1"
        class="command-palette-option"
        :aria-selected="index === active"
        :data-command="option.command"
        @pointerenter="active = index"
        @pointerdown.prevent
        @click="choose(option)"
      >
        <span class="command-palette-icon"><Icon :name="chatCommandIcons[option.command]" :size="17" /></span>
        <span class="command-palette-copy">
          <span class="command-palette-label">{{ option.label }}</span>
          <span v-if="option.description" class="command-palette-description">{{ option.description }}</span>
        </span>
        <Icon v-if="option.opensSkills" name="chevron-right" :size="13" class="command-palette-next" />
      </button>
    </div>
    <p v-else class="command-palette-hint" role="status">{{ t('pages.chatCommands.skillHint') }}</p>
    <div v-if="options.length" class="command-palette-keys">
      <span><kbd>↑</kbd><kbd>↓</kbd>{{ t('pages.chatCommands.move') }}</span>
      <span><kbd>↵</kbd>{{ t('pages.chatCommands.insert') }}</span>
      <span><kbd>Esc</kbd>{{ t('pages.chatCommands.close') }}</span>
    </div>
  </div>
</template>

<style scoped>
.command-palette {
  position: absolute;
  z-index: 30;
  bottom: calc(100% + 8px);
  inset-inline-start: 0;
  width: min(100%, 34rem);
  overflow: hidden;
  border: 1px solid rgb(var(--c-line));
  border-radius: 16px;
  background: rgb(var(--c-surface));
  box-shadow: 0 12px 32px rgb(20 15 36 / 13%), 0 2px 8px rgb(20 15 36 / 8%);
}
.command-palette-list {
  max-height: min(16rem, 46dvh);
  overflow-y: auto;
  padding: 5px;
  overscroll-behavior: contain;
}
.command-palette-option {
  display: flex;
  align-items: center;
  width: 100%;
  min-width: 0;
  gap: 10px;
  padding: 7px 9px;
  text-align: start;
  border: 1px solid transparent;
  border-radius: 10px;
  color: rgb(var(--c-txt));
}
.command-palette-option[aria-selected='true'] {
  border-color: rgb(var(--c-accent) / 16%);
  background: rgb(var(--c-accent) / 9%);
}
.command-palette-icon {
  display: grid;
  flex: none;
  place-items: center;
  width: 32px;
  height: 32px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 9px;
  background: rgb(var(--c-base));
  color: rgb(var(--c-txt2));
}
.command-palette-option[aria-selected='true'] .command-palette-icon {
  border-color: rgb(var(--c-accent) / 18%);
  background: rgb(var(--c-surface));
  color: rgb(var(--c-accent-2));
}
.command-palette-copy { display: grid; min-width: 0; flex: 1; gap: 2px; overflow-wrap: anywhere; }
.command-palette-label { font-size: 13px; font-weight: 600; line-height: 1.35; }
.command-palette-description { font-size: 11px; line-height: 1.4; color: rgb(var(--c-txt2)); }
.command-palette-next { flex: none; color: rgb(var(--c-txt3)); }
.command-palette-heading {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 8px;
  border-bottom: 1px solid rgb(var(--c-line) / 70%);
  color: rgb(var(--c-txt));
  font-size: 12px;
  font-weight: 600;
}
.command-palette-back {
  display: grid;
  width: 28px;
  height: 28px;
  place-items: center;
  border-radius: 6px;
  color: rgb(var(--c-txt2));
}
.command-palette-back:hover { background: rgb(var(--c-base)); color: rgb(var(--c-txt)); }
.command-palette-hint { padding: 11px 13px; color: rgb(var(--c-txt2)); font-size: 12px; }
.command-palette-keys {
  display: flex;
  flex-wrap: wrap;
  gap: 11px;
  padding: 6px 11px;
  border-top: 1px solid rgb(var(--c-line) / 70%);
  color: rgb(var(--c-txt3));
  font-size: 11px;
}
.command-palette-keys span { display: inline-flex; align-items: center; gap: 3px; }
.command-palette-keys kbd {
  min-width: 1.1rem;
  padding: 0 3px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 4px;
  text-align: center;
  background: rgb(var(--c-base));
  color: rgb(var(--c-txt2));
}
@media (pointer: coarse) {
  .command-palette-keys { display: none; }
}
</style>
