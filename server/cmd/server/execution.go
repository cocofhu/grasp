package main

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

func openExecutionController(ctx context.Context, cfg *config.Config, db *gorm.DB) (*executionlineage.Controller, error) {
	if cfg.ExecutionControl == nil {
		return executionlineage.NewController(ctx, db, nil)
	}
	protected := []string{}
	if cfg.Database.Driver != "mysql" {
		protected = append(protected, filepath.Dir(cfg.Database.Path))
	}
	authority, err := executionlineage.OpenConfigured(ctx, *cfg.ExecutionControl, []config.StorageConfig{cfg.Storage}, protected)
	if err != nil {
		closed, cause := executionlineage.NewController(ctx, db, nil)
		return closed, errors.Join(cause, err)
	}
	return executionlineage.NewController(ctx, db, authority)
}

func awaitExecutionGateway(ctx context.Context, cfg *config.Config, gateway *sandbox.GatewayClient) error {
	if cfg.Sandbox.UsesGatewayTransport() {
		return gateway.WaitTransportReady(ctx)
	}
	if cfg.Sandbox.PrivateEndpoints {
		return gateway.WaitReady(ctx, "e2b")
	}
	return nil
}

func checkNativeExecution(ctx context.Context, db *gorm.DB, execution *executionlineage.Controller, req runtime.NodeReq) error {
	if req.RunID == "" {
		return executionlineage.ErrConflict
	}
	var run models.Run
	if err := db.WithContext(ctx).Select("id", "origin_solo_id").First(&run, "id = ?", req.RunID).Error; err != nil {
		return err
	}
	entities := []executionlineage.Entity{{Kind: executionlineage.EntityRun, ID: req.RunID}}
	if run.OriginSoloID != "" {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: run.OriginSoloID})
	}
	if req.StateRunID != 0 {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityStateRun, ID: strconv.FormatUint(uint64(req.StateRunID), 10)})
	}
	return execution.Check(ctx, entities...)
}

func checkMCPExecution(ctx context.Context, db *gorm.DB, execution *executionlineage.Controller, runID string) error {
	var rows []models.Sandbox
	if err := db.WithContext(ctx).Select("id", "purpose", "thread_id").Where("run_id = ?", runID).Find(&rows).Error; err != nil {
		return err
	}
	entities := []executionlineage.Entity{}
	for _, row := range rows {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySandbox, ID: strconv.FormatUint(uint64(row.ID), 10)})
		if row.Purpose == servicesSandboxPurposeSolo {
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: row.ThreadID})
		}
	}
	var run models.Run
	err := db.WithContext(ctx).Select("id", "origin_solo_id").First(&run, "id = ?", runID).Error
	if err == nil {
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityRun, ID: runID})
		if run.OriginSoloID != "" {
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySolo, ID: run.OriginSoloID})
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) || len(rows) == 0 {
		return errors.Join(executionlineage.ErrConflict, err)
	}
	return execution.Check(ctx, entities...)
}

const servicesSandboxPurposeSolo = "solo"
