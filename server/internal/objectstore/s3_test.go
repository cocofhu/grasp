package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
)

type wireObject struct {
	body []byte
	info Info
}

// The fixture speaks HTTP/S3 to the real SDK. It is not a mock of Store.
type s3Wire struct {
	mu      sync.Mutex
	objects map[string]wireObject
	parts   map[int][]byte
	status  int
	calls   int
	outside bool
}

func (w *s3Wire) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	if !strings.HasPrefix(request.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		http.Error(response, "missing signature", http.StatusForbidden)
		return
	}
	if w.status != 0 {
		response.Header().Set("Content-Type", "application/xml")
		response.WriteHeader(w.status)
		_, _ = io.WriteString(response, "<Error><Code>Denied</Code><Message>private-signed-url-and-secret</Message></Error>")
		return
	}
	query := request.URL.Query()
	if request.Method == http.MethodHead && request.URL.Path == "/bucket" {
		return
	}
	if query.Get("list-type") == "2" {
		response.Header().Set("Content-Type", "application/xml")
		if w.outside {
			_, _ = io.WriteString(response, "<ListBucketResult><Contents><Key>another-root/private</Key><Size>1</Size></Contents></ListBucketResult>")
			return
		}
		_, _ = io.WriteString(response, "<ListBucketResult><IsTruncated>false</IsTruncated>")
		keys := make([]string, 0, len(w.objects))
		for key := range w.objects {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			object := w.objects[key]
			if strings.HasPrefix(strings.TrimPrefix(key, "/bucket/"), query.Get("prefix")) {
				_, _ = fmt.Fprintf(response, "<Contents><Key>%s</Key><Size>%d</Size></Contents>", strings.TrimPrefix(key, "/bucket/"), len(object.body))
			}
		}
		_, _ = io.WriteString(response, "</ListBucketResult>")
		return
	}
	if request.Method == http.MethodPost && query.Has("uploads") {
		w.parts = map[int][]byte{}
		w.objects[request.URL.Path] = wireObject{info: requestInfo(request)}
		_, _ = io.WriteString(response, "<InitiateMultipartUploadResult><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>")
		return
	}
	if request.Method == http.MethodPut && query.Has("partNumber") {
		part, _ := strconv.Atoi(query.Get("partNumber"))
		w.parts[part], _ = io.ReadAll(request.Body)
		response.Header().Set("ETag", fmt.Sprintf("\"part-%d\"", part))
		return
	}
	if request.Method == http.MethodPost && query.Has("uploadId") {
		object := w.objects[request.URL.Path]
		var numbers []int
		for number := range w.parts {
			numbers = append(numbers, number)
		}
		sort.Ints(numbers)
		for _, number := range numbers {
			object.body = append(object.body, w.parts[number]...)
		}
		w.objects[request.URL.Path] = object
		_, _ = io.WriteString(response, "<CompleteMultipartUploadResult><ETag>\"completed\"</ETag></CompleteMultipartUploadResult>")
		return
	}
	switch request.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(request.Body)
		w.objects[request.URL.Path] = wireObject{body, requestInfo(request)}
		response.Header().Set("ETag", "\"immutable-test-object\"")
	case http.MethodGet, http.MethodHead:
		object, ok := w.objects[request.URL.Path]
		if !ok {
			response.Header().Set("Content-Type", "application/xml")
			response.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(response, "<Error><Code>NoSuchKey</Code></Error>")
			return
		}
		response.Header().Set("Content-Length", strconv.Itoa(len(object.body)))
		response.Header().Set("Content-Type", object.info.ContentType)
		for key, value := range object.info.Metadata {
			response.Header().Set("X-Amz-Meta-"+key, value)
		}
		if request.Method == http.MethodGet {
			_, _ = response.Write(object.body)
		}
	case http.MethodDelete:
		delete(w.objects, request.URL.Path)
		response.WriteHeader(http.StatusNoContent)
	default:
		http.Error(response, "unexpected operation", http.StatusBadRequest)
	}
}

func requestInfo(request *http.Request) Info {
	info := Info{ContentType: request.Header.Get("Content-Type"), Metadata: map[string]string{}}
	for key := range request.Header {
		if strings.HasPrefix(strings.ToLower(key), "x-amz-meta-") {
			info.Metadata[strings.TrimPrefix(strings.ToLower(key), "x-amz-meta-")] = request.Header.Get(key)
		}
	}
	return info
}

