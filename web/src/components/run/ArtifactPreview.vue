<script setup lang="ts">
import { computed } from 'vue'
import Icon from '../ui/Icon.vue'
import AppModal from '../ui/AppModal.vue'
import AppButton from '../ui/AppButton.vue'
import RefreshStrip from './RefreshStrip.vue'
import HardLoadLayer from './HardLoadLayer.vue'
import HtmlPreview from '../ui/HtmlPreview.vue'
import StructuredArtifactView from './StructuredArtifactView.vue'
import SelectionAddToChat from './SelectionAddToChat.vue'

import ArtifactVersionSelect from '@/components/ui/ArtifactVersionSelect.vue'
import { useArtifactPreview } from '@/lib/run/useArtifactPreview'
import {
  artifactFriendlyNameKey,
  artifactTechnicalDisplayName,
} from '@/lib/run/reactArtifactPreview'
import type { ArtifactPreviewProps, ArtifactPreviewEmit } from '@/lib/run/useArtifactPreview'

const props = withDefaults(defineProps<ArtifactPreviewProps>(), {
  scope: 'run',
  emptyHint: '',
  artifacts: () => [],
  run: null,
  hideDelete: false,
  hideCopy: false,
  hideZoom: false,
  hideExport: false,
  hideVersionChip: false,
  hideFileRow: false,
  annotatable: false,
  shareToken: '',
  previewActive: true,
})
const emit = defineEmits<ArtifactPreviewEmit>()

const {
  t,
  toast,
  annotateChannel,
  stageEl,
  zoom,
  loading,
  loadErr,
  copying,
  structuredMode,
  contentLoadGen,
  contentCache,
  imageSrc,
  imageDownloadLoading,
  imageDownloadError,
  imageLoadGen,
  showDeleteConfirm,
  deleting,
  deleteError,
  structuredExportRootInline,
  structuredExportRootZoom,
  exporting,
  selectedVersionIndex,
  versionChoices,
  showVersionChip,
  selectedChoice,
  displayArtifact,
  viewingHistorical,
  versionChipLabel,
  currentChipLabel,
  selectVersion,
  activeContent,
  activeIsHtml,
  activeIsJson,
  jsonState,
  previewBranch,
  isStructuredPreview,
  showStructuredUi,
  showRawJson,
  structuredDoc,
  isImageBranch,
  canAnnotate,
  quoteAnnotate,
  blockZoomGesture,
  showDelete,
  stageAnnotation,
  onHtmlPick,
  onQuoteAdd,
  exportDisabled,
  revokeImageBlob,
  resetImageDownloadState,
  loadImageDownload,
  retryImageDownload,
  handleImageLoadError,
  loadContent,
  copyContent,
  download,
  resolveExportRoot,
  runStructuredExport,
  downloadPng,
  downloadPdf,
  openDeleteConfirm,
  closeDeleteConfirm,
  mapDeleteError,
  confirmDelete,
  renderMarkdown,
  fmtTime,
} = useArtifactPreview(props, emit)

const friendlyName = computed(() => {
  const key = artifactFriendlyNameKey(displayArtifact.value?.name)
  return key ? t(key) : ''
})
const technicalName = computed(() => artifactTechnicalDisplayName(displayArtifact.value?.name))
const displayName = computed(() => friendlyName.value || technicalName.value)
/** The file name already carries its extension, so the kind label would just repeat it. */
const fileMeta = computed(() => {
  if (friendlyName.value) return technicalName.value
  return /\.[a-z0-9]+$/i.test(technicalName.value) ? '' : displayArtifact.value?.kind || ''
})
const modalTitle = computed(() =>
  friendlyName.value ? `${friendlyName.value} · ${technicalName.value}` : technicalName.value,
)
/** Cover missing HTML content without hiding the preview controls. */
const htmlCanvasOverlay = computed(
  () => activeIsHtml.value && (!!loadErr.value || !activeContent.value),
)
</script>

