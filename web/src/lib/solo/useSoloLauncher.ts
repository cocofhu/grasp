import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api/api'
import type { Agent, Project, SoloSession } from '@/lib/api/apiTypes'
import type { ClarifyImage } from '@/lib/shared/types'
import { soloPrefs } from './soloPrefs'
import { isSoloLimitError, soloErrorText } from './soloErrors'
import { soloCreateChoices } from './sessionRepos'
import { useSoloSessions } from './useSoloSessions'

/**
 * Agent / repository selection and session creation shared by the home
 * composer and the /solo "new session" dialog. Repository ids persist across
 * visits; branch overrides apply only to the current launch.
 */
export function useSoloLauncher() {
  const { t, te, locale } = useI18n()
  const router = useRouter()
  const store = useSoloSessions()

  const agents = ref<Agent[]>([])
  const projects = ref<Project[]>([])
  const agentName = ref(soloPrefs.lastAgent())
  const projectIds = ref<string[]>(soloPrefs.lastProjects())
  const branches = ref<Record<string, string>>({})
  const loading = ref(false)
  const creating = ref(false)
  const error = ref('')
  const limitOpen = ref(false)

  const agent = computed(() => agents.value.find((a) => a.name === agentName.value) ?? null)
  const canStart = computed(() => !!agent.value && !creating.value)

  watch(agentName, (v) => soloPrefs.setLastAgent(v))
  watch(projectIds, (ids) => {
    soloPrefs.setLastProjects(ids)
    const keep = new Set(ids)
    const next: Record<string, string> = {}
    for (const [id, branch] of Object.entries(branches.value)) {
      if (keep.has(id) && branch) next[id] = branch
    }
    branches.value = next
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
      projectIds.value = projectIds.value.filter((id) => p.some((x) => x.id === id))
    } finally {
      loading.value = false
    }
  }

  function toggleProject(id: string) {
    const current = projectIds.value
    projectIds.value = current.includes(id) ? current.filter((x) => x !== id) : [...current, id]
  }

  function clearProjects() {
    projectIds.value = []
    branches.value = {}
  }

  function selectOnly(id: string) {
    projectIds.value = id ? [id] : []
    branches.value = {}
  }

  function setProjectBranch(id: string, branch: string) {
    branches.value = { ...branches.value, [id]: branch }
  }

  function onProjectSaved(p: Project) {
    const i = projects.value.findIndex((x) => x.id === p.id)
    if (i >= 0) projects.value.splice(i, 1, p)
    else projects.value.unshift(p)
    if (!projectIds.value.includes(p.id)) projectIds.value = [...projectIds.value, p.id]
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
        repos: soloCreateChoices(projectIds.value, branches.value),
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
        error.value = soloErrorText(e, t, te, locale.value)
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
    projectIds,
    branches,
    agent,
    loading,
    creating,
    error,
    limitOpen,
    canStart,
    load,
    start,
    toggleProject,
    clearProjects,
    selectOnly,
    setProjectBranch,
    onProjectSaved,
  }
}
