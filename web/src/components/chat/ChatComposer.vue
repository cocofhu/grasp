<script setup lang="ts">
import AttachmentDropZone from '@/components/chat/AttachmentDropZone.vue'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AnnotationChip from '@/components/run/AnnotationChip.vue'
import ComposerShell from '@/components/run/ComposerShell.vue'
import ReferenceChip from '@/components/solo/ReferenceChip.vue'
import ReferencePicker from '@/components/solo/ReferencePicker.vue'
import ReferencePlusMenu from '@/components/solo/ReferencePlusMenu.vue'
import type { ChatComposerDraft } from '@/lib/chat/useChatComposerDraft'
import type { ReferenceScope, ReferenceTab, ScopedReferenceSearch } from '@/lib/solo/useReferenceSearch'
import type { ReactAnnotation } from '@/lib/shared/types'
import ChatAttachments from './ChatAttachments.vue'
import ChatComposerTextarea from './ChatComposerTextarea.vue'

const props = withDefaults(defineProps<{
  draft: ChatComposerDraft
  picks: ReactAnnotation[]
  placeholder: string
  scope: ReferenceScope
  scopedSearch?: ScopedReferenceSearch
  allowSolo?: boolean
  disabled?: boolean
  testPrefix: string
  pickTestId: string
}>(), { allowSolo: true })
const emit = defineEmits<{ send: []; 'remove-pick': [index: number]; preview: [src: string, label: string] }>()
const { t } = useI18n()
const { attach, mention, input, composing, textareaComponent, defaultRepo } = props.draft
const { fileInput } = attach
const { picker, trigger, pickerTab, tokens } = mention
const plusOpen = ref(false)
const plus = ref<HTMLElement>()

function onKeydown(event: KeyboardEvent) {
  if (mention.onKeydown(event)) return
  if (event.key !== 'Enter' || event.shiftKey || composing.value || event.isComposing || event.keyCode === 229) return
  event.preventDefault()
  if (!props.disabled) emit('send')
}

function onPaste(event: ClipboardEvent) {
  attach.onPaste(event)
  mention.onPaste(event)
}

function pickFiles() {
  plusOpen.value = false
  fileInput.value?.click()
}

function openPicker(tab: ReferenceTab) {
  plusOpen.value = false
  mention.open(tab)
}

function onPointerDown(event: Event) {
  if (!plus.value?.contains(event.target as Node)) plusOpen.value = false
}

onMounted(() => document.addEventListener('pointerdown', onPointerDown, true))
onBeforeUnmount(() => document.removeEventListener('pointerdown', onPointerDown, true))
</script>

<template>
  <AttachmentDropZone :disabled="disabled" @files="attach.addFiles">
    <p v-if="attach.notice.value" class="mb-2 text-[12px]" :class="attach.notice.value.kind === 'error' ? 'text-err' : 'text-ok'" role="status">
      {{ attach.notice.value.text }}
    </p>
    <TransitionGroup v-if="tokens.length" tag="div" name="ref-chip" class="mb-2 flex flex-wrap gap-1.5" :data-testid="`${testPrefix}-composer-references`">
      <ReferenceChip
        v-for="(token, index) in tokens"
        :key="token.ref.target"
        :reference="token.ref"
        :default-repo="defaultRepo"
        removable
        @remove="mention.drop(index)"
      />
    </TransitionGroup>
    <ChatAttachments :images="attach.attachments.value" @preview="(...args) => emit('preview', ...args)" @remove="attach.removeAttachment" />
    <div v-if="picks.length" class="mb-2 flex flex-wrap gap-1.5">
      <AnnotationChip v-for="(pick, index) in picks" :key="index" :ann="pick" removable :test-id="pickTestId" @remove="emit('remove-pick', index)" />
    </div>
    <div class="relative">
      <ComposerShell wrap-toolbar :show-footer="false">
        <template #input>
          <Transition name="ref-pop">
            <ReferencePicker
              v-if="trigger && !disabled"
              ref="picker"
              :scope="scope"
              :scoped-search="scopedSearch"
              :allow-solo="allowSolo"
              :query="trigger.query"
              :taken="mention.targets.value"
              :full="mention.full.value"
              :initial-tab="pickerTab"
              @pick="mention.pick"
            />
          </Transition>
          <input ref="fileInput" type="file" multiple class="hidden" @change="attach.onPickFiles" />
          <ChatComposerTextarea
            ref="textareaComponent"
            v-model="input"
            :disabled="disabled"
            :placeholder="placeholder"
            :aria-label="placeholder"
            :data-testid="`${testPrefix}-input`"
            @keydown="onKeydown"
            @input="mention.updateTrigger"
            @click="mention.updateTrigger"
            @keyup="(e: KeyboardEvent) => (e.key === 'ArrowLeft' || e.key === 'ArrowRight' || e.key === 'Home' || e.key === 'End') && mention.updateTrigger()"
            @blur="trigger = null"
            @compositionstart="composing = true"
            @compositionend="composing = false; mention.updateTrigger()"
            @paste="onPaste"
          />
        </template>
        <template #toolbar-start>
          <div class="flex min-w-0 items-center gap-2">
            <div ref="plus" class="relative">
              <button
                type="button"
                class="chat-action-button"
                :disabled="disabled"
                :aria-label="t('pages.solo.chat.attach')"
                :title="t('pages.solo.chat.attach')"
                aria-haspopup="menu"
                :aria-expanded="plusOpen"
                :data-testid="`${testPrefix}-plus`"
                @click="plusOpen = !plusOpen"
              >
                <Icon name="plus" :size="18" />
              </button>
              <ReferencePlusMenu :open="plusOpen" :allow-solo="allowSolo" :test-prefix="`${testPrefix}-plus`" @upload="pickFiles" @reference="openPicker" @pick="plusOpen = false" />
            </div>
            <slot name="model" />
          </div>
        </template>
        <template #toolbar-end>
          <slot name="actions" />
        </template>
      </ComposerShell>
    </div>
  </AttachmentDropZone>
</template>
