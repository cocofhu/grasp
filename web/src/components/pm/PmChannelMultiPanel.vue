<script setup lang="ts">
import AppButton from '@/components/ui/AppButton.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import SettingsRow from '@/components/settings/SettingsRow.vue'
import Icon from '@/components/ui/Icon.vue'
import { usePmChannelMulti } from '@/lib/pm/usePmChannelMulti'
import type { Workspace } from '@/lib/shared/types'

const props = defineProps<{
  workspace: Workspace
  pmLeaderAgent?: string
}>()

const emit = defineEmits<{
  'workspace-updated': [workspace: Workspace]
}>()

const {
  t,
  toast,
  PM_MCP_OPTIONS,
  tab,
  loading,
  saving,
  channelList,
  freeAgents,
  secretsKeyConfigured,
  editingId,
  isNew,
  chType,
  saveError,
  notifyReceipts,
  chRegion,
  chMarkdown,
  chIntents,
  chEnabled,
  chName,
  chAgent,
  chAppId,
  chAppSecret,
  chAppSecretSet,
  chTurnTimeout,
  chSandbox,
  chAllowMemoryWrite,
  chAllowSchedulerWrite,
  chCronDeliver,
  chCronDeliverTarget,
  chEnabledMcps,
  chIsPrimary,
  notifySelected,
  notifySaving,
  deleteOpen,
  deleteTarget,
  deleteMode,
  deleteNewPrimaryId,
  deleteSyncPmLeader,
  TARGET_LISTBOX_ID,
  recentTargets,
  recentTargetsLoaded,
  recentTargetsLoading,
  targetComboOpen,
  targetComboRoot,
  targetActiveIndex,
  hasPrimary,
  addButtonLabel,
  agentOptions,
  editingChannel,
  clearRecentTargetsCache,
  ensureRecentTargets,
  setTargetComboOpen,
  selectPushTarget,
  onTargetComboDocClick,
  onTargetInputKeydown,
  toggleChMcp,
  defaultChannelName,
  resetForm,
  applyForm,
  load,
  openAdd,
  openEdit,
  cancelEdit,
  setChannelType,
  buildInput,
  connectionLabel,
  connectionClass,
  connectionDotClass,
  formConnectionHint,
  saveChannel,
  askDelete,
  confirmDeletePrimary,
  doDelete,
  toggleNotify,
  saveNotifyTargets,
  pushTargetPrimaryLabel
} = usePmChannelMulti(props, emit)

</script>

