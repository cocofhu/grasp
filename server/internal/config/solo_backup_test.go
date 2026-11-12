package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSoloBackupEnvAndDefaults(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "none.yaml")
	c, err := parse(missing)
	if err != nil {
		t.Fatal(err)
	}
	if c.SoloBackup.LocalDir != "data/solo-backups" || c.SoloBackup.MaxBytes != 4<<30 || c.SoloBackup.S3.Enabled() ||
		c.SoloBackup.Keep != 5 || c.SoloBackup.Interval() != 6*time.Hour {
		t.Fatalf("defaults = %+v", c.SoloBackup)
	}

	t.Setenv("GRASP_SOLO_BACKUP_DIR", "/var/backups")
	t.Setenv("GRASP_SOLO_BACKUP_MAX_BYTES", "1024")
	t.Setenv("GRASP_SOLO_BACKUP_S3_ENDPOINT", "https://s3.example.test")
	t.Setenv("GRASP_SOLO_BACKUP_S3_REGION", "us-east-1")
	t.Setenv("GRASP_SOLO_BACKUP_S3_BUCKET", "grasp")
	t.Setenv("GRASP_SOLO_BACKUP_S3_PREFIX", "prod")
	t.Setenv("GRASP_SOLO_BACKUP_S3_ACCESS_KEY_ID", "ak")
	t.Setenv("GRASP_SOLO_BACKUP_S3_SECRET_ACCESS_KEY", "sk")
	t.Setenv("GRASP_SOLO_BACKUP_S3_PATH_STYLE", "true")
	t.Setenv("GRASP_SOLO_BACKUP_KEEP", "3")
	t.Setenv("GRASP_SOLO_BACKUP_INTERVAL_MINUTES", "-1")
	c, err = parse(missing)
	if err != nil {
		t.Fatal(err)
	}
	want := SoloBackupConfig{LocalDir: "/var/backups", MaxBytes: 1024, Keep: 3, IntervalMinutes: -1, S3: SoloBackupS3{
		Endpoint: "https://s3.example.test", Region: "us-east-1", Bucket: "grasp", Prefix: "prod",
		AccessKeyID: "ak", SecretAccessKey: "sk", PathStyle: true,
	}}
	if c.SoloBackup != want {
		t.Fatalf("env = %+v", c.SoloBackup)
	}

	t.Setenv("GRASP_SOLO_BACKUP_S3_SECRET_ACCESS_KEY", "")
	if _, err := parse(missing); err == nil {
		t.Fatal("bucket without a secret must fail startup")
	}
}

func TestValidateSoloBackup(t *testing.T) {
	ok := SoloBackupS3{Bucket: "grasp", AccessKeyID: "ak", SecretAccessKey: "sk"}
	for _, tc := range []struct {
		name   string
		s3     SoloBackupS3
		wantOK bool
	}{
		{"local", SoloBackupS3{}, true},
		{"aws", ok, true},
		{"origin", s3With(ok, func(s *SoloBackupS3) { s.Endpoint = "http://minio:9000/" }), true},
		{"endpoint-without-bucket", SoloBackupS3{Endpoint: "https://s3.test"}, false},
		{"keys-without-bucket", SoloBackupS3{AccessKeyID: "ak"}, false},
		{"bucket-path", s3With(ok, func(s *SoloBackupS3) { s.Bucket = "a/b" }), false},
		{"missing-secret", s3With(ok, func(s *SoloBackupS3) { s.SecretAccessKey = "" }), false},
		{"scheme", s3With(ok, func(s *SoloBackupS3) { s.Endpoint = "ftp://s3.test" }), false},
		{"path", s3With(ok, func(s *SoloBackupS3) { s.Endpoint = "https://s3.test/bucket" }), false},
		{"userinfo", s3With(ok, func(s *SoloBackupS3) { s.Endpoint = "https://u:p@s3.test" }), false},
		{"query", s3With(ok, func(s *SoloBackupS3) { s.Endpoint = "https://s3.test?x=1" }), false},
		{"no-host", s3With(ok, func(s *SoloBackupS3) { s.Endpoint = "https://" }), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSoloBackup(SoloBackupConfig{S3: tc.s3})
			if (err == nil) != tc.wantOK {
				t.Fatalf("err = %v, want ok=%v", err, tc.wantOK)
			}
		})
	}
}

func s3With(s SoloBackupS3, fn func(*SoloBackupS3)) SoloBackupS3 {
	fn(&s)
	return s
}
