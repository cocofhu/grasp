<script setup lang="ts">
import { useGateApprovalCtx } from './gateApprovalContext'
import Icon from '../../ui/Icon.vue'

const { s } = useGateApprovalCtx()
</script>

<template>
  <div class="shrink-0 border-b border-line px-4 py-3">
    <div class="flex items-center gap-2">
      <Icon name="user-hold" :size="16" class="text-warn" />
      <span class="text-sm font-semibold text-txt">{{ s.gate.title }}</span>
    </div>
    <div class="mt-1 text-[11px] text-txt3">{{ s.t('pages.gateApproval.subtitle') }}</div>
  </div>
</template>
