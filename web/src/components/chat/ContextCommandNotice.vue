<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import { chatCommandIcons, type ContextCommand } from '@/lib/chat/chatCommands'

defineProps<{ command: ContextCommand }>()
const { t } = useI18n()
</script>

<template>
  <div
    class="context-notice"
    :class="command === 'new' ? 'context-notice-new' : 'context-notice-compact'"
    :data-context-command="command"
    data-testid="chat-context-notice"
    role="status"
  >
    <div class="context-notice-heading">
      <span class="context-notice-icon"><Icon :name="chatCommandIcons[command]" :size="15" :interactive="false" /></span>
      <span>{{ t('pages.chatCommands.' + command + '.status') }}</span>
    </div>
    <p>{{ t('pages.chatCommands.' + command + '.detail') }}</p>
  </div>
</template>

<style scoped>
.context-notice {
  min-width: 0;
  color: rgb(var(--c-txt2));
  font-size: 12px;
  line-height: 1.55;
  overflow-wrap: anywhere;
}
.context-notice-heading {
  display: flex;
  align-items: center;
  gap: 7px;
  color: rgb(var(--c-txt));
  font-weight: 600;
}
.context-notice-icon { display: inline-flex; flex: none; color: rgb(var(--c-accent-2)); }
.context-notice-new { padding: 5px 0; text-align: center; }
.context-notice-new .context-notice-heading { gap: 9px; }
.context-notice-new .context-notice-heading::before,
.context-notice-new .context-notice-heading::after {
  flex: 1;
  min-width: 10px;
  height: 1px;
  background: rgb(var(--c-line));
  content: '';
}
.context-notice-new p { margin-top: 4px; }
.context-notice-compact {
  width: fit-content;
  padding: 8px 11px;
  border: 1px solid rgb(var(--c-accent) / 16%);
  border-radius: 10px;
  background: rgb(var(--c-accent) / 6%);
}
.context-notice-compact p { margin-inline-start: 22px; }
</style>
