<script setup lang="ts">
import { ref } from 'vue'
import Icon from '../ui/Icon.vue'

interface BootChecklistStep {
  id: string
  label: string
  state: 'done' | 'running' | 'pending'
  /** Formatted duration, shown only on done steps. */
  sec?: string
}

// The sandbox boot row in the chat's folded tool-row look (使用了 N 个工具),
// shared by workflow nodes (ClarifyBootLoader) and Solo sessions
// (SoloStartupCard). Callers own the step states; nothing here advances on
// its own.
withDefaults(defineProps<{
  title: string
  steps: BootChecklistStep[]
  /** What the backend is doing now. */
  context?: string
  elapsed?: string
  hint?: string
  testidPrefix?: string
}>(), { context: '', elapsed: '', hint: '', testidPrefix: 'clarify-boot' })

const open = ref(true)
</script>

<template>
  <div class="agent-tools chat-meta w-full min-w-0">
    <!-- Live region for the step in progress; the toggle keeps plain button semantics. -->
    <span class="sr-only" role="status" aria-live="polite" :data-testid="`${testidPrefix}-status`">{{ context }}</span>
    <button
      type="button"
      class="agent-tools-head grasp-status-host"
      :aria-expanded="open"
      :data-testid="`${testidPrefix}-head`"
      @click="open = !open"
    >
      <Icon name="arc-spinner" busy :size="16" class="agent-tool-icon agent-tool-running" aria-hidden="true" />
      <span class="agent-tools-count" :data-testid="`${testidPrefix}-title`">{{ title }}</span>
      <span v-if="elapsed" class="tabular-nums text-txt3" :data-testid="`${testidPrefix}-elapsed`">{{ elapsed }}</span>
      <span class="agent-tools-context text-txt3" :data-testid="`${testidPrefix}-context`">{{ context }}</span>
      <Icon :interactive="false" name="chevron-down" :size="12" class="agent-tool-chevron" :class="{ 'rotate-180': open }" aria-hidden="true" />
    </button>
    <ul v-if="open" class="agent-tool-list" :data-testid="`${testidPrefix}-checklist`">
      <li
        v-for="s in steps"
        :key="s.id"
        class="agent-tool-row-main"
        :data-state="s.state"
        :data-testid="`${testidPrefix}-step`"
      >
        <!-- type=transition: the spinner's infinite spin must not hold the swap to ✓. -->
        <Transition name="startfade" mode="out-in" type="transition">
          <Icon v-if="s.state === 'running'" key="r" name="arc-spinner" busy :size="16" class="agent-tool-icon agent-tool-running" aria-hidden="true" />
          <Icon v-else-if="s.state === 'done'" key="d" name="check-draw" :size="16" class="agent-tool-icon text-ok" aria-hidden="true" />
          <Icon v-else key="p" name="dot" :size="16" class="agent-tool-icon text-txt3/60" aria-hidden="true" />
        </Transition>
        <span class="agent-tool-label" :class="s.state === 'pending' ? 'text-txt3' : ''">{{ s.label }}</span>
        <span v-if="s.sec" class="tabular-nums text-[11px] text-txt3">{{ s.sec }}</span>
      </li>
    </ul>
    <p v-if="hint" class="chat-meta m-0 pl-[34px] text-txt3" :data-testid="`${testidPrefix}-hint`">{{ hint }}</p>
  </div>
</template>

<style scoped>
.startfade-enter-active,
.startfade-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}
.startfade-enter-from {
  opacity: 0;
  transform: translateY(4px);
}
.startfade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

@media (prefers-reduced-motion: reduce) {
  .startfade-enter-active,
  .startfade-leave-active {
    transition: none;
  }
}
</style>
<style scoped src="./agentToolGroup.css"></style>