<template>
  <div class="artifact-preview flex h-full min-h-0 min-w-0 flex-1 flex-col" data-testid="artifact-preview">
    <div v-if="displayArtifact && !activeIsHtml" class="artifact-preview-header" data-testid="artifact-preview-identity-bar">
      <div v-if="!hideFileRow" class="artifact-preview-file-row" data-testid="artifact-preview-file-row">
        <Icon name="doc" :size="17" class="shrink-0 text-txt3" />
        <div class="min-w-0 flex-1">
          <p class="artifact-preview-file-name" :title="technicalName">{{ displayName }}</p>
          <p v-if="fileMeta" class="artifact-preview-file-meta" data-testid="artifact-preview-file-meta"><span :class="{ uppercase: !friendlyName }" :title="technicalName">{{ fileMeta }}</span></p>
        </div>
        <div class="artifact-preview-revisions" data-testid="artifact-preview-revisions">
          <ArtifactVersionSelect
            v-if="showVersionChip"
            :choices="versionChoices"
            :selected-index="selectedChoice?.index"
            :current-label="currentChipLabel"
            :menu-aria-label="t('pages.reactArtifactStage.versionMenu')"
            chip-test-id="artifact-preview-version-chip"
            button-test-id="artifact-preview-version-chip-btn"
            menu-test-id="artifact-preview-version-menu"
            option-test-id-prefix="artifact-preview-version-option-v"
            :label-for="versionChipLabel"
            @select="selectVersion"
          />
          <span v-if="viewingHistorical" class="artifact-preview-readonly" data-testid="artifact-preview-historical-readonly">{{ t('pages.reactArtifactStage.readonlyBadge') }}</span>
        </div>
      </div>
      <div class="artifact-preview-toolbar" data-testid="artifact-preview-toolbar">
        <div v-if="isStructuredPreview" class="artifact-preview-view-options" data-testid="artifact-preview-mode-toggle" role="group" :aria-label="t('pages.artifactPreview.modeToggleAria')">
          <button type="button" :class="{ 'is-selected': structuredMode === 'structured' }" :aria-pressed="structuredMode === 'structured'" data-testid="artifact-preview-mode-structured" @click="structuredMode = 'structured'">{{ t('pages.artifactPreview.modeStructured') }}</button>
          <button type="button" :class="{ 'is-selected': structuredMode === 'raw' }" :aria-pressed="structuredMode === 'raw'" data-testid="artifact-preview-mode-raw" @click="structuredMode = 'raw'">{{ t('pages.artifactPreview.modeRaw') }}</button>
        </div>
        <div class="artifact-preview-actions" data-testid="artifact-preview-actions" role="group" :aria-label="t('common.table.actions')">
          <button v-if="!hideZoom" type="button" class="artifact-preview-icon-action" :title="t('pages.artifactPreview.enlarge')" :aria-label="t('pages.artifactPreview.enlarge')" data-testid="artifact-preview-zoom" @click="zoom = true"><Icon name="expand" :size="15" /></button>
          <button v-if="!hideCopy" type="button" class="artifact-preview-icon-action" :title="copying ? t('common.buttons.copying') : t('pages.artifactPreview.copy')" :aria-label="copying ? t('common.buttons.copying') : t('pages.artifactPreview.copy')" :disabled="copying" :aria-busy="copying ? 'true' : undefined" data-testid="artifact-preview-copy" @click="copyContent"><Icon :name="copying ? 'spinner' : 'copy'" :size="15" :class="copying ? 'animate-spin' : ''" aria-hidden="true" /></button>
          <button type="button" class="artifact-preview-icon-action" :title="t('pages.artifactPreview.downloadRawTitle')" :aria-label="t('pages.artifactPreview.downloadRawTitle')" data-testid="artifact-preview-download-raw" @click="download"><Icon name="download" :size="15" /></button>
          <button v-if="!hideExport && isStructuredPreview" type="button" class="artifact-preview-text-action" data-testid="artifact-preview-download-png" :disabled="exportDisabled" @click="downloadPng">{{ exporting ? t('pages.artifactPreview.exporting') : t('pages.artifactPreview.downloadPng') }}</button>
          <button v-if="!hideExport && isStructuredPreview" type="button" class="artifact-preview-text-action" data-testid="artifact-preview-download-pdf" :disabled="exportDisabled" @click="downloadPdf">{{ exporting ? t('pages.artifactPreview.exporting') : t('pages.artifactPreview.downloadPdf') }}</button>
          <button v-if="showDelete" type="button" class="artifact-preview-icon-action artifact-preview-delete-action" :title="t('pages.artifactPreview.delete')" :aria-label="t('pages.artifactPreview.delete')" data-testid="artifact-preview-delete" @click="openDeleteConfirm"><Icon name="trash" :size="15" /></button>
        </div>
      </div>
    </div>
    <div
      ref="stageEl"
      class="flex-1 overflow-hidden"
      data-review-annotate-stage
      :class="activeIsHtml ? 'flex min-h-0 flex-col' : 'scroll-area overflow-y-auto p-4'"
    >
      <template v-if="displayArtifact && isImageBranch">
        <div v-if="loadErr" class="flex h-full items-center justify-center text-center text-[12px] text-err" role="alert">
          {{ t('pages.artifactPreview.loadFailed') }}
        </div>
        <div
          v-else-if="imageDownloadError"
          class="flex h-full flex-col items-center justify-center gap-3 text-center"
          data-testid="artifact-preview-image-error"
        >
          <div class="text-[13px] font-medium text-err">{{ t('pages.artifactPreview.imageLoadFailed') }}</div>
          <AppButton variant="outline" data-testid="artifact-preview-image-retry" @click="retryImageDownload">
            {{ t('pages.artifactPreview.retry') }}
          </AppButton>
        </div>
        <div
          v-else-if="loading || imageDownloadLoading || !imageSrc"
          class="flex h-full items-center justify-center text-[12px] text-txt3"
          data-testid="artifact-preview-image-loading"
        >
          {{ t('pages.artifactPreview.loading') }}
        </div>
        <div
          v-else
          class="flex h-full min-h-48 w-full items-center justify-center py-3"
          data-testid="artifact-preview-image-wrap"
        >
          <img
            :src="imageSrc!"
            :alt="displayArtifact.name"
            class="max-h-full max-w-full object-contain"
            @error="handleImageLoadError"
          />
        </div>
      </template>
      <template v-else-if="displayArtifact">
        <RefreshStrip v-if="loading && activeContent" />
        <HardLoadLayer
          v-else-if="!activeIsHtml && loading && !activeContent && !loadErr"
          :overlay="false"
          :stuck-after-ms="10_000"
          :stage="t('pages.artifactPreview.loading')"
          @retry="displayArtifact && loadContent(displayArtifact, { force: true })"
        />
        <div
          v-if="!activeIsHtml && loadErr"
          class="mb-2 flex items-center justify-center gap-2 text-center text-[12px] text-err"
          role="alert"
          data-testid="artifact-preview-load-error"
        >
          {{ t('pages.artifactPreview.loadFailed') }}
          <button
            type="button"
            class="rounded-lg inline-flex min-h-11 items-center border border-line px-3 text-[12px] text-txt"
            @click="displayArtifact && loadContent(displayArtifact, { force: true })"
          >
            {{ t('pages.artifactPreview.retry') }}
          </button>
        </div>
        <HtmlPreview
          v-if="activeIsHtml"
          :html="activeContent"
          fill-parent
          :inspectable="canAnnotate"
          :modal-title="modalTitle"
          :presented="previewActive"
          class="h-full min-h-0"
          @pick="onHtmlPick"
        >
          <template #toolbar-actions>
            <ArtifactVersionSelect
              v-if="showVersionChip"
              :choices="versionChoices"
              :selected-index="selectedChoice?.index"
              :current-label="currentChipLabel"
              :menu-aria-label="t('pages.reactArtifactStage.versionMenu')"
              chip-test-id="artifact-preview-version-chip"
              button-test-id="artifact-preview-version-chip-btn"
              menu-test-id="artifact-preview-version-menu"
              option-test-id-prefix="artifact-preview-version-option-v"
              :label-for="versionChipLabel"
              @select="selectVersion"
            />
            <span
              v-if="viewingHistorical"
              class="shrink-0 rounded-md border border-line px-1 py-px text-[10px] text-txt3"
              data-testid="artifact-preview-historical-readonly"
            >{{ t('pages.reactArtifactStage.readonlyBadge') }}</span>
            <button
              v-if="showDelete"
              type="button"
              class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-txt3 hover:bg-overlay hover:text-err"
              :title="t('pages.artifactPreview.delete')"
              :aria-label="t('pages.artifactPreview.delete')"
              data-testid="artifact-preview-delete"
              @click="openDeleteConfirm"
            ><Icon name="trash" :size="15" /></button>
            <button
              v-if="!hideCopy"
              type="button"
              class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-txt3 hover:bg-overlay hover:text-txt disabled:opacity-50"
              :title="copying ? t('common.buttons.copying') : t('pages.artifactPreview.copy')"
              :aria-label="copying ? t('common.buttons.copying') : t('pages.artifactPreview.copy')"
              :disabled="copying"
              :aria-busy="copying ? 'true' : undefined"
              data-testid="artifact-preview-copy"
              @click="copyContent"
            ><Icon :name="copying ? 'spinner' : 'copy'" :size="15" :class="copying ? 'animate-spin' : ''" aria-hidden="true" /></button>
            <button
              type="button"
              class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-txt3 hover:bg-overlay hover:text-txt"
              :title="t('pages.artifactPreview.download')"
              :aria-label="t('pages.artifactPreview.download')"
              data-testid="artifact-preview-download-raw"
              @click="download"
            ><Icon name="download" :size="15" /></button>
          </template>
          <template v-if="htmlCanvasOverlay" #canvas-overlay>
            <HardLoadLayer
              v-if="loading && !activeContent && !loadErr"
              :stuck-after-ms="10_000"
              :stage="t('pages.artifactPreview.loading')"
              @retry="displayArtifact && loadContent(displayArtifact, { force: true })"
            />
            <div
              v-else-if="loadErr"
              class="flex h-full items-center justify-center gap-2 px-4 text-center text-[12px] text-err"
              role="alert"
              data-testid="artifact-preview-load-error"
            >
              {{ t('pages.artifactPreview.loadFailed') }}
              <button
                type="button"
                class="rounded-lg inline-flex min-h-11 items-center border border-line px-3 text-[12px] text-txt"
                @click="displayArtifact && loadContent(displayArtifact, { force: true })"
              >
                {{ t('pages.artifactPreview.retry') }}
              </button>
            </div>
            <div
              v-else
              class="flex h-full items-center justify-center px-4 text-center text-[12px] text-txt3"
            >
              {{ t('pages.artifactPreview.contentEmpty') }}
            </div>
          </template>
        </HtmlPreview>
        <div
          v-else-if="showStructuredUi"
          ref="structuredExportRootInline"
          data-testid="structured-artifact-export-root"
          class="structured-artifact-export-root"
        >
          <StructuredArtifactView
            :name="displayArtifact.name"
            :doc="structuredDoc"
            :artifacts="artifacts"
            :run-id="runId || displayArtifact.runId"
          />
        </div>
        <div
          v-else-if="showRawJson && jsonState"
          class="json-code-view scroll-area"
          :class="blockZoomGesture ? '' : 'cursor-zoom-in'"
          data-testid="artifact-preview-raw-json"
          @dblclick="!blockZoomGesture && (zoom = true)"
        >
          <div v-if="!jsonState.ok" class="fallback-tag">{{ t('pages.artifactPreview.fallbackPlainText') }}</div>
          <pre v-html="jsonState.html" />
        </div>
        <div
          v-else-if="previewBranch.kind === 'markdown' && activeContent"
          class="md"
          :class="blockZoomGesture ? '' : 'cursor-zoom-in'"
          v-html="renderMarkdown(activeContent)"
          @dblclick="!blockZoomGesture && (zoom = true)"
        />
        <div
          v-else-if="!loading && !loadErr"
          class="flex h-full items-center justify-center text-center text-[12px] text-txt3"
        >
          {{ t('pages.artifactPreview.contentEmpty') }}
        </div>
      </template>
      <div v-else class="flex h-full flex-col items-center justify-center gap-3 px-5 text-center text-[12px] leading-6 text-txt3">
        <Icon name="doc" :size="26" class="opacity-60" />
        <p>{{ emptyHint || t('pages.artifactPreview.emptyHint') }}</p>
      </div>
    </div>
  </div>

  <SelectionAddToChat
    v-if="quoteAnnotate"
    :enabled="quoteAnnotate"
    :root="stageEl"
    @add="onQuoteAdd"
  />

  <AppModal :open="zoom" :title="modalTitle" :width="960" @close="zoom = false">
    <div
      v-if="displayArtifact && showStructuredUi"
      ref="structuredExportRootZoom"
      data-testid="structured-artifact-export-root-zoom"
      class="structured-artifact-export-root"
    >
      <StructuredArtifactView
        :name="displayArtifact.name"
        :doc="structuredDoc"
        :artifacts="artifacts"
        :run-id="runId || displayArtifact.runId"
      />
    </div>
    <div
      v-else-if="displayArtifact && showRawJson"
      class="json-code-view json-code-view--modal scroll-area -m-5 min-h-[280px] p-5"
      data-testid="artifact-preview-zoom-raw-json"
    >
      <template v-if="jsonState">
        <div v-if="!jsonState.ok" class="fallback-tag">{{ t('pages.artifactPreview.fallbackPlainText') }}</div>
        <pre v-html="jsonState.html" />
      </template>
      <div v-else class="py-8 text-center text-[12px] text-txt3">
        {{ t('pages.artifactPreview.contentEmpty') }}
      </div>
    </div>
    <div
      v-else-if="displayArtifact && previewBranch.kind === 'image'"
      class="flex min-h-[280px] items-center justify-center"
      data-testid="artifact-preview-zoom-image"
    >
      <div v-if="loadErr" class="py-8 text-center text-[12px] text-err" role="alert">
        {{ t('pages.artifactPreview.loadFailed') }}
      </div>
      <div
        v-else-if="imageDownloadError"
        class="flex flex-col items-center justify-center gap-3 py-8 text-center"
      >
        <div class="text-[13px] font-medium text-err">{{ t('pages.artifactPreview.imageLoadFailed') }}</div>
        <AppButton variant="outline" @click="retryImageDownload">
          {{ t('pages.artifactPreview.retry') }}
        </AppButton>
      </div>
      <div
        v-else-if="loading || imageDownloadLoading || !imageSrc"
        class="py-8 text-center text-[12px] text-txt3"
      >
        {{ t('pages.artifactPreview.loading') }}
      </div>
      <div v-else class="flex h-full min-h-[360px] w-full items-center justify-center p-3">
        <img
          :src="imageSrc!"
          :alt="displayArtifact.name"
          class="max-h-[70vh] max-w-full object-contain"
          @error="handleImageLoadError"
        />
      </div>
    </div>
    <div v-else-if="displayArtifact" class="md mx-auto max-w-3xl" v-html="renderMarkdown(activeContent)" />
    <template #footer>
      <span class="mr-auto text-[11px] text-txt3">
        <template v-if="scope === 'platform' && displayArtifact?.workflowName">{{ displayArtifact.workflowName }} · </template>
        {{ displayArtifact?.nodeId }} · {{ displayArtifact ? fmtTime(displayArtifact.createdAt) : '' }}
      </span>
      <button
        v-if="!hideCopy"
        class="chip hover:text-txt"
        data-testid="artifact-preview-zoom-copy"
        @click="copyContent"
      >
        <Icon name="copy" :size="13" />{{ t('pages.artifactPreview.copy') }}
      </button>
      <button
        class="chip hover:text-txt"
        :title="t('pages.artifactPreview.downloadRawTitle')"
        data-testid="artifact-preview-zoom-download-raw"
        @click="download"
      >
        <Icon name="download" :size="13" />{{ t('pages.artifactPreview.download') }}
      </button>
      <button
        v-if="!hideExport && isStructuredPreview"
        type="button"
        class="chip hover:text-txt disabled:cursor-not-allowed disabled:opacity-45"
        data-testid="artifact-preview-zoom-download-png"
        :disabled="exportDisabled"
        @click="downloadPng"
      >
        {{ exporting ? t('pages.artifactPreview.exporting') : t('pages.artifactPreview.downloadPng') }}
      </button>
      <button
        v-if="!hideExport && isStructuredPreview"
        type="button"
        class="chip hover:text-txt disabled:cursor-not-allowed disabled:opacity-45"
        data-testid="artifact-preview-zoom-download-pdf"
        :disabled="exportDisabled"
        @click="downloadPdf"
      >
        {{ exporting ? t('pages.artifactPreview.exporting') : t('pages.artifactPreview.downloadPdf') }}
      </button>
    </template>
  </AppModal>

  <AppModal
    :open="showDeleteConfirm"
    :title="t('pages.artifactPreview.deleteTitle', { name: artifact?.name || '' })"
    :width="440"
    @close="closeDeleteConfirm"
  >
    <div class="space-y-3 text-sm text-txt2">
      <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
        <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
        {{ t('pages.artifactPreview.deleteWarning') }}
      </div>
      <p>{{ t('pages.artifactPreview.deleteConfirm', { name: artifact?.name || '' }) }}</p>
      <div v-if="deleteError" class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
        <Icon name="alert" :size="14" class="mt-0.5" />{{ deleteError }}
      </div>
    </div>
    <template #footer>
      <AppButton variant="ghost" :disabled="deleting" @click="closeDeleteConfirm">{{ t('common.buttons.cancel') }}</AppButton>
      <AppButton variant="danger" icon="trash" :disabled="deleting" @click="confirmDelete">
        {{ deleting ? t('common.buttons.deleting') : t('common.buttons.confirmDelete') }}
      </AppButton>
    </template>
  </AppModal>
