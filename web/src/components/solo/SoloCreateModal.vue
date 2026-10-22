<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/ui/AppModal.vue'
import AppButton from '@/components/ui/AppButton.vue'
import SoloAgentSelect from './SoloAgentSelect.vue'
import SoloProjectSelect from './SoloProjectSelect.vue'
import ProjectDrawer from './ProjectDrawer.vue'
import SoloLimitModal from './SoloLimitModal.vue'
import { useSoloLauncher } from '@/lib/solo/useSoloLauncher'
import type { Project } from '@/lib/api/apiTypes'

const props = defineProps<{ open: boolean; presetAgent?: string; presetProject?: string }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const { t } = useI18n()
const launcher = useSoloLauncher()
const { agents, projects, agentName, projectId, branch, project, loading, creating, error, limitOpen } = launcher

const text = ref('')
const title = ref('')
const drawerOpen = ref(false)

watch(
  () => props.open,
  async (open) => {
    if (!open) return
    text.value = ''
    title.value = ''
    error.value = ''
    await launcher.load()
    if (props.presetAgent && agents.value.some((a) => a.name === props.presetAgent)) agentName.value = props.presetAgent
    if (props.presetProject && projects.value.some((p) => p.id === props.presetProject)) projectId.value = props.presetProject
  },
  { immediate: true },
)

async function submit() {
  const s = await launcher.start(text.value, [], title.value)
  if (s) emit('close')
}

function onSaved(p: Project) {
  drawerOpen.value = false
  launcher.onProjectSaved(p)
}
</script>

<template>
  <AppModal :open="open" :title="t('pages.solo.create.title')" :width="520" close-on-esc @close="emit('close')">
    <form class="space-y-4" data-testid="solo-create-modal" @submit.prevent="submit">
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <span class="label">{{ t('pages.solo.create.agent') }}</span>
          <SoloAgentSelect v-model="agentName" class="!max-w-none" :agents="agents" :loading="loading" :invalid="!!error && !agentName" />
        </div>
        <div>
          <span class="label">{{ t('pages.solo.create.project') }}</span>
          <SoloProjectSelect v-model="projectId" class="!max-w-none" :projects="projects" :loading="loading" @create="drawerOpen = true" />
        </div>
      </div>
      <label v-if="project" class="block">
        <span class="label">{{ t('pages.solo.create.branch') }}</span>
        <input
          v-model="branch"
          class="input w-full font-mono"
          spellcheck="false"
          :placeholder="project.defaultBranch || t('pages.solo.create.branchPlaceholder')"
          data-testid="solo-create-branch"
        />
      </label>
      <label class="block">
        <span class="label">{{ t('pages.solo.create.titleLabel') }}</span>
        <input v-model="title" class="input w-full" maxlength="80" :placeholder="t('pages.solo.create.titlePlaceholder')" />
      </label>
      <label class="block">
        <span class="label">{{ t('pages.solo.create.firstMessage') }}</span>
        <textarea
          v-model="text"
          rows="4"
          class="input w-full resize-y"
          :placeholder="t('pages.solo.create.firstMessagePlaceholder')"
          data-testid="solo-create-text"
          @keydown.enter.exact.meta.prevent="submit"
          @keydown.enter.exact.ctrl.prevent="submit"
        />
      </label>
      <p class="text-[11.5px] leading-relaxed text-txt3">{{ t('pages.solo.create.residentHint') }}</p>
      <p v-if="error" class="text-[12px] text-err" role="alert">{{ error }}</p>
    </form>
    <template #footer>
      <AppButton variant="ghost" @click="emit('close')">{{ t('common.buttons.cancel') }}</AppButton>
      <AppButton variant="primary" icon="play" :loading="creating" :disabled="!agentName" data-testid="solo-create-submit" @click="submit">
        {{ t('pages.solo.create.submit') }}
      </AppButton>
    </template>
    <ProjectDrawer :open="drawerOpen" @close="drawerOpen = false" @saved="onSaved" />
    <SoloLimitModal :open="limitOpen" @close="limitOpen = false" @freed="limitOpen = false" />
  </AppModal>
</template>
