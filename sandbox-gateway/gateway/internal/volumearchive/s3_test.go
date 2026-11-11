package volumearchive

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeS3 implements the path-style subset of S3 that the store uses.
type fakeS3 struct {
	mu       sync.Mutex
	objects  map[string][]byte
	uploads  map[string]map[int][]byte
	aborted  int
	failPart int
	failAll  bool
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string][]byte{}, uploads: map[string]map[int][]byte{}}
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>down</Message></Error>`)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/archives/")
	q := r.URL.Query()
	body, _ := io.ReadAll(r.Body)
	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		id := fmt.Sprintf("u%d", len(f.uploads)+1)
		f.uploads[id] = map[int][]byte{}
		_, _ = fmt.Fprintf(w, `<InitiateMultipartUploadResult><Bucket>archives</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, key, id)
	case r.Method == http.MethodPut && q.Has("uploadId"):
		n, _ := strconv.Atoi(q.Get("partNumber"))
		if n == f.failPart {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>part</Message></Error>`)
			return
		}
		f.uploads[q.Get("uploadId")][n] = body
		w.Header().Set("ETag", fmt.Sprintf(`"etag-%d"`, n))
	case r.Method == http.MethodPost && q.Has("uploadId"):
		var done struct {
			Parts []struct{ PartNumber int } `xml:"Part"`
		}
		_ = xml.Unmarshal(body, &done)
		parts := f.uploads[q.Get("uploadId")]
		var nums []int
		for _, p := range done.Parts {
			nums = append(nums, p.PartNumber)
		}
		sort.Ints(nums)
		var data []byte
		for _, n := range nums {
			data = append(data, parts[n]...)
		}
		f.objects[key] = data
		delete(f.uploads, q.Get("uploadId"))
		_, _ = fmt.Fprintf(w, `<CompleteMultipartUploadResult><Key>%s</Key><ETag>"done"</ETag></CompleteMultipartUploadResult>`, key)
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		f.aborted++
		delete(f.uploads, q.Get("uploadId"))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		f.objects[key] = body
	case r.Method == http.MethodHead, r.Method == http.MethodGet:
		data, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`)
			}
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func testStore(t *testing.T, partSize int) (*S3, *fakeS3) {
	t.Helper()
	fake := newFakeS3()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	store, err := NewS3(S3Config{Endpoint: srv.URL + "/", Bucket: "archives", Prefix: "/volumes/", AccessKeyID: "ak", SecretAccessKey: "sk", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	store.partSize = partSize
	return store, fake
}

func TestS3RoundTripSmallAndMultipart(t *testing.T) {
	ctx := context.Background()
	for name, size := range map[string]int{"single": 5, "exact parts": 16, "multipart": 21} {
		t.Run(name, func(t *testing.T) {
			store, fake := testStore(t, 8)
			data := bytes.Repeat([]byte("abcdefg\n"), 3)[:size]
			n, err := store.Put(ctx, "ns/id/a.tar.gz", bytes.NewReader(data))
			if err != nil || n != int64(size) {
				t.Fatalf("put n=%d err=%v", n, err)
			}
			if !bytes.Equal(fake.objects["volumes/ns/id/a.tar.gz"], data) {
				t.Fatalf("stored %q", fake.objects["volumes/ns/id/a.tar.gz"])
			}
			if got, err := store.Size(ctx, "ns/id/a.tar.gz"); err != nil || got != int64(size) {
				t.Fatalf("size=%d err=%v", got, err)
			}
			body, err := store.Get(ctx, "ns/id/a.tar.gz")
			if err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(body)
			_ = body.Close()
			if !bytes.Equal(got, data) {
				t.Fatalf("read %q", got)
			}
			if err := store.Delete(ctx, "ns/id/a.tar.gz"); err != nil || len(fake.objects) != 0 {
				t.Fatalf("delete err=%v objects=%d", err, len(fake.objects))
			}
		})
	}
}

func TestS3MultipartFailureAborts(t *testing.T) {
	store, fake := testStore(t, 4)
	fake.failPart = 2
	if _, err := store.Put(context.Background(), "k", strings.NewReader("0123456789")); err == nil {
		t.Fatal("part failure hidden")
	}
	if fake.aborted != 1 || len(fake.uploads) != 0 || len(fake.objects) != 0 {
		t.Fatalf("upload not aborted: aborted=%d uploads=%d", fake.aborted, len(fake.uploads))
	}
	fake.failPart = 0
	if _, err := store.Put(context.Background(), "k", io.MultiReader(strings.NewReader("01234567"), errReader{})); err == nil || fake.aborted != 2 {
		t.Fatalf("body failure not aborted: %v aborted=%d", err, fake.aborted)
	}
	if _, err := store.Put(context.Background(), "k", errReader{}); err == nil {
		t.Fatal("first read failure hidden")
	}
	store.partSize = 1
	big := strings.NewReader(strings.Repeat("x", maxParts+1))
	if _, err := store.Put(context.Background(), "k", big); err == nil || !strings.Contains(err.Error(), "part limit") {
		t.Fatalf("part limit not enforced: %v", err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("tar died") }

func TestS3ReportsServerErrors(t *testing.T) {
	ctx := context.Background()
	store, fake := testStore(t, 4)
	if _, err := store.Size(ctx, "missing"); err == nil {
		t.Fatal("missing object stat succeeded")
	}
	if _, err := store.Get(ctx, "missing"); err == nil {
		t.Fatal("missing object read succeeded")
	}
	fake.failAll = true
	if _, err := store.Put(ctx, "k", strings.NewReader("ab")); err == nil {
		t.Fatal("put failure hidden")
	}
	if _, err := store.Put(ctx, "k", strings.NewReader("0123456789")); err == nil {
		t.Fatal("multipart start failure hidden")
	}
	if err := store.Delete(ctx, "k"); err == nil {
		t.Fatal("delete failure hidden")
	}
}

func TestS3CompleteFailureAborts(t *testing.T) {
	fake := newFakeS3()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Query().Has("uploadId") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>complete</Message></Error>`)
			return
		}
		fake.ServeHTTP(w, r)
	}))
	defer srv.Close()
	store, err := NewS3(S3Config{Endpoint: srv.URL, Region: "cn-north-1", Bucket: "archives", AccessKeyID: "ak", SecretAccessKey: "sk", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	store.partSize = 4
	if _, err := store.Put(context.Background(), "k", strings.NewReader("0123456789")); err == nil || !strings.Contains(err.Error(), "complete") {
		t.Fatalf("complete failure hidden: %v", err)
	}
	if fake.aborted != 1 {
		t.Fatal("failed completion not aborted")
	}
}

func TestNewS3Validation(t *testing.T) {
	good := S3Config{Endpoint: "https://s3.example.test", Bucket: "b", AccessKeyID: "ak", SecretAccessKey: "sk"}
	for name, mutate := range map[string]func(*S3Config){
		"scheme":    func(c *S3Config) { c.Endpoint = "ftp://s3.example.test" },
		"path":      func(c *S3Config) { c.Endpoint = "https://s3.example.test/bucket" },
		"userinfo":  func(c *S3Config) { c.Endpoint = "https://u:p@s3.example.test" },
		"query":     func(c *S3Config) { c.Endpoint = "https://s3.example.test?x=1" },
		"empty":     func(c *S3Config) { c.Endpoint = "" },
		"bucket":    func(c *S3Config) { c.Bucket = "a/b" },
		"no bucket": func(c *S3Config) { c.Bucket = " " },
		"keys":      func(c *S3Config) { c.SecretAccessKey = "" },
	} {
		cfg := good
		mutate(&cfg)
		if _, err := NewS3(cfg); err == nil {
			t.Fatalf("%s: invalid config accepted", name)
		}
	}
	store, err := NewS3(good)
	if err != nil || store.key("x") != "x" {
		t.Fatalf("store=%v err=%v", store, err)
	}
}
