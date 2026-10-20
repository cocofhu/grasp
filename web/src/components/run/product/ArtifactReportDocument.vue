<template>
  <article class="artifact-report-document" data-testid="artifact-report-document"><slot /></article>
</template>

<style scoped>
.artifact-report-document {
  width: 100%;
  min-width: 0;
  max-width: 840px;
  margin-inline: auto;
  color: rgb(var(--c-txt2));
  font-size: 13px;
  line-height: 1.75;
  overflow-wrap: anywhere;
}
.artifact-report-document :deep(.report-title) { color: rgb(var(--c-txt)); font-size: 17px; font-weight: 600; line-height: 1.5; letter-spacing: -.02em; }
.artifact-report-document :deep(.report-summary) { color: rgb(var(--c-txt2)); font-size: 13px; line-height: 1.85; }
.artifact-report-document :deep(.report-summary + .report-summary) { margin-top: 12px; }
.artifact-report-document :deep(.report-section) { margin-top: 24px; border-top: 1px solid rgb(var(--c-line) / .7); padding-top: 18px; }
.artifact-report-document :deep(.report-section:first-child) { margin-top: 0; border-top: 0; padding-top: 0; }
.artifact-report-document :deep(.report-heading) { display: flex; align-items: center; gap: 6px; margin-bottom: 10px; color: rgb(var(--c-txt2)); font-size: 12px; font-weight: 600; line-height: 1.5; letter-spacing: 0; text-transform: none; }
.artifact-report-document :deep(.report-item) { padding: 12px 0; }
.artifact-report-document :deep(.report-item:first-child) { padding-top: 0; }
.artifact-report-document :deep(.report-item + .report-item) { border-top: 1px solid rgb(var(--c-line) / .5); }
.artifact-report-document :deep(.report-note) { margin-top: 10px; border-left: 2px solid rgb(var(--c-line-strong)); padding-left: 12px; font-size: 13px; line-height: 1.75; }
.artifact-report-document :deep(.report-metrics) { display: flex; gap: 0; margin-top: 12px; }
.artifact-report-document :deep(.report-metric) { flex: 1; padding: 2px 16px; }
.artifact-report-document :deep(.report-metric:first-child) { padding-left: 0; }
.artifact-report-document :deep(.report-metric + .report-metric) { border-left: 1px solid rgb(var(--c-line) / .7); }
.artifact-report-document :deep(.report-transcript-entry) { border-left: 2px solid rgb(var(--c-line)); padding: 8px 0 8px 12px; }
.artifact-report-document :deep(.report-code) { overflow-x: auto; margin-top: 12px; border-left: 2px solid rgb(var(--c-line)); padding: 8px 12px; background: rgb(var(--c-txt) / .025); font-size: 12px; }
.artifact-report-document :deep(.report-table-scroll) { overflow-x: auto; }
.artifact-report-document :deep(.report-item h4) { color: rgb(var(--c-txt)); font-size: 14px; font-weight: 500; line-height: 1.6; }
.artifact-report-document :deep(.report-section ul), .artifact-report-document :deep(.report-section ol) { line-height: 1.8; }
</style>
