package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/cocofhu/grasp/internal/config"
)

const multipartThreshold = 8 << 20

type S3 struct {
	client *s3.Client
	upload *transfermanager.Client
	bucket string
	prefix string
}

// operationError keeps URLs, signatures and server response bodies out of user
// errors while retaining the original error for cancellation and diagnostics.
type operationError struct {
	op    string
	code  string
	cause error
}

func (e *operationError) Error() string {
	if e.code == "" {
		return "object storage " + e.op + " failed"
	}
	return "object storage " + e.op + " failed (" + e.code + ")"
}

func (e *operationError) Unwrap() error { return e.cause }

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
			return &operationError{op: op, code: code, cause: os.ErrNotExist}
		}
	} else if errors.As(err, &api) {
		// Only expose known SDK codes, never arbitrary upstream messages.
		switch api.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchBucket":
			return &operationError{op: op, code: "404", cause: os.ErrNotExist}
		case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
			code = "403"
		}
	}
	return &operationError{op: op, code: code, cause: err}
}

func validKey(key string, empty bool) bool {
	if key == "" {
		return empty
	}
	if path.IsAbs(key) || strings.Contains(key, "\\") || strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return false
	}
	return path.Clean(key) == key && key != "." && key != ".." && !strings.HasPrefix(key, "../")
}

func NewS3(cfg config.S3StorageConfig) (*S3, error) {
	origin, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil || origin.Hostname() == "" || (origin.Scheme != "http" && origin.Scheme != "https") ||
		origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return nil, errors.New("S3 endpoint must be an HTTP(S) origin")
	}
	if strings.TrimSpace(cfg.Bucket) == "" || strings.ContainsAny(cfg.Bucket, "/\\\r\n") {
		return nil, errors.New("S3 bucket is required and must be a bucket name")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("S3 access key and secret key are required")
	}
	prefix := strings.Trim(cfg.Prefix, "/")
	if !validKey(prefix, true) {
		return nil, errors.New("S3 prefix must be a relative object prefix")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	pathStyle := cfg.PathStyle == nil || *cfg.PathStyle
	client := s3.New(s3.Options{
		Region: region, BaseEndpoint: aws.String(strings.TrimSuffix(origin.String(), "/")),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		UsePathStyle: pathStyle, HTTPClient: &http.Client{Timeout: 2 * time.Minute}, RetryMaxAttempts: 3,
		// COS and local S3 servers need the portable API subset. Payload hashes
		// remain platform metadata and are verified by the payload adapters.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	upload := transfermanager.New(client, func(options *transfermanager.Options) {
		options.PartSizeBytes = multipartThreshold
		options.MultipartUploadThreshold = multipartThreshold
		options.Concurrency = 3
		options.FailTimeout = 15 * time.Second
		options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	return &S3{client: client, upload: upload, bucket: cfg.Bucket, prefix: prefix}, nil
}

func (s *S3) key(key string) (string, error) {
	if !validKey(key, false) {
		return "", errors.New("invalid object key")
	}
	if s.prefix == "" {
		return key, nil
	}
	return s.prefix + "/" + key, nil
}

func (s *S3) Probe(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	return storageError("probe", err)
}

func (s *S3) Put(ctx context.Context, key string, body io.ReadSeeker, info Info) error {
	physical, err := s.key(key)
	if err != nil {
		return err
	}
	size, err := body.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if info.Size < 0 || size != info.Size {
		return errors.New("object body size does not match metadata")
	}
	if _, err = body.Seek(0, io.SeekStart); err != nil {
		return err
	}
	contentType := info.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if size < multipartThreshold {
		_, err = s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(physical),
			Body: body, ContentLength: aws.Int64(size), ContentType: aws.String(contentType), Metadata: maps.Clone(info.Metadata)})
	} else {
		_, err = s.upload.UploadObject(ctx, &transfermanager.UploadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(physical),
			Body: body, ContentType: aws.String(contentType), Metadata: maps.Clone(info.Metadata)})
	}
	return storageError("put", err)
}

func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	physical, err := s.key(key)
	if err != nil {
		return nil, Info{}, err
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(physical)})
	if err != nil {
		return nil, Info{}, storageError("get", err)
	}
	return result.Body, Info{Key: key, Size: aws.ToInt64(result.ContentLength), ContentType: aws.ToString(result.ContentType),
		Metadata: maps.Clone(result.Metadata), ETag: aws.ToString(result.ETag), VersionID: aws.ToString(result.VersionId)}, nil
}

func (s *S3) Head(ctx context.Context, key string) (Info, error) {
	physical, err := s.key(key)
	if err != nil {
		return Info{}, err
	}
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(physical)})
	if err != nil {
		return Info{}, storageError("head", err)
	}
	return Info{Key: key, Size: aws.ToInt64(result.ContentLength), ContentType: aws.ToString(result.ContentType),
		Metadata: maps.Clone(result.Metadata), ETag: aws.ToString(result.ETag), VersionID: aws.ToString(result.VersionId)}, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	physical, err := s.key(key)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(physical)})
	return storageError("delete", err)
}

func (s *S3) List(ctx context.Context, prefix, cursor string) (Page, error) {
	if !validKey(strings.TrimSuffix(prefix, "/"), true) {
		return Page{}, errors.New("invalid object prefix")
	}
	root := ""
	if s.prefix != "" {
		root = s.prefix + "/"
	}
	input := &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(root + prefix), MaxKeys: aws.Int32(1000)}
	if cursor != "" {
		input.ContinuationToken = aws.String(cursor)
	}
	result, err := s.client.ListObjectsV2(ctx, input)
	if err != nil {
		return Page{}, storageError("list", err)
	}
	page := Page{Next: aws.ToString(result.NextContinuationToken)}
	for _, object := range result.Contents {
		physical := aws.ToString(object.Key)
		if !strings.HasPrefix(physical, root) {
			return Page{}, errors.New("object listing escaped configured prefix")
		}
		page.Objects = append(page.Objects, Info{Key: strings.TrimPrefix(physical, root), Size: aws.ToInt64(object.Size), ETag: aws.ToString(object.ETag)})
	}
	return page, nil
}
