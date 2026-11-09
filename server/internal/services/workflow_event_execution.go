package services

import "github.com/cocofhu/grasp/internal/executionlineage"

func workflowExecutionEntities(event workflowEventRecord) []executionlineage.Entity {
	return []executionlineage.Entity{{Kind: executionlineage.EntityWorkflowEvent, ID: event.ID}, {Kind: executionlineage.EntityRun, ID: event.RunID}, {Kind: executionlineage.EntitySolo, ID: event.SoloID}}
}
