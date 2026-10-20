<script setup lang="ts">
withDefaults(
  defineProps<{
    /** Hide the bordered input chrome (cold path: footer only). */
    showChrome?: boolean
    showFooter?: boolean
    /** Height-constrained hosts (mobile gate drawer): shorter toolbar and tighter gaps. */
    compact?: boolean
    /** Keep shared actions inside the input and wrap them on narrow hosts. */
    wrapToolbar?: boolean
    boxTestId?: string
    toolbarTestId?: string
    footerTestId?: string
  }>(),
  {
    showChrome: true,
    showFooter: true,
    compact: false,
    wrapToolbar: false,
    boxTestId: 'composer-shell-box',
    toolbarTestId: 'composer-shell-toolbar',
    footerTestId: 'composer-shell-footer',
  },
)
</script>

<template>
  <div data-testid="composer-shell">
    <div
      v-if="showChrome"
      class="chat-composer flex min-w-0 flex-col"
      :data-testid="boxTestId"
    >
      <div class="min-w-0 px-4" :class="compact ? 'pt-1.5' : 'pt-4'">
        <slot name="input" />
      </div>
      <!-- flex-shrink:0 so SVG/tool buttons do not stretch with auto-grow textarea (plan g1.1 / f6) -->
      <div
        class="chat-toolbar flex shrink-0 items-center gap-1.5 px-2"
        :class="wrapToolbar
          ? (compact ? 'min-h-9 flex-wrap py-1' : 'min-h-11 flex-wrap pb-2 pt-1')
          : (compact ? 'h-9 pb-1' : 'h-11 pb-2 pt-1')"
        :data-testid="toolbarTestId"
      >
        <slot name="toolbar-start" />
        <div class="ml-auto flex shrink-0 items-center gap-1.5" :class="wrapToolbar ? 'max-w-full flex-wrap justify-end' : ''">
          <slot name="toolbar-end" />
        </div>
      </div>
    </div>
    <div
      v-if="showFooter"
      class="flex min-w-0 flex-wrap items-center justify-between gap-2"
      :class="compact ? 'mt-1.5' : 'mt-2'"
      :data-testid="footerTestId"
    >
      <!-- Without a hint the empty column would still claim a wrapped flex row. -->
      <div
        v-if="$slots.hint"
        class="min-w-0 flex-1 basis-40 text-[11px] leading-snug text-txt3 [overflow-wrap:anywhere]"
      >
        <slot name="hint" />
      </div>
      <div class="ml-auto flex shrink-0 flex-wrap items-center justify-end gap-2">
        <slot name="footer" />
      </div>
    </div>
  </div>
</template>
