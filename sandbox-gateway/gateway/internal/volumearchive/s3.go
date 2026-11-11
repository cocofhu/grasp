// Package volumearchive stores whole-volume archives of stopped Kubernetes
// sandboxes in an S3-compatible object store.
package volumearchive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	defaultPartSize = 64 << 20
	maxParts        = 10000
)

// S3Config selects the bucket and credentials for volume archives.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	PathStyle       bool
}

// S3 streams archives with multipart uploads so no archive is buffered whole.
type S3 struct {
	client   *s3.Client
	bucket   string
	prefix   string
	partSize int
}

// NewS3 validates cfg and builds the store.
func NewS3(cfg S3Config) (*S3, error) {
	origin, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil || origin.Hostname() == "" || (origin.Scheme != "http" && origin.Scheme != "https") ||
		origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return nil, errors.New("volume archive S3 endpoint must be an HTTP(S) origin")
	}
	if strings.TrimSpace(cfg.Bucket) == "" || strings.ContainsAny(cfg.Bucket, "/\\\r\n") {
		return nil, errors.New("volume archive S3 bucket is required and must be a bucket name")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("volume archive S3 access key and secret key are required")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	client := s3.New(s3.Options{
		Region:       region,
		BaseEndpoint: aws.String(strings.TrimSuffix(origin.String(), "/")),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		UsePathStyle: cfg.PathStyle,
		// Self-hosted S3 servers support the portable subset. Archive integrity
		// is verified by size and SHA-256 recorded on the Deployment.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &S3{client: client, bucket: cfg.Bucket, prefix: strings.Trim(cfg.Prefix, "/"), partSize: defaultPartSize}, nil
}

func (s *S3) key(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

// Put uploads body and returns the number of bytes stored.
func (s *S3) Put(ctx context.Context, key string, body io.Reader) (int64, error) {
	buf := make([]byte, s.partSize)
	n, err := io.ReadFull(body, buf)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
			Body: bytes.NewReader(buf[:n]), ContentLength: aws.Int64(int64(n)),
		})
		if err != nil {
			return 0, fmt.Errorf("put volume archive: %w", err)
		}
		return int64(n), nil
	}
	if err != nil {
		return 0, err
	}
	created, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key))})
	if err != nil {
		return 0, fmt.Errorf("start volume archive upload: %w", err)
	}
	total, err := s.uploadParts(ctx, key, created.UploadId, body, buf, n)
	if err != nil {
		abort, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		_, abortErr := s.client.AbortMultipartUpload(abort, &s3.AbortMultipartUploadInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)), UploadId: created.UploadId})
		return 0, errors.Join(err, abortErr)
	}
	return total, nil
}

// uploadParts sends the already-read first part and the rest of body.
func (s *S3) uploadParts(ctx context.Context, key string, uploadID *string, body io.Reader, buf []byte, n int) (int64, error) {
	var parts []types.CompletedPart
	var total int64
	for number := int32(1); ; number++ {
		if number > maxParts {
			return 0, errors.New("volume archive exceeds the multipart part limit")
		}
		out, err := s.client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)), UploadId: uploadID,
			PartNumber: aws.Int32(number), Body: bytes.NewReader(buf[:n]), ContentLength: aws.Int64(int64(n)),
		})
		if err != nil {
			return 0, fmt.Errorf("upload volume archive part %d: %w", number, err)
		}
		parts = append(parts, types.CompletedPart{ETag: out.ETag, PartNumber: aws.Int32(number)})
		total += int64(n)
		if n < len(buf) {
			break
		}
		n, err = io.ReadFull(body, buf)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, err
		}
	}
	_, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)), UploadId: uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	if err != nil {
		return 0, fmt.Errorf("complete volume archive upload: %w", err)
	}
	return total, nil
}

// Size returns the stored object length.
func (s *S3) Size(ctx context.Context, key string) (int64, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key))})
	if err != nil {
		return 0, fmt.Errorf("stat volume archive: %w", err)
	}
	return aws.ToInt64(out.ContentLength), nil
}

// Get opens the stored object for streaming.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key))})
	if err != nil {
		return nil, fmt.Errorf("read volume archive: %w", err)
	}
	return out.Body, nil
}

// Delete removes the object; deleting a missing object succeeds.
func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.key(key))})
	if err != nil {
		return fmt.Errorf("delete volume archive: %w", err)
	}
	return nil
}
