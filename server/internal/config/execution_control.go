package config

import "strconv"

func applyExecutionControlEnv(c *Config) {
	keys := []string{"DRIVER", "BLOBS_ROOT", "S3_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_PREFIX", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "S3_SESSION_TOKEN", "S3_PATH_STYLE"}
	for _, key := range keys {
		if env("GRASP_EXECUTION_CONTROL_"+key) != "" && c.ExecutionControl == nil {
			c.ExecutionControl = &StorageConfig{}
		}
	}
	if c.ExecutionControl == nil {
		return
	}
	control := c.ExecutionControl
	for key, field := range map[string]*string{
		"DRIVER": &control.Driver, "BLOBS_ROOT": &control.BlobsRoot,
		"S3_ENDPOINT": &control.S3.Endpoint, "S3_REGION": &control.S3.Region,
		"S3_BUCKET": &control.S3.Bucket, "S3_PREFIX": &control.S3.Prefix,
		"S3_ACCESS_KEY_ID": &control.S3.AccessKeyID, "S3_SECRET_ACCESS_KEY": &control.S3.SecretAccessKey,
		"S3_SESSION_TOKEN": &control.S3.SessionToken,
	} {
		if value := env("GRASP_EXECUTION_CONTROL_" + key); value != "" {
			*field = value
		}
	}
	if value := env("GRASP_EXECUTION_CONTROL_S3_PATH_STYLE"); value != "" {
		if pathStyle, err := strconv.ParseBool(value); err == nil {
			control.S3.PathStyle = &pathStyle
		}
	}
	// Do not infer a local driver/root or copy any payload credentials. A
	// partially configured authority must fail closed at execution startup.
}
