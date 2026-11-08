package config

import "testing"

func TestObjectStorageEnvironmentAndPresets(t *testing.T) {
	t.Setenv("GRASP_STORAGE_DRIVER", "cos")
	t.Setenv("GRASP_S3_ENDPOINT", "https://cos.test")
	t.Setenv("GRASP_S3_REGION", "ap-guangzhou")
	t.Setenv("GRASP_S3_BUCKET", "bucket-123456")
	t.Setenv("GRASP_S3_PREFIX", "workspace")
	t.Setenv("GRASP_S3_ACCESS_KEY_ID", "synthetic-id")
	t.Setenv("GRASP_S3_SECRET_ACCESS_KEY", "synthetic-secret")
	t.Setenv("GRASP_S3_SESSION_TOKEN", "synthetic-session")
	cfg := &Config{}
	applyEnvOverrides(cfg)
	setDefaults(cfg)
	if cfg.Storage.Driver != "cos" || cfg.Storage.S3.Endpoint != "https://cos.test" || cfg.Storage.S3.Region != "ap-guangzhou" ||
		cfg.Storage.S3.Bucket != "bucket-123456" || cfg.Storage.S3.Prefix != "workspace" ||
		cfg.Storage.S3.AccessKeyID != "synthetic-id" || cfg.Storage.S3.SecretAccessKey != "synthetic-secret" ||
		cfg.Storage.S3.SessionToken != "synthetic-session" || cfg.Storage.S3.PathStyle == nil || *cfg.Storage.S3.PathStyle {
		t.Fatal("COS environment or virtual-host addressing was not applied")
	}
	t.Setenv("GRASP_S3_PATH_STYLE", "true")
	applyEnvOverrides(cfg)
	if !*cfg.Storage.S3.PathStyle {
		t.Fatal("explicit addressing override was ignored")
	}
	localS3 := &Config{Storage: StorageConfig{Driver: "s3"}}
	setDefaults(localS3)
	if localS3.Storage.S3.PathStyle == nil || !*localS3.Storage.S3.PathStyle || localS3.Storage.S3.Region != "us-east-1" {
		t.Fatal("local S3 defaults require path-style addressing and a signing region")
	}
}
