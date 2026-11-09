package config

import "testing"

func TestExecutionControlNeverDefaultsToPayloadStorageOrCredentials(t *testing.T) {
	cfg := &Config{Storage: StorageConfig{Driver: "s3", BlobsRoot: "payloads", S3: S3StorageConfig{Endpoint: "https://payload.invalid", AccessKeyID: "payload-key", SecretAccessKey: "payload-secret"}}}
	applyExecutionControlEnv(cfg)
	setDefaults(cfg)
	if cfg.ExecutionControl != nil {
		t.Fatal("a missing independent authority inherited payload storage")
	}
	t.Setenv("GRASP_EXECUTION_CONTROL_S3_SECRET_ACCESS_KEY", "independent-secret")
	applyExecutionControlEnv(cfg)
	if cfg.ExecutionControl == nil || cfg.ExecutionControl.Driver != "" || cfg.ExecutionControl.S3.Endpoint != "" || cfg.ExecutionControl.S3.AccessKeyID != "" || cfg.ExecutionControl.BlobsRoot != "" {
		t.Fatal("partially configured authority gained an implicit backend")
	}
	t.Setenv("GRASP_EXECUTION_CONTROL_DRIVER", "local")
	t.Setenv("GRASP_EXECUTION_CONTROL_BLOBS_ROOT", "independent-control")
	t.Setenv("GRASP_EXECUTION_CONTROL_S3_ACCESS_KEY_ID", "independent-key")
	t.Setenv("GRASP_EXECUTION_CONTROL_S3_PATH_STYLE", "false")
	applyExecutionControlEnv(cfg)
	if cfg.ExecutionControl.Driver != "local" || cfg.ExecutionControl.BlobsRoot != "independent-control" || cfg.ExecutionControl.S3.AccessKeyID != "independent-key" || cfg.ExecutionControl.S3.SecretAccessKey != "independent-secret" || cfg.ExecutionControl.S3.PathStyle == nil || *cfg.ExecutionControl.S3.PathStyle {
		t.Fatal("explicit independent control overrides were not applied")
	}
	if cfg.Storage.S3.AccessKeyID != "payload-key" || cfg.Storage.S3.SecretAccessKey != "payload-secret" {
		t.Fatal("control credentials replaced payload credentials")
	}
}
