<script setup lang="ts">
const props = defineProps<{ name: string; size?: number | string }>()

const paths: Record<string, string> = {
  dashboard: '<path d="M3 3h8v8H3zM13 3h8v5h-8zM13 12h8v9h-8zM3 13h8v8H3z"/>',
  chart: '<path d="M4 19V5M4 19h16M8 17V9M12 17V7M16 17v-5"/>',
  board: '<rect x="3" y="4" width="5" height="16"/><rect x="10" y="4" width="5" height="16"/><rect x="17" y="4" width="4" height="16"/>',
  workflow: '<circle cx="6" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="12" r="3"/><path d="M9 6h4a3 3 0 0 1 3 3M9 18h4a3 3 0 0 0 3-3"/>',
  runs: '<circle cx="12" cy="12" r="9"/><path d="M10 9l5 3-5 3z"/>',
  gate: '<path d="M12 3l8 4v5c0 5-3.5 8-8 9-4.5-1-8-4-8-9V7z"/><path d="M9 12l2 2 4-4"/>',
  lock: '<rect x="5" y="11" width="14" height="10"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>',
  key: '<circle cx="8" cy="15" r="4"/><path d="M11.5 12.5 21 3M16 8l3 3M18 5l3 3"/>',
  server: '<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01"/>',
  artifact: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/>',
  skills: '<path d="M4 7h12v5H4z"/><path d="M16 9h2a2 2 0 0 1 0 4h-2"/><path d="M9 4a2 2 0 1 1 4 0v3H9z"/><path d="M4 12v5a2 2 0 0 0 2 2h8"/>',
  connector: '<path d="M9 7V3M15 7V3M8 7h8v4a4 4 0 0 1-8 0z"/><path d="M12 15v6"/>',
  trigger: '<path d="M13 2L4 14h6l-1 8 9-12h-6z"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  play: '<path d="M7 5l12 7-12 7z"/>',
  save: '<path d="M5 3h12l4 4v14H3V5z"/><path d="M8 3v6h8V3M8 21v-7h8v7"/>',
  more: '<circle cx="12" cy="5" r="1.5"/><circle cx="12" cy="12" r="1.5"/><circle cx="12" cy="19" r="1.5"/>',
  close: '<path d="M6 6l12 12M18 6L6 18"/>',
  check: '<path d="M5 12l5 5 9-11"/>',
  alert: '<path d="M12 3l9 16H3z"/><path d="M12 9v5M12 17h.01"/>',
  'chevron-down': '<path d="M6 9l6 6 6-6"/>',
  'chevrons-down': '<path d="M6 5l6 6 6-6"/><path d="M6 11l6 6 6-6"/>',
  'chevron-left': '<path d="M15 6l-6 6 6 6"/>',
  'chevron-right': '<path d="M9 6l6 6-6 6"/>',
  'arrow-left': '<path d="M19 12H5M11 6l-6 6 6 6"/>',
  'arrow-up': '<path d="M12 19V5M5 12l7-7 7 7"/>',
  refresh: '<path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/>',
  copy: '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/>',
  download: '<path d="M12 3v12M7 10l5 5 5-5"/><path d="M4 21h16"/>',
  robot: '<rect x="4" y="8" width="16" height="11" rx="2"/><path d="M12 3v5M8 13h.01M16 13h.01M9 17h6"/>',
  sparkles: '<path d="M12 3l1.6 4.4L18 9l-4.4 1.6L12 15l-1.6-4.4L6 9l4.4-1.6z"/><path d="M19 14l.8 2.2L22 17l-2.2.8L19 20l-.8-2.2L16 17l2.2-.8z"/>',
  user: '<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>',
  branch: '<circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="8" r="2.5"/><path d="M6 8.5v7M6 12h6a6 6 0 0 0 6-1.5"/>',
  ci: '<circle cx="12" cy="12" r="9"/><path d="M8 12l3 3 5-6"/>',
  chat: '<path d="M21 12a8 8 0 0 1-11.3 7.3L4 20l.7-5.7A8 8 0 1 1 21 12z"/>',
  terminal: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9l3 3-3 3M13 15h4"/>',
  bell: '<path d="M6 9a6 6 0 0 1 12 0c0 5 2 6 2 6H4s2-1 2-6z"/><path d="M10 20a2 2 0 0 0 4 0"/>',
  dot: '<circle cx="12" cy="12" r="4"/>',
  edit: '<path d="M4 20h4L19 9l-4-4L4 16z"/><path d="M14 6l4 4"/>',
  'eye-off': '<path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94"/><path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19"/><path d="M14.12 14.12a3 3 0 1 1-4.24-4.24"/><path d="M1 1l22 22"/>',
  doc: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5M9 13h6M9 17h6"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  input: '<path d="M4 12h12M11 7l5 5-5 5"/><path d="M20 4v16"/>',
  output: '<path d="M8 12h12M15 7l5 5-5 5"/><path d="M4 4v16"/>',
  flask: '<path d="M9 3h6M10 3v6l-5 9a2 2 0 0 0 2 3h10a2 2 0 0 0 2-3l-5-9V3"/>',
  send: '<path d="M22 2L11 13M22 2l-7 20-4-9-9-4z"/>',
  // Filled square used only as the composer stop control.
  stop: '<rect x="6" y="6" width="12" height="12" rx="2"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  moon: '<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>',
  expand: '<path d="M9 3H3v6M3 3l7 7M15 21h6v-6M21 21l-7-7"/>',
  flag: '<path d="M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1z"/><path d="M4 22v-7"/>',
  history: '<path d="M3 3v5h5"/><path d="M3.05 13A9 9 0 1 0 6 5.3L3 8"/><path d="M12 7v5l4 2"/>',
  variable: '<path d="M5 4C2 9 2 15 5 20M19 4c3 5 3 11 0 16"/><path d="M9 9l6 6M15 9l-6 6"/>',
  trash: '<path d="M4 7h16M9 7V5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2M6 7l1 13h10l1-13"/>',
  leave: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="M16 17l5-5-5-5"/><path d="M21 12H9"/>',
  git: '<circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="9" r="2.5"/><path d="M18 11.5a6 6 0 0 1-6 6H9M6 8.5v7"/>',
  // Dual-tone loading ring: faint full track + ~90° accent arc (not the old 270° bubble-like arc).
  spinner: '<circle cx="12" cy="12" r="9" opacity="0.25"/><path d="M21 12a9 9 0 0 1-9 9"/>',
  paperclip: '<path d="M21 11l-8.5 8.5a5 5 0 0 1-7-7L14 4a3.5 3.5 0 0 1 5 5l-8.5 8.5a2 2 0 0 1-3-3L15 6"/>',
  monitor: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
  mobile: '<rect x="7" y="3" width="10" height="18" rx="2"/><path d="M11 18h2"/>',
  menu: '<path d="M4 7h16M4 12h16M4 17h16"/>',
  'panel-left': '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18"/><path d="m16 15-3-3 3-3"/>',
  globe: '<circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>',
  crosshair: '<circle cx="12" cy="12" r="7"/><path d="M12 2v4M12 18v4M2 12h4M18 12h4"/>',
  tag: '<path d="M20.6 13.4 12 22l-8.6-8.6a5 5 0 0 1 0-7.1l.7-.7a5 5 0 0 1 7.1 0L12 6l.8-.8a5 5 0 0 1 7.1 0l.7.7a5 5 0 0 1 0 7.1z"/><circle cx="7.5" cy="7.5" r="1.2"/>',
  star: '<path d="m12 3.6 2.5 5.1 5.6.8-4 3.9.9 5.6L12 16.3 7 19l.9-5.6-4-3.9 5.6-.8z"/>',
  'star-filled': '<path d="m12 3.6 2.5 5.1 5.6.8-4 3.9.9 5.6L12 16.3 7 19l.9-5.6-4-3.9 5.6-.8z"/>',
  help: '<circle cx="12" cy="12" r="9"/><path d="M9.5 9a2.5 2.5 0 1 1 3.2 2.4c-.8.3-1.2.8-1.2 1.6V14"/><circle cx="12" cy="17" r="1"/>',
  // Dedicated glyphs for the four workflow-artifact sandbox cards. Shared doc/terminal/globe stay unchanged.
  'stage-ide': '<rect x="3.2" y="3.6" width="17.6" height="16.8" rx="2"/><path d="M3.2 8h17.6"/><path d="M8.2 8v12.4"/><path d="M11 12.2h6"/><path d="M11 15.4h4"/>',
  'stage-terminal': '<rect x="3.2" y="4.2" width="17.6" height="15.6" rx="2"/><path d="M7.1 9.3 10.3 12 7.1 14.7"/><path d="M12.2 14.8h4.6"/>',
  'stage-browser': '<circle cx="12" cy="12" r="8"/><path d="M4 12h16"/><path d="M12 4c2.1 2.1 3.3 4.9 3.3 8s-1.2 5.9-3.3 8c-2.1-2.1-3.3-4.9-3.3-8s1.2-5.9 3.3-8z"/>',
  'stage-log': '<circle cx="6.1" cy="6.6" r="1.15" fill="currentColor" stroke="none"/><circle cx="6.1" cy="12" r="1.15" fill="currentColor" stroke="none"/><circle cx="6.1" cy="17.4" r="1.15" fill="currentColor" stroke="none"/><path d="M9.3 6.6h8.6"/><path d="M9.3 12h6.4"/><path d="M9.3 17.4h8.6"/>',
}

/** Icons that render with solid fill (favorited star). */
const filledNames = new Set(['star-filled', 'stop'])

const size = props.size ?? 18
const body = paths[props.name] ?? paths.dot
const filled = filledNames.has(props.name)
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    :fill="filled ? 'currentColor' : 'none'"
    stroke="currentColor"
    :stroke-width="filled ? 1.2 : 1.7"
    stroke-linecap="round"
    stroke-linejoin="round"
    :class="{ 'icon-spinner': props.name === 'spinner' }"
    :style="props.name === 'spinner' ? { display: 'block', transformOrigin: 'center' } : undefined"
    v-html="body"
  />
</template>
