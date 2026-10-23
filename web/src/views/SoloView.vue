<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import SoloSessionPane from '@/components/solo/SoloSessionPane.vue'
import SoloCreateModal from '@/components/solo/SoloCreateModal.vue'
import { useSoloSessions } from '@/lib/solo/useSoloSessions'
import { soloPrefs } from '@/lib/solo/soloPrefs'

const route = useRoute()
const router = useRouter()
const store = useSoloSessions()

const createOpen = ref(false)
const presetAgent = ref('')
const presetProject = ref('')

const sessionId = computed(() => (typeof route.params.id === 'string' ? route.params.id : ''))

// ?new=1&agent=x&project=y opens the dialog prefilled (Agent Studio, project list).
watch(
  () => route.query.new,
  (v) => {
    if (v !== '1') return
    presetAgent.value = typeof route.query.agent === 'string' ? route.query.agent : ''
    presetProject.value = typeof route.query.project === 'string' ? route.query.project : ''
    createOpen.value = true
    void router.replace({ path: route.path })
  },
  { immediate: true },
)

function goHome() {
  soloPrefs.setHomeMode('solo')
  void router.replace('/dashboard')
}

// Solo starts from Home. /solo without a session redirects to the first
// available conversation or back to Home.
watch(
  () => [store.loaded.value, sessionId.value, createOpen.value, route.query.new] as const,
  ([ok, id, creating, newFlag]) => {
    if (!ok || id || creating || newFlag === '1') return
    const first = store.active.value[0] ?? store.sessions.value[0]
    if (first) {
      void router.replace(`/solo/${first.id}`)
      return
    }
    goHome()
  },
)

onMounted(() => void store.refresh())
</script>

<template>
  <div class="solo-view flex h-full min-h-0" data-testid="solo-view">
    <main class="solo-main min-h-0 min-w-0 flex-1">
      <SoloSessionPane v-if="sessionId" :key="sessionId" :session-id="sessionId" @back="goHome" />
    </main>

    <SoloCreateModal :open="createOpen" :preset-agent="presetAgent" :preset-project="presetProject" @close="createOpen = false" />
  </div>
</template>

<style scoped>
.solo-view {
  min-width: 0;
  overflow: hidden;
  background: rgb(var(--c-base));
}
.solo-main {
  display: flex;
}
</style>
