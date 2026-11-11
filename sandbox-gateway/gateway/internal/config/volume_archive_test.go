package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeArchiveConfig(t *testing.T, archive string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	content := "driver: kubernetes\nimage:\n  ref: my/img:1\nkubernetes:\n  volumeArchive:\n" + archive
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validArchive = `    enabled: true
    helperImage: tools:tar
    s3:
      endpoint: https://s3.example.test
      bucket: archives
      prefix: volumes
      accessKeyID: ak
      secretAccessKey: sk
      pathStyle: false
`

func TestVolumeArchiveConfigDefaults(t *testing.T) {
	cfg, err := Load(writeArchiveConfig(t, validArchive))
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.K8s.VolumeArchive
	if !a.Enabled || a.HelperImage != "tools:tar" || a.S3.Bucket != "archives" || a.S3.PathStyle == nil || *a.S3.PathStyle {
		t.Fatalf("archive config %+v", a)
	}
	if cfg.VolumeArchiveAfter() != 30*time.Minute || cfg.VolumeArchiveInterval() != time.Minute || cfg.VolumeArchiveTimeout() != 2*time.Hour {
		t.Fatalf("after=%s interval=%s timeout=%s", cfg.VolumeArchiveAfter(), cfg.VolumeArchiveInterval(), cfg.VolumeArchiveTimeout())
	}
}

func TestVolumeArchiveConfigRejectsUnsafeSettings(t *testing.T) {
	cases := map[string]string{
		"short after":   "    afterSeconds: 60\n",
		"short timeout": "    timeoutSeconds: 10\n",
		"endpoint path": "    s3: {endpoint: https://s3.example.test/bucket, bucket: b, accessKeyID: a, secretAccessKey: s}\n",
		"no keys":       "    s3: {endpoint: https://s3.example.test, bucket: b}\n",
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			archive := validArchive
			if strings.Contains(extra, "s3:") {
				archive = "    enabled: true\n"
			}
			_, err := Load(writeArchiveConfig(t, archive+extra))
			if err == nil || !strings.Contains(err.Error(), "volumeArchive") {
				t.Fatalf("accepted: %v", err)
			}
		})
	}
	// Disabled archiving is not validated.
	if _, err := Load(writeArchiveConfig(t, "    enabled: false\n    afterSeconds: 60\n")); err != nil {
		t.Fatal(err)
	}
}

func TestVolumeArchiveConfigFromEnvironment(t *testing.T) {
	t.Setenv("SBGW_K8S_VOLUME_ARCHIVE_ENABLED", "true")
	t.Setenv("SBGW_K8S_VOLUME_ARCHIVE_AFTER_SEC", "3600")
	t.Setenv("SBGW_K8S_VOLUME_ARCHIVE_S3_ENDPOINT", "http://minio:9000")
	t.Setenv("SBGW_K8S_VOLUME_ARCHIVE_S3_REGION", "cn-1")
	t.Setenv("SBGW_K8S_VOLUME_ARCHIVE_S3_BUCKET", "env-bucket")
	t.Setenv("SBGW_K8S_VOLUME_ARCHIVE_S3_PREFIX", "p")
	t.Setenv("SBGW_K8S_VOLUME_ARCHIVE_S3_ACCESS_KEY_ID", "eak")
	t.Setenv("SBGW_K8S_VOLUME_ARCHIVE_S3_SECRET_ACCESS_KEY", "esk")
	t.Setenv("SBGW_K8S_VOLUME_ARCHIVE_S3_SESSION_TOKEN", "tok")
	cfg, err := Load(writeArchiveConfig(t, "    enabled: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.K8s.VolumeArchive
	if !a.Enabled || cfg.VolumeArchiveAfter() != time.Hour || a.S3.Endpoint != "http://minio:9000" || a.S3.Region != "cn-1" || a.S3.Bucket != "env-bucket" ||
		a.S3.Prefix != "p" || a.S3.AccessKeyID != "eak" || a.S3.SecretAccessKey != "esk" || a.S3.SessionToken != "tok" {
		t.Fatalf("env not applied: %+v", a)
	}
	t.Setenv("SBGW_K8S_VOLUME_ARCHIVE_AFTER_SEC", "soon")
	if _, err := Load(writeArchiveConfig(t, "    enabled: false\n")); err == nil || !strings.Contains(err.Error(), "afterSeconds") {
		t.Fatalf("malformed after accepted: %v", err)
	}
}