<template>
  <div data-testid="pm-channel-multi">
    <div class="mb-3 flex items-start justify-between gap-3">
      <div class="min-w-0">
        <h2 class="m-0 text-[15px] font-semibold leading-6 text-txt">
          {{ t('pages.workspace.pm.channel.sectionTitle') }}
        </h2>
        <p class="m-0 mt-1 text-[13px] leading-5 text-txt3">
          {{ t('pages.workspace.pm.channel.sectionHint') }}
        </p>
      </div>
    </div>

    <div class="mb-3 flex gap-0 border-b border-line" role="tablist">
      <button
        type="button"
        class="border-b-2 px-3.5 py-2.5 text-[13px]"
        :class="
          tab === 'list'
            ? 'border-accent text-txt'
            : 'border-transparent text-txt2 hover:text-txt'
        "
        data-testid="channel-tab-list"
        @click="tab = 'list'"
      >
        {{ t('pages.workspace.pm.channel.tabList') }}
      </button>
      <button
        type="button"
        class="border-b-2 px-3.5 py-2.5 text-[13px]"
        :class="
          tab === 'edit'
            ? 'border-accent text-txt'
            : 'border-transparent text-txt2 hover:text-txt'
        "
        data-testid="channel-tab-edit"
        @click="tab = 'edit'"
      >
        {{ t('pages.workspace.pm.channel.tabEdit') }}
      </button>
      <button
        type="button"
        class="border-b-2 px-3.5 py-2.5 text-[13px]"
        :class="
          tab === 'notify'
            ? 'border-accent text-txt'
            : 'border-transparent text-txt2 hover:text-txt'
        "
        data-testid="channel-tab-notify"
        @click="tab = 'notify'"
      >
        {{ t('pages.workspace.pm.channel.tabNotify') }}
      </button>
    </div>

    <div v-if="loading" class="text-sm text-txt3">{{ t('common.buttons.loading') }}</div>

    <!-- List -->
    <div v-else-if="tab === 'list'" data-testid="channel-panel-list">
      <div class="mb-3 flex items-center justify-between gap-2">
        <p class="m-0 text-xs text-txt2">{{ t('pages.workspace.pm.channel.listHint') }}</p>
        <AppButton variant="primary" data-testid="channel-add" @click="openAdd">
          {{ addButtonLabel }}
        </AppButton>
      </div>

      <div v-if="!channelList.length" class="py-4 text-[13px] text-txt3">
        {{ t('pages.workspace.pm.channel.listEmpty') }}
      </div>
      <div v-else class="border-y border-line/60">
        <div
          class="grid grid-cols-[1fr_100px_140px_80px_120px] gap-3 border-b border-line/60 py-2 text-[11px] uppercase tracking-wide text-txt3 max-md:grid-cols-2"
        >
          <span>{{ t('pages.workspace.pm.channel.colName') }}</span>
          <span>{{ t('pages.workspace.pm.channel.colRole') }}</span>
          <span class="max-md:hidden">{{ t('pages.workspace.pm.channel.colAgent') }}</span>
          <span class="max-md:hidden">{{ t('pages.workspace.pm.channel.colStatus') }}</span>
          <span>{{ t('pages.workspace.pm.channel.colActions') }}</span>
        </div>
        <div
          v-for="ch in channelList"
          :key="ch.id"
          class="grid grid-cols-[1fr_100px_140px_80px_120px] items-center gap-3 border-b border-line/60 py-4 last:border-b-0 max-md:grid-cols-2"
          data-testid="channel-row"
        >
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2 text-[13px] font-medium text-txt">
              <span
                class="rounded-md border px-2 py-0.5 text-[11px]"
                data-testid="channel-type-badge"
                :class="
                  ch.type === 'wecom'
                    ? 'border-accent/55 bg-accent-dim text-accent-2'
                    : ch.type === 'feishu'
                      ? 'border-cyan-400/40 bg-cyan-400/10 text-cyan-300'
                      : ch.type === 'dingtalk'
                        ? 'border-blue-400/40 bg-blue-400/10 text-blue-300'
                        : 'border-sky-400/40 bg-sky-400/10 text-sky-300'
                "
              >
                {{
                  ch.type === 'wecom'
                    ? t('pages.workspace.pm.channel.typeWecom')
                    : ch.type === 'feishu'
                      ? t('pages.workspace.pm.channel.badgeFeishu')
                      : ch.type === 'dingtalk'
                        ? t('pages.workspace.pm.channel.badgeDingTalk')
                        : t('pages.workspace.pm.channel.badgeQQ')
                }}
              </span>
              <span class="truncate">{{ ch.name || ch.appId }}</span>
              <span
                v-if="ch.isPrimary"
                class="rounded-md border border-accent/55 bg-accent-dim px-2 py-0.5 text-[11px] text-accent-2"
              >
                {{ t('pages.workspace.pm.channel.rolePrimary') }}
              </span>
            </div>
            <div class="mt-0.5 font-mono text-[11px] text-txt3">{{ ch.appId }}</div>
          </div>
          <div class="text-[12px] text-txt2">
            {{
              ch.isPrimary
                ? t('pages.workspace.pm.channel.rolePrimary')
                : t('pages.workspace.pm.channel.roleSecondary')
            }}
          </div>
          <div class="truncate font-mono text-[12px] text-txt2 max-md:hidden">
            {{ ch.agentName || '—' }}
          </div>
          <div class="flex flex-col gap-1 text-[12px] max-md:hidden">
            <span :class="ch.enabled ? 'text-ok' : 'text-txt3'">
              {{
                ch.enabled
                  ? t('pages.workspace.pm.channel.statusOn')
                  : t('pages.workspace.pm.channel.statusOff')
              }}
            </span>
            <span class="inline-flex items-center gap-1.5" :class="connectionClass(ch)" data-testid="channel-conn-state">
              <span
                class="inline-block h-1.5 w-1.5"
                :class="connectionDotClass(ch)"
              />
              {{ connectionLabel(ch) }}
            </span>
          </div>
          <div class="flex flex-wrap gap-1.5">
            <AppButton variant="ghost" class="!px-2 !py-1 text-[12px]" @click="openEdit(ch)">
              {{ t('pages.workspace.pm.channel.edit') }}
            </AppButton>
            <AppButton
              variant="ghost"
              class="!px-2 !py-1 text-[12px] text-err"
              :disabled="saving"
              @click="askDelete(ch)"
            >
              {{ t('pages.workspace.pm.channel.delete') }}
            </AppButton>
          </div>
        </div>
      </div>

      <div class="mt-3 text-[13px] leading-5 text-txt3">
        <strong class="text-info">{{ t('pages.workspace.pm.channel.noteTitle') }}</strong>
        {{ t('pages.workspace.pm.channel.noteHint') }}
      </div>
    </div>

    <!-- Edit -->
    <div v-else-if="tab === 'edit'" data-testid="channel-panel-edit">
      <div class="mb-3 flex items-center justify-between gap-2">
        <div>
          <strong class="block text-sm text-txt">
            {{
              isNew
                ? addButtonLabel
                : t('pages.workspace.pm.channel.editTitle')
            }}
          </strong>
          <p class="m-0 mt-1 text-xs text-txt2">
            {{
              chIsPrimary
                ? t('pages.workspace.pm.channel.editPrimaryHint')
                : t('pages.workspace.pm.channel.editSecondaryHint')
            }}
          </p>
        </div>
        <AppButton variant="ghost" @click="cancelEdit">
          {{ t('pages.workspace.pm.channel.backToList') }}
        </AppButton>
      </div>

      <div v-if="!isNew && !editingId" class="py-4 text-[13px] text-txt3">
        {{ t('pages.workspace.pm.channel.editEmpty') }}
      </div>

      <template v-else>
        <div class="space-y-7">
          <div class="min-w-0">
            <h3 class="m-0 text-[14px] font-semibold leading-5 text-txt">
              {{ t('pages.workspace.pm.channel.basicsTitle') }}
            </h3>
            <p class="m-0 mb-3.5 mt-1 text-xs text-txt2">
              {{ t('pages.workspace.pm.channel.basicsHint') }}
            </p>

            <label class="flex items-center justify-between gap-3 text-[13px] text-txt">
              <span>{{ t('pages.workspace.pm.channel.enable') }}</span>
              <AppSwitch v-model="chEnabled" :aria-label="t('pages.workspace.pm.channel.enable')" />
            </label>

            <div class="mt-3">
              <span class="label">{{ t('pages.workspace.pm.channel.typeLabel') }}</span>
              <div class="rounded-md overflow-hidden mt-1 flex border border-line" role="group" data-testid="channel-type-seg">
                <button
                  type="button"
                  class="flex-1 px-3 py-2 text-[13px]"
                  :class="chType === 'qq' ? 'bg-accent-dim text-txt' : 'bg-base text-txt2'"
                  :disabled="!isNew"
                  data-testid="channel-type-qq"
                  @click="isNew && setChannelType('qq')"
                >
                  {{ t('pages.workspace.pm.channel.typeQQ') }}
                </button>
                <button
                  type="button"
                  class="flex-1 border-l border-line px-3 py-2 text-[13px]"
                  :class="chType === 'wecom' ? 'bg-accent-dim text-txt' : 'bg-base text-txt2'"
                  :disabled="!isNew"
                  data-testid="channel-type-wecom"
                  @click="isNew && setChannelType('wecom')"
                >
                  {{ t('pages.workspace.pm.channel.typeWecom') }}
                </button>
                <button
                  type="button"
                  class="flex-1 border-l border-line px-3 py-2 text-[13px]"
                  :class="chType === 'feishu' ? 'bg-accent-dim text-txt' : 'bg-base text-txt2'"
                  :disabled="!isNew"
                  data-testid="channel-type-feishu"
                  @click="isNew && setChannelType('feishu')"
                >
                  {{ t('pages.workspace.pm.channel.typeFeishu') }}
                </button>
                <button
                  type="button"
                  class="flex-1 border-l border-line px-3 py-2 text-[13px]"
                  :class="chType === 'dingtalk' ? 'bg-accent-dim text-txt' : 'bg-base text-txt2'"
                  :disabled="!isNew"
                  data-testid="channel-type-dingtalk"
                  @click="isNew && setChannelType('dingtalk')"
                >
                  {{ t('pages.workspace.pm.channel.typeDingTalk') }}
                </button>
              </div>
              <p v-if="!isNew" class="mt-1 text-[11px] text-txt3">
                {{ t('pages.workspace.pm.channel.typeFrozen') }}
              </p>
              <p class="mt-1 text-[11px] text-txt3">{{ t('pages.workspace.pm.channel.typeHint') }}</p>
            </div>

            <div v-if="isNew" class="mt-3">
              <label class="label" for="ch-multi-role">{{ t('pages.workspace.pm.channel.colRole') }}</label>
              <select
                id="ch-multi-role"
                class="input max-w-md"
                data-testid="channel-role"
                :value="chIsPrimary ? 'primary' : 'secondary'"
                @change="chIsPrimary = ($event.target as HTMLSelectElement).value === 'primary'"
              >
                <option value="secondary">{{ t('pages.workspace.pm.channel.roleSecondary') }}</option>
                <option value="primary" :disabled="hasPrimary">
                  {{ t('pages.workspace.pm.channel.rolePrimary') }}
                </option>
              </select>
            </div>

            <div class="mt-3">
              <label class="label" for="ch-multi-name">{{ t('pages.workspace.pm.channel.name') }}</label>
              <input
                id="ch-multi-name"
                v-model="chName"
                class="input max-w-md"
                :placeholder="t('pages.workspace.pm.channel.namePlaceholder')"
              />
            </div>

            <div class="mt-3">
              <label class="label" for="ch-multi-agent">
                {{ t('pages.workspace.pm.channel.bindAgent') }}
              </label>
              <select id="ch-multi-agent" v-model="chAgent" class="input max-w-md">
                <option value="">{{ t('pages.workspace.pm.selectAgent') }}</option>
                <option v-for="name in agentOptions" :key="name" :value="name">{{ name }}</option>
              </select>
              <p class="mt-1 text-[11px] text-txt3">
                {{ t('pages.workspace.pm.channel.bindAgentHint') }}
              </p>
            </div>

            <div class="mt-3 grid grid-cols-2 gap-3 max-sm:grid-cols-1">
              <div>
                <label class="label" for="ch-multi-appid">
                  {{
                    chType === 'wecom'
                      ? t('pages.workspace.pm.channel.botId')
                      : t('pages.workspace.pm.channel.appId')
                  }}
                </label>
                <input
                  id="ch-multi-appid"
                  v-model="chAppId"
                  class="input w-full"
                  :disabled="!isNew && chType === 'wecom'"
                  data-testid="channel-appid"
                />
              </div>
              <div>
                <label class="label" for="ch-multi-secret">
                  {{
                    chType === 'wecom'
                      ? t('pages.workspace.pm.channel.secret')
                      : t('pages.workspace.pm.channel.appSecret')
                  }}
                </label>
                <input
                  id="ch-multi-secret"
                  v-model="chAppSecret"
                  type="password"
                  class="input w-full"
                  :placeholder="chAppSecretSet ? t('pages.workspace.pm.channel.appSecretKeep') : ''"
                  data-testid="channel-secret"
                />
                <p v-if="chAppSecretSet" class="mt-1 text-[11px] text-txt3">
                  {{ t('pages.workspace.pm.channel.appSecretSet') }}
                </p>
              </div>
            </div>
            <p
              v-if="saveError"
              class="mt-2 text-[13px] leading-5 text-err"
              data-testid="channel-save-error"
            >
              {{ saveError }}
            </p>

            <div class="mt-3 grid grid-cols-2 gap-3 max-sm:grid-cols-1">
              <div>
                <label class="label" for="ch-multi-timeout">
                  {{ t('pages.workspace.pm.channel.turnTimeout') }}
                </label>
                <input
                  id="ch-multi-timeout"
                  v-model.number="chTurnTimeout"
                  type="number"
                  min="0"
                  class="input w-[140px]"
                />
                <p class="mt-1 text-[11px] text-txt3">
                  {{ t('pages.workspace.pm.channel.turnTimeoutHint') }}
                </p>
              </div>
              <div v-if="chType === 'feishu'">
                <label class="label" for="ch-multi-region">{{ t('pages.workspace.pm.channel.region') }}</label>
                <select id="ch-multi-region" v-model="chRegion" class="input w-full" data-testid="channel-region">
                  <option value="cn">{{ t('pages.workspace.pm.channel.regionCN') }}</option>
                  <option value="lark">{{ t('pages.workspace.pm.channel.regionLark') }}</option>
                </select>
              </div>
              <label
                v-else-if="chType === 'qq'"
                class="flex items-center justify-between gap-3 pt-6 text-[13px] text-txt"
                data-testid="channel-sandbox"
              >
                <span>{{ t('pages.workspace.pm.channel.sandbox') }}</span>
                <AppSwitch v-model="chSandbox" :aria-label="t('pages.workspace.pm.channel.sandbox')" />
              </label>
            </div>

            <div v-if="chType === 'qq'" class="mt-3 grid grid-cols-2 gap-3 max-sm:grid-cols-1" data-testid="channel-qq-only">
              <div>
                <label class="label" for="ch-multi-intents">{{ t('pages.workspace.pm.channel.intents') }}</label>
                <input id="ch-multi-intents" v-model="chIntents" class="input w-full" />
              </div>
              <label class="flex items-center justify-between gap-3 pt-6 text-[13px] text-txt">
                <span>{{ t('pages.workspace.pm.channel.qqMarkdown') }}</span>
                <AppSwitch v-model="chMarkdown" :aria-label="t('pages.workspace.pm.channel.qqMarkdown')" />
              </label>
            </div>
            <p
              v-else-if="chType === 'dingtalk'"
              class="mt-3 text-[11px] leading-snug text-txt3"
              data-testid="channel-dingtalk-hint"
            >
              {{ t('pages.workspace.pm.channel.dingTalkHint') }}
            </p>
            <p v-else class="mt-3 text-[11px] leading-snug text-txt3" data-testid="channel-feishu-hint">
              {{ t('pages.workspace.pm.channel.longConnHint') }}
            </p>

            <div class="mt-5 border-t border-line/60 pt-4">
              <label class="flex cursor-pointer items-center justify-between gap-3 text-[13px] text-txt">
                <span>{{ t('pages.workspace.pm.channel.cronDeliver') }}</span>
                <AppSwitch
                  v-model="chCronDeliver"
                  data-testid="cron-deliver-enable"
                  :aria-label="t('pages.workspace.pm.channel.cronDeliver')"
                />
              </label>
              <p class="mt-1.5 text-[11px] leading-snug text-txt3">
                {{ t('pages.workspace.pm.channel.cronDeliverHintMulti') }}
              </p>
              <div v-if="chCronDeliver" class="mt-3">
                <label class="label" for="ch-multi-cron">
                  {{ t('pages.workspace.pm.channel.cronDeliverTarget') }}
                </label>
                <div ref="targetComboRoot" class="relative">
                  <div class="flex gap-1">
                    <input
                      id="ch-multi-cron"
                      v-model="chCronDeliverTarget"
                      class="input w-full flex-1 font-mono text-[12px]"
                      placeholder="guild:123"
                      autocomplete="off"
                      role="combobox"
                      data-testid="cron-deliver-target-input"
                      @focus="setTargetComboOpen(true)"
                      @keydown="onTargetInputKeydown"
                    />
                    <button
                      type="button"
                      class="chip shrink-0 px-2 hover:border-accent/50"
                      @click="setTargetComboOpen(!targetComboOpen)"
                    >
                      <Icon name="chevron-down" :size="14" />
                    </button>
                  </div>
                  <div
                    v-if="targetComboOpen"
                    :id="TARGET_LISTBOX_ID"
                    class="card scroll-area absolute left-0 right-0 z-20 mt-1 max-h-64 overflow-y-auto"
                    role="listbox"
                  >
                    <template v-if="recentTargets.length">
                      <button
                        v-for="(opt, idx) in recentTargets"
                        :key="opt.value"
                        type="button"
                        class="block w-full px-3 py-2 text-left hover:bg-base"
                        :class="idx === targetActiveIndex ? 'bg-base' : ''"
                        @click="selectPushTarget(opt.value)"
                      >
                        <span class="block truncate text-[12px] text-txt">
                          {{ pushTargetPrimaryLabel(opt) }}
                        </span>
                        <span class="mt-0.5 block font-mono text-[11px] text-txt3">
                          {{ opt.value }}
                          <template v-if="opt.unspoken"> · {{ t('pages.workspace.pm.unspoken') }}</template>
                        </span>
                      </button>
                    </template>
                    <p v-else class="px-3 py-2 text-[11px] text-txt3">
                      {{ t('pages.workspace.pm.channel.recentTargetsEmpty') }}
                    </p>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div class="space-y-7 border-t border-line/60 pt-5">
            <div class="min-w-0">
              <h3 class="m-0 text-[14px] font-semibold leading-5 text-txt">
                {{ t('pages.workspace.pm.channel.sessionCapsTitle') }}
              </h3>
              <p class="m-0 mb-3 mt-1 text-xs text-txt2">
                {{ t('pages.workspace.pm.channel.sessionCapsHint') }}
              </p>
              <SettingsRow
                :label="t('pages.workspace.pm.channel.allowMemoryWrite')"
                :description="t('pages.workspace.pm.channel.allowMemoryWriteHint')"
                inline
              >
                <AppSwitch v-model="chAllowMemoryWrite" :aria-label="t('pages.workspace.pm.channel.allowMemoryWrite')" />
              </SettingsRow>
              <SettingsRow
                :label="t('pages.workspace.pm.channel.allowSchedulerWrite')"
                :description="t('pages.workspace.pm.channel.allowSchedulerWriteHint')"
                class="border-t border-line/60"
                inline
              >
                <AppSwitch v-model="chAllowSchedulerWrite" :aria-label="t('pages.workspace.pm.channel.allowSchedulerWrite')" />
              </SettingsRow>
            </div>

            <div class="min-w-0">
              <h3 class="m-0 text-[14px] font-semibold leading-5 text-txt">
                {{ t('pages.workspace.pm.channel.channelMcpsTitle') }}
              </h3>
              <p class="m-0 mb-2 mt-1 text-xs text-txt2">
                {{ t('pages.workspace.pm.channel.channelMcpsHint') }}
              </p>
              <div class="mb-3 text-[13px] leading-5 text-txt3">
                {{ t('pages.workspace.pm.channel.channelMcpsNote') }}
              </div>
              <div class="divide-y divide-line/60">
                <SettingsRow
                  v-for="opt in PM_MCP_OPTIONS"
                  :key="opt.id"
                  :label="t(opt.labelKey)"
                  inline
                >
                  <template #meta>
                    <code class="font-mono text-[11px]">{{ opt.id }}</code>
                  </template>
                  <AppSwitch
                    :model-value="chEnabledMcps.includes(opt.id)"
                    :aria-label="opt.id"
                    @update:model-value="toggleChMcp(opt.id)"
                  />
                </SettingsRow>
              </div>
            </div>
          </div>
        </div>

        <p
          v-if="secretsKeyConfigured === false"
          class="mt-3 text-[11px] text-err"
        >
          {{ t('pages.workspace.pm.channel.secretKeyHint') }}
        </p>

        <div
          v-if="formConnectionHint()"
          class="mt-3 text-[13px] leading-5"
          :class="
            formConnectionHint()!.kind === 'ok'
              ? 'text-ok'
              : formConnectionHint()!.kind === 'warn'
                ? 'text-warn'
                : 'text-err'
          "
          data-testid="channel-conn-hint"
        >
          {{ formConnectionHint()!.text }}
        </div>

        <div class="mt-5 flex flex-wrap justify-end gap-2">
          <AppButton variant="primary" :disabled="saving" data-testid="channel-save" @click="saveChannel">
            {{ saving ? t('common.buttons.saving') : t('pages.workspace.pm.channel.saveAndConnect') }}
          </AppButton>
          <AppButton variant="ghost" :disabled="saving" @click="cancelEdit">
            {{ t('common.buttons.cancel') }}
          </AppButton>
          <AppButton
            v-if="editingId && !isNew"
            variant="ghost"
            class="text-err"
            :disabled="saving"
            @click="editingChannel && askDelete(editingChannel)"
          >
            {{ t('pages.workspace.pm.channel.delete') }}
          </AppButton>
        </div>
      </template>
    </div>

    <!-- Notify targets -->
    <div v-else data-testid="channel-panel-notify">
      <div class="mb-3 flex items-center justify-between gap-2">
        <div>
          <strong class="block text-sm text-txt">
            {{ t('pages.workspace.pm.channel.notifyTitle') }}
          </strong>
          <p class="m-0 mt-1 text-xs text-txt2">
            {{ t('pages.workspace.pm.channel.notifyHint') }}
          </p>
        </div>
        <AppButton
          variant="primary"
          :disabled="notifySaving"
          data-testid="channel-notify-save"
          @click="saveNotifyTargets"
        >
          {{
            notifySaving
              ? t('common.buttons.saving')
              : t('pages.workspace.pm.channel.notifySave')
          }}
        </AppButton>
      </div>

      <div class="mb-3 flex items-center justify-between gap-3 border-b border-line/60 py-4">
        <div>
          <div class="text-[13px] font-medium text-txt">
            {{ t('pages.workspace.pm.channel.notifyPolicyTitle') }}
          </div>
          <div class="mt-0.5 text-[11px] text-txt3">
            {{ t('pages.workspace.pm.channel.notifyPolicyHint') }}
          </div>
        </div>
        <span class="rounded-md border border-line bg-elevated px-2 py-0.5 text-[12px] text-txt2">
          {{ t('pages.workspace.pm.channel.notifySelected', { n: notifySelected.length }) }}
        </span>
      </div>

      <div v-if="!channelList.length" class="py-4 text-[13px] text-txt3">
        {{ t('pages.workspace.pm.channel.listEmpty') }}
      </div>
      <div v-else class="border-y border-line/60">
        <label
          v-for="ch in channelList"
          :key="ch.id"
          class="flex cursor-pointer items-center gap-3 border-b border-line/60 py-4 last:border-b-0"
        >
          <input
            type="checkbox"
            class="accent-[rgb(var(--c-accent))]"
            :checked="notifySelected.includes(ch.id)"
            @change="toggleNotify(ch.id)"
          />
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2 text-[13px] text-txt">
              <span>{{ ch.name || ch.appId }}</span>
              <span
                v-if="ch.isPrimary"
                class="rounded-md border border-accent/55 bg-accent-dim px-2 py-0.5 text-[11px] text-accent-2"
              >
                {{ t('pages.workspace.pm.channel.rolePrimary') }}
              </span>
              <span
                v-if="!ch.enabled"
                class="rounded-md border border-line px-2 py-0.5 text-[11px] text-txt3"
              >
                {{ t('pages.workspace.pm.channel.statusOff') }}
              </span>
            </div>
            <div class="mt-0.5 font-mono text-[11px] text-txt3">
              {{ ch.agentName || '—' }} · {{ ch.cronDeliverTarget || t('pages.workspace.pm.channel.noPushTarget') }}
            </div>
          </div>
        </label>
      </div>

      <div
        v-if="!notifySelected.length"
        class="mt-3 text-[13px] leading-5 text-txt3"
        data-testid="notify-empty-hint"
      >
        {{ t('pages.workspace.pm.channel.notifyEmpty') }}
      </div>

      <div class="mt-7" data-testid="channel-deliver-log">
        <div class="border-b border-line/60 pb-3 text-[14px] font-medium text-txt">
          {{ t('pages.workspace.pm.channel.deliverLogTitle') }}
        </div>
        <div v-if="!notifyReceipts.length" class="py-4 text-[13px] text-txt3">
          {{ t('pages.workspace.pm.channel.deliverLogEmpty') }}
        </div>
        <div
          v-for="(rec, idx) in notifyReceipts"
          :key="idx"
          class="flex items-start gap-2 border-b border-line/60 py-3 last:border-b-0"
        >
          <span
            class="rounded-md shrink-0 border px-2 py-0.5 text-[11px]"
            :class="rec.status === 'ok' ? 'border-ok/40 text-ok' : 'border-err/45 text-err'"
          >
            {{ rec.status === 'ok' ? t('pages.workspace.pm.channel.deliverOk') : t('pages.workspace.pm.channel.deliverFail') }}
          </span>
          <div class="min-w-0">
            <div class="font-mono text-[12px] text-txt">{{ rec.kind }} · {{ rec.runId }}</div>
            <div v-if="rec.error" class="mt-0.5 text-[11px] text-err">{{ rec.error }}</div>
          </div>
        </div>
      </div>

      <div class="mt-3 text-[13px] leading-5 text-txt3">
        {{ t('pages.workspace.pm.channel.notifyFanoutHint') }}
      </div>
    </div>

    <!-- Delete primary modal -->
    <div
      v-if="deleteOpen && deleteTarget"
      class="fixed inset-0 z-40 flex items-center justify-center bg-black/55 p-6"
      data-testid="channel-delete-primary-modal"
    >
      <div class="rounded-xl w-full max-w-md border border-line-strong bg-surface p-4 shadow-[var(--shadow-card)]">
        <h3 class="m-0 text-sm font-semibold text-txt">
          {{ t('pages.workspace.pm.channel.deletePrimaryTitle') }}
        </h3>
        <p class="mb-3.5 mt-2 text-xs text-txt2">
          {{ t('pages.workspace.pm.channel.deletePrimaryHint') }}
        </p>
        <div class="mb-3.5 grid gap-2">
          <button
            type="button"
            class="rounded-lg border px-3 py-2.5 text-left text-xs"
            :class="
              deleteMode === 'promote'
                ? 'border-accent bg-accent-dim text-txt'
                : 'border-line bg-base text-txt2'
            "
            @click="deleteMode = 'promote'"
          >
            {{ t('pages.workspace.pm.channel.deletePrimaryPromote') }}
          </button>
          <button
            type="button"
            class="rounded-lg border px-3 py-2.5 text-left text-xs"
            :class="
              deleteMode === 'none'
                ? 'border-accent bg-accent-dim text-txt'
                : 'border-line bg-base text-txt2'
            "
            @click="deleteMode = 'none'"
          >
            {{ t('pages.workspace.pm.channel.deletePrimaryNone') }}
          </button>
        </div>
        <div v-if="deleteMode === 'promote'" class="mb-3">
          <label class="label">{{ t('pages.workspace.pm.channel.newPrimary') }}</label>
          <select v-model="deleteNewPrimaryId" class="input">
            <option
              v-for="ch in channelList.filter((c) => c.id !== deleteTarget?.id)"
              :key="ch.id"
              :value="ch.id"
            >
              {{ ch.name || ch.appId }} ({{ ch.agentName || '—' }})
            </option>
          </select>
          <label class="mt-2 flex items-center gap-2 text-[12px] text-txt2">
            <input v-model="deleteSyncPmLeader" type="checkbox" class="accent-[rgb(var(--c-accent))]" />
            {{ t('pages.workspace.pm.channel.syncPmLeaderOnPromote') }}
          </label>
        </div>
        <div class="flex justify-end gap-2">
          <AppButton variant="ghost" @click="deleteOpen = false">
            {{ t('common.buttons.cancel') }}
          </AppButton>
          <AppButton variant="primary" :disabled="saving" @click="confirmDeletePrimary">
            {{ t('pages.workspace.pm.channel.confirmDelete') }}
          </AppButton>
        </div>
      </div>
    </div>
  </div>
</template>
