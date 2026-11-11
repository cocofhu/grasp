package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRetiredExecutionControlSettingsDoNotOverrideStorage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	const current = `database:
  driver: sqlite
  path: data/application.sqlite
storage:
  driver: cos
  blobs_root: data/payloads
  s3:
    endpoint: https://payload.example.test
    region: ap-guangzhou
    bucket: payload-bucket
    prefix: application
    access_key_id: synthetic-payload-id
    secret_access_key: synthetic-payload-secret
`
	if err := os.WriteFile(path, []byte(current), 0600); err != nil {
		t.Fatal(err)
	}
	want, err := parse(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"DRIVER", "BLOBS_ROOT", "S3_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_PREFIX", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "S3_SESSION_TOKEN", "S3_PATH_STYLE"} {
		t.Setenv("GRASP_EXECUTION_CONTROL_"+key, "retired-setting-is-not-read")
	}
	const retired = `execution_control:
  driver: s3
  blobs_root: data/application.sqlite
  s3:
    endpoint: https://retired-control.example.test
    bucket: retired-control
    prefix: control
    access_key_id: retired-synthetic-id
    secret_access_key: retired-synthetic-secret
    path_style: not-a-boolean
`
	if err := os.WriteFile(path, []byte(current+retired), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := parse(path)
	if err != nil {
		t.Fatalf("retired configuration must not be parsed or block the current configuration: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("retired control YAML/environment changed the resolved SQL or payload configuration")
	}
	for _, descriptor := range OptionDescriptors() {
		if strings.HasPrefix(descriptor.Env, "GRASP_EXECUTION_CONTROL_") || strings.HasPrefix(descriptor.YAML, "execution_control.") {
			t.Fatal("retired control configuration is still advertised as a supported option")
		}
	}
}
