<script setup lang="ts">
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AgentGitGuide from '@/components/agent/AgentGitGuide.vue'
import EnvCredentialHelpModal from '@/components/agent/EnvCredentialHelpModal.vue'
import WizardApiKeyStepPanel from '@/components/agent/WizardApiKeyStepPanel.vue'
import WizardAcpStartPathPanel from '@/components/agent/WizardAcpStartPathPanel.vue'
import AgentTemplateSelect from '@/components/agent/AgentTemplateSelect.vue'

import { kvToRec } from '@/lib/agent/agentCreateWizard'
import { useAgentCreateWizard } from '@/lib/agent/useAgentCreateWizard'
import type { AgentCreateWizardProps, AgentCreateWizardEmit } from '@/lib/agent/useAgentCreateWizard'

const props = defineProps<AgentCreateWizardProps>()
const emit = defineEmits<AgentCreateWizardEmit>()

const {
  t,
  draft,
  nameError,
  creating,
  createError,
  pendingAcp,
  showAcpConfirm,
  envHelpOpen,
  stepAnimKey,
  apiKeyInput,
  customConfigError,
  openCodeBaseError,
  openCodeModelError,
  currentStep,
  progressPct,
  reviewItems,
  currentRegionPolicy,
  currentRegion,
  authGuide,
  authConfigured,
  showAuthReminder,
  primaryAuthKey,
  primaryAuthAlt,
  headSub,
  templateOptions,
  showDescField,
  templateHint,
  onTemplateSelect,
  close,
  upsertEnv,
  selectRegion,
  markConfigured,
  selectAcp,
  selectStartPath,
  confirmAcpSwitch,
  cancelAcpSwitch,
  syncApiKeyInput,
  setAuthMode,
  onCustomConfigInput,
  onApiKeyInput,
  onOpenCodeProvider,
  onOpenCodeBaseURL,
  onOpenCodeModel,
  onOpenCodeVision,
  onGitCredentialType,
  inheritedEnv,
  goPrev,
  goSkip,
  goNext,
  submitCreate,
  chipClass,
  WIZARD_STEPS,
} = useAgentCreateWizard(props, emit)
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="wiz-root fixed inset-0 z-50 flex items-center justify-center p-4">
      <div class="absolute inset-0 bg-black/70" @click="close" />
      <div
        class="rounded-xl wiz-modal relative z-10 flex w-full flex-col overflow-hidden border border-line bg-surface shadow-card"
        style="width: min(980px, 100%); height: min(700px, 94vh); border-radius: 16px"
        role="dialog"
        aria-modal="true"
      >
        <div class="wiz-head relative flex h-16 shrink-0 items-center gap-3.5 border-b border-line px-5">
          <div class="rounded-md hero-mark grid h-9 w-9 shrink-0 place-items-center border border-accent/55 text-accent-2">
            <Icon name="robot" :size="20" />
          </div>
          <div class="min-w-0 flex-1">
            <h2 class="m-0 text-[16px] font-semibold tracking-tight text-txt">
              {{ t('pages.agentStudio.wizard.title') }}
            </h2>
            <span class="mt-0.5 block text-[12px] font-normal text-txt3">{{ headSub }}</span>
          </div>
          <button
            type="button"
            class="grid h-8 w-8 shrink-0 place-items-center text-txt3 hover:bg-elevated hover:text-txt"
            :aria-label="t('pages.agentStudio.dialogs.cancel')"
            :disabled="creating"
            @click="close"
          >
            <Icon name="close" :size="18" />
          </button>
          <div class="wiz-progress absolute inset-x-0 bottom-0 h-[3px] overflow-hidden bg-elevated">
            <span :style="{ width: progressPct + '%' }" />
          </div>
        </div>

        <div class="flex min-h-0 flex-1">
          <aside class="wiz-rail scroll-area w-[208px] shrink-0 overflow-y-auto border-r border-line bg-elevated px-3 pb-4 pt-5">
            <div class="mb-4 flex items-center gap-2 px-1.5 text-[10px] font-semibold uppercase tracking-[0.08em] text-txt3">
              <span class="pulse-dot h-1.5 w-1.5 shrink-0 bg-accent" />
              {{ t('pages.agentStudio.wizard.railCap') }}
            </div>
            <div
              v-for="(s, i) in WIZARD_STEPS"
              :key="s.id"
              class="rail-item"
              :class="{ done: i < draft.step, cur: i === draft.step }"
            >
              <div class="track">
                <div class="node" aria-hidden="true"><i /></div>
                <div class="connector" />
              </div>
              <div class="lbl">
                <strong>{{ t(s.labelKey) }}</strong>
              </div>
            </div>
          </aside>

          <div class="flex min-w-0 flex-1 flex-col">
            <div class="scroll-area min-h-0 flex-1 overflow-y-auto px-8 py-7">
              <div :key="stepAnimKey" class="step-pane">
                <div class="sec-head">
                  <div class="sec-bar">
                    <h3>{{ t(currentStep.labelKey) }}</h3>
                  </div>
                </div>

                <template v-if="currentStep.id === 'basics'">
                  <p class="sec-meta">{{ t('pages.agentStudio.wizard.basics.meta') }}</p>
                  <label class="mb-4 block">
                    <span class="mb-1.5 block text-[12px] font-medium text-txt2">
                      {{ t('pages.agentStudio.dialogs.createLabel') }}
                      <span class="text-err">*</span>
                    </span>
                    <input
                      id="wiz-name-input"
                      v-model="draft.name"
                      class="rounded-md w-full border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent"
                      :placeholder="t('pages.agentStudio.dialogs.createPlaceholder')"
                      @input="nameError = ''"
                    />
                    <p v-if="nameError" class="mt-1.5 text-[12px] text-err">{{ nameError }}</p>
                  </label>
                  <!-- plan g1 — pipeline-style template dropdown; name stays independent (g1.4) -->
                  <div class="mb-4 block max-w-[38rem]">
                    <span class="mb-1.5 block text-[12px] font-medium text-txt2">
                      {{ t('pages.agentStudio.wizard.basics.templateLabel') }}
                    </span>
                    <AgentTemplateSelect
                      :options="templateOptions"
                      :model-value="draft.templateId"
                      :disabled="creating"
                      @update:model-value="onTemplateSelect"
                    />
                    <p class="mt-1.5 text-[11px] text-txt3">{{ templateHint }}</p>
                  </div>
                  <label v-if="showDescField" class="block">
                    <span class="mb-1.5 block text-[12px] font-medium text-txt2">
                      {{ t('pages.agentStudio.wizard.basics.descLabel') }}
                    </span>
                    <textarea
                      v-model="draft.description"
                      rows="3"
                      class="rounded-md w-full resize-y border border-line bg-base px-3 py-2 font-mono text-[12px] leading-6 text-txt outline-none focus:border-accent"
                      :placeholder="t('pages.agentStudio.wizard.basics.descPlaceholder')"
                    />
                    <p class="mt-1.5 text-[11px] text-txt3">
                      {{ t('pages.agentStudio.wizard.basics.descHint') }}
                    </p>
                  </label>
                </template>

                <template v-else-if="currentStep.id === 'acp'">
                  <p class="sec-meta">{{ t('pages.agentStudio.wizard.acp.meta') }}</p>
                  <WizardAcpStartPathPanel
                    :start-path="draft.startPath"
                    :acp-backend="draft.acpBackend"
                    :config-root="draft.configRoot"
                    :region-policy="currentRegionPolicy"
                    :current-region="currentRegion"
                    region-as-radios
                    show-region-hints
                    show-config-root
                    test-id-prefix="agent-wizard"
                    @select-start-path="selectStartPath"
                    @select-backend="selectAcp"
                    @select-region="selectRegion"
                  />
                </template>

                <template v-else-if="currentStep.id === 'apiKey'">
                  <WizardApiKeyStepPanel
                    :acp-backend="draft.acpBackend"
                    :config-root="draft.configRoot"
                    :auth-mode="draft.authMode"
                    :api-key-input="apiKeyInput"
                    :custom-config-content="draft.customConfigContent"
                    :custom-config-error="customConfigError"
                    :auth-guide="authGuide"
                    :primary-auth-key="primaryAuthKey"
                    :primary-auth-alt="primaryAuthAlt"
                    :env="kvToRec(draft.env)"
                    :open-code-base-error="openCodeBaseError"
                    :open-code-model-error="openCodeModelError"
                    @update:auth-mode="setAuthMode"
                    @update:api-key-input="onApiKeyInput"
                    @update:custom-config-content="onCustomConfigInput"
                    @update:open-code-provider="onOpenCodeProvider"
                    @update:open-code-base-url="onOpenCodeBaseURL"
                    @update:open-code-model="onOpenCodeModel"
                    @update:open-code-vision="onOpenCodeVision"
                  />
                </template>

                <template v-else-if="currentStep.id === 'git'">
                  <p class="sec-meta">{{ t('pages.agentStudio.wizard.git.meta') }}</p>
                  <AgentGitGuide
                    :env="draft.env"
                    :inherited-env="inheritedEnv"
                    :allow-token-recommend="false"
                    :upsert-env="
                      (k, v) => {
                        upsertEnv(k, v)
                        markConfigured('git')
                      }
                    "
                    :credential-type="draft.gitCredentialType"
                    @update:credential-type="onGitCredentialType"
                    @help="envHelpOpen = true"
                  />
                </template>

                <template v-else-if="currentStep.id === 'review'">
                  <p class="sec-meta">{{ t('pages.agentStudio.wizard.review.meta') }}</p>
                  <div class="flex flex-wrap gap-2">
                    <span
                      v-for="item in reviewItems"
                      :key="item.key"
                      class="rounded-md inline-flex items-center gap-1 border px-2 py-1 text-[11px]"
                      :class="chipClass(item.kind)"
                    >
                      {{ t(item.labelKey) }}
                      <template v-if="item.detail"> · {{ item.detail }}</template>
                    </span>
                  </div>
                  <div
                    v-if="showAuthReminder"
                    class="rounded-lg mt-4 border border-warn/35 bg-warn/10 px-3 py-2.5 text-[12px] leading-5 text-txt2"
                    role="status"
                  >
                    {{ t('pages.agentStudio.wizard.review.authReminderDetail') }}
                  </div>
                  <p v-if="createError" class="mt-4 text-[12px] text-err">{{ createError }}</p>
                </template>
              </div>
            </div>

            <div class="flex shrink-0 items-center justify-between gap-2 border-t border-line bg-surface px-5 py-3.5">
              <AppButton variant="ghost" :disabled="creating" @click="close">
                {{ t('pages.agentStudio.dialogs.cancel') }}
              </AppButton>
              <div class="ml-auto flex items-center gap-2">
                <AppButton variant="outline" :disabled="draft.step === 0 || creating" @click="goPrev">
                  {{ t('pages.agentStudio.wizard.prev') }}
                </AppButton>
                <AppButton v-if="currentStep.skip" variant="outline" :disabled="creating" @click="goSkip">
                  {{ t('pages.agentStudio.wizard.skip') }}
                </AppButton>
                <AppButton variant="primary" :disabled="creating" @click="goNext">
                  {{
                    creating
                      ? t('pages.agentStudio.wizard.creating')
                      : currentStep.id === 'review'
                        ? t('pages.agentStudio.wizard.create')
                        : t('pages.agentStudio.wizard.next')
                  }}
                </AppButton>
              </div>
            </div>
          </div>
        </div>

        <div v-if="creating" class="absolute inset-0 z-20 grid place-items-center bg-black/50">
          <div class="rounded-lg border border-line bg-surface px-6 py-4 text-[13px] text-txt2">
            {{ t('pages.agentStudio.wizard.creating') }}…
          </div>
        </div>
      </div>

      <EnvCredentialHelpModal
        :open="open && envHelpOpen"
        section="git"
        :backend="draft.acpBackend"
        elevated
        @close="envHelpOpen = false"
      />

      <div v-if="showAcpConfirm" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
        <div class="absolute inset-0 bg-black/60" @click="cancelAcpSwitch" />
        <div
          class="rounded-xl relative z-10 w-full max-w-[420px] border border-line bg-surface p-5 shadow-card"
          style="border-radius: 16px"
        >
          <h3 class="m-0 text-[15px] font-semibold text-txt">
            {{ t('pages.agentStudio.wizard.acp.remapTitle') }}
          </h3>
          <p class="mt-2 text-[13px] leading-6 text-txt2">
            {{ t('pages.agentStudio.wizard.acp.remapBody') }}
          </p>
          <div class="mt-4 flex justify-end gap-2">
            <AppButton variant="outline" @click="cancelAcpSwitch">
              {{ t('pages.agentStudio.dialogs.cancel') }}
            </AppButton>
            <AppButton variant="primary" @click="confirmAcpSwitch">
              {{ t('pages.agentStudio.dialogs.confirm') }}
            </AppButton>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.wiz-head {
  background: linear-gradient(90deg, rgba(123, 97, 255, 0.12) 0%, transparent 42%), rgb(var(--c-surface));
}
.hero-mark {
  background: linear-gradient(145deg, rgba(123, 97, 255, 0.22), rgb(var(--c-elevated)));
  border-radius: 12px;
}
.wiz-progress span {
  display: block;
  height: 100%;
  position: relative;
  background: linear-gradient(90deg, #6d4dff 0%, #7b61ff 40%, #a78bfa 78%, #818cf8 100%);
  box-shadow: 0 0 14px rgba(123, 97, 255, 0.55);
  transition: width 0.55s cubic-bezier(0.4, 0, 0.2, 1);
}
.wiz-progress span::before {
  content: '';
  position: absolute;
  inset: -2px 0;
  background: inherit;
  filter: blur(6px);
  opacity: 0.55;
  animation: progPulse 1.8s ease-in-out infinite;
}
.wiz-progress span::after {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: 0;
  width: 42%;
  background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.42), transparent);
  animation: progShine 1.7s ease-in-out infinite;
}
@keyframes progPulse {
  0%,
  100% {
    opacity: 0.35;
  }
  50% {
    opacity: 0.75;
  }
}
@keyframes progShine {
  0% {
    transform: translateX(-130%);
    opacity: 0;
  }
  35% {
    opacity: 1;
  }
  100% {
    transform: translateX(260%);
    opacity: 0;
  }
}
.pulse-dot {
  animation: railCapPulse 1.6s ease-in-out infinite;
}
@keyframes railCapPulse {
  0%,
  100% {
    box-shadow: 0 0 0 0 rgba(123, 97, 255, 0.5);
    opacity: 1;
  }
  50% {
    box-shadow: 0 0 0 5px rgba(123, 97, 255, 0);
    opacity: 0.72;
  }
}
.rail-item {
  position: relative;
  display: flex;
  align-items: stretch;
  gap: 10px;
  width: 100%;
  min-height: 40px;
  padding: 0 2px;
  color: rgb(var(--c-txt3));
  font-size: 13px;
}
.rail-item .track {
  position: relative;
  width: 18px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.rail-item .node {
  position: relative;
  z-index: 1;
  width: 14px;
  height: 14px;
  flex-shrink: 0;
  margin-top: 2px;
  border: 1.5px solid rgb(var(--c-line-strong));
  border-radius: 9999px;
  background: transparent;
  display: grid;
  place-items: center;
  transition:
    border-color 0.3s ease,
    background 0.3s ease,
    box-shadow 0.3s ease;
}
.rail-item .node i {
  display: block;
  width: 6px;
  height: 6px;
  background: transparent;
}
.rail-item .connector {
  width: 1px;
  flex: 1;
  min-height: 14px;
  margin: 4px 0 0;
  background: rgb(var(--c-line));
  transition: background 0.45s ease;
}
.rail-item:last-child .connector {
  display: none;
}
.rail-item .lbl {
  flex: 1;
  min-width: 0;
  padding: 0 0 12px;
  display: flex;
  align-items: center;
}
.rail-item .lbl strong {
  font-size: 13px;
  font-weight: 500;
  line-height: 1.3;
  letter-spacing: -0.01em;
  transition: color 0.25s ease;
}
.rail-item.done {
  color: rgb(var(--c-txt2));
}
.rail-item.done .node {
  border-color: rgba(52, 211, 153, 0.55);
  animation: railDonePop 0.4s cubic-bezier(0.22, 1, 0.36, 1);
}
.rail-item.done .node i {
  background: #34d393;
}
.rail-item.done .connector {
  background: rgba(52, 211, 153, 0.35);
}
.rail-item.done .lbl strong {
  color: rgb(var(--c-txt2));
}
.rail-item.cur {
  color: rgb(var(--c-txt));
}
.rail-item.cur .node {
  border-color: #7b61ff;
  background: rgba(123, 97, 255, 0.18);
  animation: railNodePulse 2s ease-in-out infinite;
}
.rail-item.cur .node i {
  background: rgb(var(--c-accent-2));
}
.rail-item.cur .lbl strong {
  color: rgb(var(--c-txt));
  font-weight: 600;
}
@keyframes railNodePulse {
  0%,
  100% {
    box-shadow: 0 0 0 0 rgba(123, 97, 255, 0.45);
  }
  55% {
    box-shadow: 0 0 0 7px rgba(123, 97, 255, 0);
  }
}
@keyframes railDonePop {
  0% {
    transform: scale(0.72);
    opacity: 0.55;
  }
  70% {
    transform: scale(1.08);
  }
  100% {
    transform: scale(1);
    opacity: 1;
  }
}
.sec-head {
  display: block;
  margin: 0 0 8px;
}
.sec-bar {
  display: block;
  padding: 0;
  border: 0;
  background: none;
  min-height: 0;
  position: static;
}
.sec-bar h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  line-height: 1.3;
  letter-spacing: -0.01em;
  color: rgb(var(--c-txt));
}
.sec-meta {
  margin: 0 0 20px;
  font-size: 13px;
  color: rgb(var(--c-txt2));
  line-height: 1.65;
  max-width: 38rem;
}
.step-pane {
  animation: stepIn 0.38s cubic-bezier(0.22, 1, 0.36, 1);
}
@keyframes stepIn {
  from {
    opacity: 0;
    transform: translateY(10px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

html.light .wiz-head {
  background: linear-gradient(90deg, rgba(99, 102, 241, 0.08) 0%, transparent 42%), rgb(var(--c-surface));
}
html.light .hero-mark {
  color: #6366f1;
  background: linear-gradient(145deg, rgba(99, 102, 241, 0.14), rgb(var(--c-elevated)));
  border-color: rgba(99, 102, 241, 0.45);
}
html.light .wiz-progress span {
  box-shadow: 0 0 10px rgba(99, 102, 241, 0.28);
}
html.light .wiz-progress span::before {
  opacity: 0.35;
}
</style>
