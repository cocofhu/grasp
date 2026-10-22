import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api/api'
import type { Agent, Project, SoloSession } from '@/lib/api/apiTypes'
import type { ClarifyImage } from '@/lib/shared/types'
import { soloPrefs } from './soloPrefs'
import { isSoloLimitError, soloErrorText } from './soloErrors'
import { useSoloSessions } from './useSoloSessions'

/**
 * Agent / project / branch selection and session creation shared by the home
 * composer and the /solo "new session" dialog. Choices persist across visits.
 */
export function useSoloLauncher() {
  const { t, te } = useI18n()
  const router = useRouter()
  const store = useSoloSessions()

  const agents = ref<Agent[]>([])
  const projects = ref<Project[]>([])
  const agentName = ref(soloPrefs.lastAgent())
  const projectId = ref(soloPrefs.lastProject())
  const branch = ref('')
  const loading = ref(false)
  const creating = ref(false)
  const error = ref('')
  const limitOpen = ref(false)

  const agent = computed(() => agents.value.find((a) => a.name === agentName.value) ?? null)
  const project = computed(() => projects.value.find((p) => p.id === projectId.value) ?? null)
  const canStart = computed(() => !!agent.value && !creating.value)

  watch(agentName, (v) => soloPrefs.setLastAgent(v))
  watch(projectId, (v) => {
    soloPrefs.setLastProject(v)
    branch.value = ''
  })

  async function load() {
    loading.value = true
    try {
      const [a, p] = await Promise.all([
        api.listAgents().catch(() => [] as Agent[]),
        api.listProjects().catch(() => [] as Project[]),
        store.refresh(),
      ])
      agents.value = a
      projects.value = p
      if (agentName.value && !a.some((x) => x.name === agentName.value)) agentName.value = ''
      if (!agentName.value && a.length === 1) agentName.value = a[0]!.name
      if (projectId.value && !p.some((x) => x.id === projectId.value)) projectId.value = ''
    } finally {
      loading.value = false
    }
  }

  function onProjectSaved(p: Project) {
    const i = projects.value.findIndex((x) => x.id === p.id)
    if (i >= 0) projects.value.splice(i, 1, p)
    else projects.value.unshift(p)
    projectId.value = p.id
  }

  /** Creates the session and opens it; the first message runs once the sandbox is up. */
  async function start(text: string, images: ClarifyImage[] = [], title = ''): Promise<SoloSession | null> {
    if (!canStart.value) {
      if (!agent.value) error.value = t('pages.solo.home.noAgent')
      return null
    }
    creating.value = true
    error.value = ''
    try {
      const s = await api.createSoloSession({
        agentName: agentName.value,
        projectId: projectId.value || undefined,
        branch: branch.value.trim() || undefined,
        title: title.trim() || undefined,
        text: text.trim() || undefined,
        images: images.length ? images : undefined,
      })
      store.upsert(s)
      void router.push(`/solo/${s.id}`)
      return s
    } catch (e) {
      if (isSoloLimitError(e)) {
        limitOpen.value = true
        void store.refresh()
      } else {
        error.value = soloErrorText(e, t, te)
      }
      return null
    } finally {
      creating.value = false
    }
  }

  return {
    agents,
    projects,
    agentName,
    projectId,
    branch,
    agent,
    project,
    loading,
    creating,
    error,
    limitOpen,
    canStart,
    load,
    start,
    onProjectSaved,
  }
}