</template>

<style scoped>
.artifact-preview-header { flex: none; border-bottom: 1px solid rgb(var(--c-line) / .7); }
.artifact-preview-file-row { display: flex; min-width: 0; align-items: center; gap: 10px; padding: 12px 16px 8px; }
.artifact-preview-file-name { overflow: hidden; color: rgb(var(--c-txt)); font-size: 13px; font-weight: 500; line-height: 20px; text-overflow: ellipsis; white-space: nowrap; }
.artifact-preview-file-meta { display: flex; min-width: 0; overflow: hidden; color: rgb(var(--c-txt3)); font-size: 10px; line-height: 17px; white-space: nowrap; }
.artifact-preview-file-meta > span { overflow: hidden; text-overflow: ellipsis; }
.artifact-preview-revisions { display: flex; max-width: 50%; flex: none; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 5px; }
.artifact-preview-revisions :deep(button) { min-height: 26px; border-color: transparent; background: rgb(var(--c-txt) / .04); padding-inline: 7px; }
.artifact-preview-readonly { color: rgb(var(--c-txt3)); font-size: 10px; white-space: nowrap; }
.artifact-preview-toolbar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 6px 12px; padding: 0 12px 8px; }
/* Without the file row (hideFileRow) the toolbar leads the header. */
.artifact-preview-toolbar:first-child { padding-top: 8px; }
.artifact-preview-view-options { display: inline-flex; flex: none; align-items: center; gap: 2px; }
.artifact-preview-view-options > button { min-height: 30px; border-radius: 6px; padding: 4px 8px; color: rgb(var(--c-txt3)); font-size: 11px; white-space: nowrap; }
.artifact-preview-view-options > button.is-selected { background: rgb(var(--c-txt) / .055); color: rgb(var(--c-txt)); }
.artifact-preview-actions { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 2px; margin-left: auto; }
.artifact-preview-icon-action { display: inline-flex; width: 30px; height: 30px; flex: none; align-items: center; justify-content: center; border-radius: 6px; color: rgb(var(--c-txt3)); }
.artifact-preview-text-action { min-height: 30px; border-radius: 6px; padding: 4px 6px; color: rgb(var(--c-txt2)); font-size: 11px; white-space: nowrap; }
.artifact-preview-toolbar button:hover { background: rgb(var(--c-txt) / .04); color: rgb(var(--c-txt)); }
.artifact-preview-toolbar button:disabled { cursor: not-allowed; opacity: .45; }
.artifact-preview-delete-action { margin-left: 5px; }
.artifact-preview-toolbar .artifact-preview-delete-action:hover { color: rgb(var(--c-err)); }
.artifact-preview :deep([data-testid="html-preview-toolbar"]) { padding-block: 10px; border-color: rgb(var(--c-line) / .7); background: transparent; }
@media (max-width: 767px) {
  .artifact-preview-file-row { padding-inline: 12px; }
  .artifact-preview-toolbar { padding-inline: 8px; }
  .artifact-preview-view-options > button, .artifact-preview-text-action { min-height: 36px; }
  .artifact-preview-icon-action { width: 36px; height: 36px; }
}
.json-code-view {
  padding: 12px 14px;
  overflow: auto;
  max-height: 100%;
}
.json-code-view--modal {
  border: none;
  max-height: none;
}
.fallback-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 10px;
  padding: 3px 8px;
  font-size: 11px;
  color: rgb(var(--c-warn, 251 191 36));
  border: 1px solid rgba(251, 191, 36, 0.35);
  background: rgba(251, 191, 36, 0.08);
}
</style>
