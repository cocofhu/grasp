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
import { soloPrefs } from '@/lib/solo/soloPrefs'
import type { Project } from '@/lib/api/apiTypes'

const props = defineProps<{ open: boolean; presetAgent?: string; presetProject?: string }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const { t } = useI18n()
const launcher = useSoloLauncher()
const { agents, projects, agentName, projectIds, branches, loading, creating, error, limitOpen } = launcher

const text = ref('')
const title = ref('')
const drawerOpen = ref(false)
const rememberedProjects = ref<string[]>([])
const launched = ref(false)
const presetLocksMemory = ref(false)

watch(
  () => props.open,
  async (open) => {
    if (!open) return
    text.value = ''
    title.value = ''
    error.value = ''
    launched.value = false
    presetLocksMemory.value = false
    rememberedProjects.value = soloPrefs.lastProjects()
    // A deep link only changes this dialog. Keep the home page's ids until the user actually starts.
    launcher.setPersistProjects(!props.presetProject)
    projectIds.value = rememberedProjects.value.slice()
    await launcher.load()
    if (!props.open) return
    if (props.presetAgent && agents.value.some((a) => a.name === props.presetAgent)) agentName.value = props.presetAgent
    if (props.presetProject && projects.value.some((p) => p.id === props.presetProject)) {
      presetLocksMemory.value = true
      launcher.setPersistProjects(false)
      launcher.selectOnly(props.presetProject)
    } else {
      launcher.setPersistProjects(true)
    }
  },
  { immediate: true },
)

function dismiss() {
  if (!launched.value && presetLocksMemory.value) {
    launcher.setPersistProjects(true)
    soloPrefs.setLastProjects(rememberedProjects.value)
    projectIds.value = rememberedProjects.value.slice()
  }
  emit('close')
}

async function submit() {
  const s = await launcher.start(text.value, [], title.value)
  if (!s) return
  launched.value = true
  emit('close')
}

function onSaved(p: Project) {
  drawerOpen.value = false
  launcher.onProjectSaved(p)
}
</script>

<template>
  <AppModal :open="open" :title="t('pages.solo.create.title')" :width="520" close-on-esc @close="dismiss">
    <form class="space-y-4" data-testid="solo-create-modal" @submit.prevent="submit">
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <span class="label">{{ t('pages.solo.create.agent') }}</span>
          <SoloAgentSelect v-model="agentName" class="!max-w-none" :agents="agents" :loading="loading" :invalid="!!error && !agentName" />
        </div>
        <div>
          <span class="label">{{ t('pages.solo.create.project') }}</span>
          <SoloProjectSelect
            v-model="projectIds"
            class="!max-w-none"
            :projects="projects"
            :branches="branches"
            :loading="loading"
            @update:branch="launcher.setProjectBranch"
            @create="drawerOpen = true"
          />
        </div>
      </div>
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
      <AppButton variant="ghost" @click="dismiss">{{ t('common.buttons.cancel') }}</AppButton>
      <AppButton variant="primary" icon="play" :loading="creating" :disabled="!agentName" data-testid="solo-create-submit" @click="submit">
        {{ t('pages.solo.create.submit') }}
      </AppButton>
    </template>
    <ProjectDrawer :open="drawerOpen" @close="drawerOpen = false" @saved="onSaved" />
    <SoloLimitModal :open="limitOpen" @close="limitOpen = false" @freed="limitOpen = false" />
  </AppModal>
</template>
