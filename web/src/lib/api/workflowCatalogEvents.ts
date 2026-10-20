import { ref } from 'vue'

export const workflowCatalogRevision = ref(0)
export function invalidateWorkflowCatalog() { workflowCatalogRevision.value++ }