func fixture(t *testing.T) (*S3, *s3Wire) {
	t.Helper()
	wire := &s3Wire{objects: map[string]wireObject{}}
	server := httptest.NewServer(wire)
	t.Cleanup(server.Close)
	store, err := NewS3(config.S3StorageConfig{Endpoint: server.URL, Region: "us-east-1", Bucket: "bucket", Prefix: "root", AccessKeyID: "synthetic-access", SecretAccessKey: "synthetic-secret"})
	if err != nil {
		t.Fatal(err)
	}
	return store, wire
}

func TestS3WireRoundTripAndMultipart(t *testing.T) {
	store, wire := fixture(t)
	ctx := context.Background()
	if err := store.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 113, multipartThreshold + 117} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			key := "payloads/" + strconv.Itoa(size)
			body := bytes.Repeat([]byte{0, 0xff, 0x24}, size/3+1)[:size]
			info := Info{Size: int64(size), ContentType: "application/octet-stream", Metadata: map[string]string{"grasp-sha256": "checked-by-adapter"}}
			if err := store.Put(ctx, key, bytes.NewReader(body), info); err != nil {
				t.Fatal(err)
			}
			reader, actual, err := store.Open(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			result, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil || !bytes.Equal(result, body) || actual.Size != int64(size) || actual.Metadata["grasp-sha256"] != "checked-by-adapter" {
				t.Fatalf("roundtrip size=%d metadata=%v err=%v", len(result), actual, err)
			}
			head, err := store.Head(ctx, key)
			if err != nil || head.Size != int64(size) {
				t.Fatalf("head=%v err=%v", head, err)
			}
			page, err := store.List(ctx, "payloads/", "")
			if err != nil || len(page.Objects) != 1 || page.Objects[0].Key != key {
				t.Fatalf("page=%v err=%v", page, err)
			}
			if err = store.Delete(ctx, key); err != nil {
				t.Fatal(err)
			}
			if _, err = store.Head(ctx, key); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("deleted head=%v", err)
			}
		})
	}
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if len(wire.parts) != 2 {
		t.Fatalf("large object did not use multipart: %d", len(wire.parts))
	}
}

func TestS3ErrorsDoNotExposeSecretsOrConflateUnavailableWithMissing(t *testing.T) {
	store, wire := fixture(t)
	for _, status := range []int{403, 404, 500} {
		wire.mu.Lock()
		wire.status = status
		wire.mu.Unlock()
		_, _, err := store.Open(context.Background(), "private")
		if err == nil || strings.Contains(err.Error(), "private-signed-url") || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatalf("unsafe or missing error: %v", err)
		}
		if errors.Is(err, os.ErrNotExist) != (status == 404) {
			t.Fatalf("wrong missing classification for %d: %v", status, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := store.Open(ctx, "private")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestS3RejectsInvalidConfigurationAndKeysBeforeNetwork(t *testing.T) {
	base := config.S3StorageConfig{Endpoint: "http://storage.test", Bucket: "bucket", AccessKeyID: "key", SecretAccessKey: "secret"}
	for _, endpoint := range []string{"", "file:///tmp", "http://user:secret@storage.test", "https://storage.test/private", "https://storage.test?token=secret"} {
		cfg := base
		cfg.Endpoint = endpoint
		if _, err := NewS3(cfg); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("endpoint=%q error=%v", endpoint, err)
		}
	}
	for _, change := range []func(*config.S3StorageConfig){
		func(cfg *config.S3StorageConfig) { cfg.Bucket = "" },
		func(cfg *config.S3StorageConfig) { cfg.AccessKeyID = "" },
		func(cfg *config.S3StorageConfig) { cfg.Prefix = "../escape" },
	} {
		cfg := base
		change(&cfg)
		if _, err := NewS3(cfg); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	store, wire := fixture(t)
	for _, key := range []string{"", "../private", "a/../private", "/private", "a\\private", "a\nprivate"} {
		if err := store.Put(context.Background(), key, strings.NewReader("body"), Info{Size: 4}); err == nil {
			t.Fatalf("invalid key accepted: %q", key)
		}
		if _, _, err := store.Open(context.Background(), key); err == nil {
			t.Fatalf("invalid read key accepted: %q", key)
		}
	}
	if err := store.Put(context.Background(), "valid", strings.NewReader("body"), Info{Size: 9}); err == nil {
		t.Fatal("wrong body size accepted")
	}
	if _, err := store.List(context.Background(), "../private", ""); err == nil {
		t.Fatal("invalid list prefix accepted")
	}
	wire.mu.Lock()
	if wire.calls != 0 {
		t.Fatalf("validation reached network: %d", wire.calls)
	}
	wire.outside = true
	wire.mu.Unlock()
	if _, err := store.List(context.Background(), "", ""); err == nil {
		t.Fatal("server returned keys outside the configured root")
	}
}
