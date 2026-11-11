package solobackup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/cocofhu/grasp/internal/config"
)

const partSize = 8 << 20

// S3 streams archives as multipart uploads, so no local spool is needed.
type S3 struct {
	client *s3.Client
	upload *transfermanager.Client
	bucket string
	prefix string
	max    int64
}

// NewS3 builds a client for an S3-compatible bucket. Config parsing has
// already validated the endpoint and credentials.
func NewS3(cfg config.SoloBackupS3, max int64) (*S3, error) {
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix != "" && !validKey(prefix) {
		return nil, errors.New("solo_backup.s3.prefix must be a relative object prefix")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	opts := s3.Options{
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		UsePathStyle: cfg.PathStyle,
		HTTPClient:   &http.Client{Timeout: 10 * time.Minute}, RetryMaxAttempts: 3,
		// Self-hosted S3 servers implement the portable subset only.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	if endpoint := strings.TrimSuffix(strings.TrimSpace(cfg.Endpoint), "/"); endpoint != "" {
		opts.BaseEndpoint = aws.String(endpoint)
	}
	client := s3.New(opts)
	upload := transfermanager.New(client, func(o *transfermanager.Options) {
		o.PartSizeBytes = partSize
		o.MultipartUploadThreshold = partSize
		o.Concurrency = 2
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	return &S3{client: client, upload: upload, bucket: cfg.Bucket, prefix: prefix, max: max}, nil
}

func (s *S3) Kind() string { return KindS3 }

func (s *S3) key(key string) (string, error) {
	if !validKey(key) {
		return "", keyError(key)
	}
	if s.prefix == "" {
		return key, nil
	}
	return s.prefix + "/" + key, nil
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader) (Object, error) {
	physical, err := s.key(key)
	if err != nil {
		return Object{}, err
	}
	m := newMeter(r, s.max)
	_, err = s.upload.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(physical), Body: m,
		ContentType: aws.String("application/gzip"),
	})
	if errors.Is(err, ErrTooLarge) {
		return Object{}, ErrTooLarge
	}
	if err != nil {
		return Object{}, storageError("upload", err)
	}
	return m.object(), nil
}

func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	physical, err := s.key(key)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(physical)})
	if err != nil {
		return nil, storageError("download", err)
	}
	return out.Body, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	physical, err := s.key(key)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(physical)})
	if err = storageError("delete", err); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// storageError keeps signed URLs and upstream bodies out of user-facing
// errors while preserving the cause for errors.Is.
func storageError(op string, err error) error {
	if err == nil {
		return nil
	}
	var response *smithyhttp.ResponseError
	var api smithy.APIError
	code := ""
	if errors.As(err, &response) {
		code = fmt.Sprint(response.HTTPStatusCode())
		if response.HTTPStatusCode() == http.StatusNotFound {
			return &opError{op: op, code: code, cause: os.ErrNotExist}
		}
	} else if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchBucket":
			return &opError{op: op, code: "404", cause: os.ErrNotExist}
		case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
			code = "403"
		}
	}
	return &opError{op: op, code: code, cause: err}
}

type opError struct {
	op, code string
	cause    error
}

func (e *opError) Error() string {
	if e.code == "" {
		return "backup storage " + e.op + " failed"
	}
	return "backup storage " + e.op + " failed (" + e.code + ")"
}

func (e *opError) Unwrap() error { return e.cause }
