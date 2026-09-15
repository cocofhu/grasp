<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CommentPin } from '@/lib/inbox/useCommentPins'
import { formatAnnotationArtifactPreview } from '@/lib/inbox/useCommentPins'

const props = defineProps<{
  pins: CommentPin[]
  selectedId?: string | null
  artifactCommitted: boolean
  writing?: boolean
  writeError?: string | null
}>()

const emit = defineEmits<{
  (e: 'select', pinId: string): void
  (e: 'edit', pinId: string): void
  (e: 'delete', pinId: string): void
  (e: 'write'): void
}>()

const { t } = useI18n()
const tab = ref<'comments' | 'artifact'>('comments')

const sortedPins = computed(() => [...props.pins].sort((a, b) => a.seq - b.seq))
const previewText = computed(() => formatAnnotationArtifactPreview(props.pins))
const canWrite = computed(() => props.pins.length > 0 && !props.writing)

function switchTab(name: 'comments' | 'artifact') {
  tab.value = name
}
</script>

<template>
  <div
    class="rounded-lg flex min-h-0 flex-1 flex-col overflow-hidden border border-line bg-surface"
    data-testid="comment-artifact-sidebar"
  >
    <div class="flex shrink-0 border-b border-line">
      <button
        type="button"
        class="flex-1 border-b-2 px-2 py-2.5 text-[13px]"
        :class="
          tab === 'comments'
            ? 'border-accent bg-accent-dim text-txt'
            : 'border-transparent text-txt3'
        "
        data-testid="comment-tab-comments"
        @click="switchTab('comments')"
      >
        {{ t('pages.gateApproval.commentPins.tabComments') }}
      </button>
      <button
        type="button"
        class="flex-1 border-b-2 px-2 py-2.5 text-[13px]"
        :class="
          tab === 'artifact'
            ? 'border-accent bg-accent-dim text-txt'
            : 'border-transparent text-txt3'
        "
        data-testid="comment-tab-artifact"
        @click="switchTab('artifact')"
      >
        {{ t('pages.gateApproval.commentPins.tabArtifact') }}
      </button>
    </div>

    <div
      v-if="artifactCommitted"
      class="rounded-lg mx-3 mt-2 shrink-0 border border-warn/35 bg-warn/10 px-2.5 py-2 text-[12px] text-warn"
      data-testid="comment-artifact-committed-banner"
    >
      {{ t('pages.gateApproval.commentPins.committedBanner') }}
    </div>

    <div
      v-show="tab === 'comments'"
      class="flex min-h-0 flex-1 flex-col gap-2 overflow-auto p-3"
      data-testid="comment-panel-comments"
    >
      <div
        v-if="!sortedPins.length"
        class="rounded-lg border border-dashed border-line px-3 py-7 text-center text-[13px] text-txt3"
        data-testid="comment-pins-empty"
      >
        {{ t('pages.gateApproval.commentPins.empty') }}
      </div>
      <div v-else class="flex flex-col gap-2" data-testid="comment-pins-list">
        <div
          v-for="pin in sortedPins"
          :key="pin.id"
          class="rounded-lg cursor-pointer border border-line bg-elevated p-2.5"
          :class="pin.id === selectedId ? 'border-accent bg-accent-dim' : 'hover:border-line-strong'"
          :data-testid="'comment-pin-item-' + pin.seq"
          @click="emit('select', pin.id)"
        >
          <div class="mb-1.5 flex items-center justify-between gap-2">
            <span class="font-mono text-xs text-accent">#{{ pin.seq }}</span>
            <span class="rounded-md border border-line px-1.5 text-[11px] text-txt2">
              {{ t('pages.gateApproval.commentPins.badgeLabel') }}
            </span>
          </div>
          <p class="mb-1.5 whitespace-pre-wrap text-[13px] text-txt">{{ pin.comment }}</p>
          <div class="truncate font-mono text-[11px] text-txt3" :title="pin.selector">
            {{ pin.selector }}
          </div>
          <div class="mt-1 text-[11px] text-txt3">
            {{
              pin.screenshot === 'present'
                ? t('pages.gateApproval.commentPins.hasShot')
                : t('pages.gateApproval.commentPins.noShot')
            }}
            · {{ t('pages.gateApproval.commentPins.notAgent') }}
          </div>
          <div class="mt-2 flex gap-1.5">
            <button
              type="button"
              class="rounded-md border border-line px-2 py-1 text-[11px] text-txt2 hover:text-txt"
              @click.stop="emit('edit', pin.id)"
            >
              {{ t('pages.gateApproval.commentPins.edit') }}
            </button>
            <button
              type="button"
              class="rounded-md border border-err/35 px-2 py-1 text-[11px] text-err hover:bg-err/10"
              data-testid="comment-pin-delete"
              @click.stop="emit('delete', pin.id)"
            >
              {{ t('pages.gateApproval.commentPins.delete') }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <div
      v-show="tab === 'artifact'"
      class="flex min-h-0 flex-1 flex-col gap-2 overflow-auto p-3"
      data-testid="comment-panel-artifact"
    >
      <div class="text-[11px] text-txt3" data-testid="comment-artifact-meta">
        {{
          artifactCommitted
            ? t('pages.gateApproval.commentPins.metaCommitted')
            : t('pages.gateApproval.commentPins.metaDraft', { n: pins.length })
        }}
      </div>
      <pre
        class="rounded-lg max-h-[260px] overflow-auto border border-line bg-base p-2.5 font-mono text-[11px] leading-relaxed text-txt2 whitespace-pre-wrap"
        data-testid="comment-artifact-preview"
      >{{ previewText }}</pre>
      <button
        type="button"
        class="w-full bg-accent px-3 py-2 text-xs font-medium text-white hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-45"
        :disabled="!canWrite"
        data-testid="comment-artifact-write"
        @click="emit('write')"
      >
        {{
          writing
            ? t('pages.gateApproval.commentPins.writing')
            : t('pages.gateApproval.commentPins.writeArtifact')
        }}
      </button>
      <p v-if="writeError" class="text-[11px] text-err" role="alert">{{ writeError }}</p>
      <p class="text-[11px] text-txt3">
        {{ t('pages.gateApproval.commentPins.hardScopeHint') }}
      </p>
    </div>
  </div>
</template>
