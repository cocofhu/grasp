package kubernetes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	kt "k8s.io/client-go/testing"
	"sandbox-gateway/internal/driver"
)

func init() {
	archivePollInterval = time.Millisecond
}

type memStore struct {
	mu        sync.Mutex
	objects   map[string][]byte
	deleted   []string
	putErr    error
	getErr    error
	sizeErr   error
	deleteErr error
	sizeDelta int64
}

func newMemStore() *memStore { return &memStore{objects: map[string][]byte{}} }

func (m *memStore) Put(_ context.Context, key string, body io.Reader) (int64, error) {
	data, err := io.ReadAll(body)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putErr != nil {
		return 0, m.putErr
	}
	if err != nil {
		return 0, err
	}
	m.objects[key] = data
	return int64(len(data)), nil
}

func (m *memStore) Size(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sizeErr != nil {
		return 0, m.sizeErr
	}
	data, ok := m.objects[key]
	if !ok {
		return 0, errors.New("no such key")
	}
	return int64(len(data)) + m.sizeDelta, nil
}

func (m *memStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("no such key")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.objects, key)
	m.deleted = append(m.deleted, key)
	return nil
}

func (m *memStore) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}

func (m *memStore) set(fn func(*memStore)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m)
}

type fakeExec struct {
	mu        sync.Mutex
	volume    []byte
	extracted []byte
	calls     []string
	fail      map[string]error
	gate      chan struct{}
	onCreate  func()
}

func (f *fakeExec) run(_ context.Context, _, _ string, command []string, stdin io.Reader, stdout io.Writer) error {
	kind := command[0]
	if len(command) > 1 {
		kind = strings.TrimPrefix(command[1], "--")
	}
	f.mu.Lock()
	f.calls = append(f.calls, kind)
	err := f.fail[kind]
	gate := f.gate
	onCreate := f.onCreate
	f.mu.Unlock()
	if kind == "create" && onCreate != nil {
		onCreate()
	}
	if kind == "extract" && gate != nil {
		<-gate
	}
	if err != nil {
		return err
	}
	switch kind {
	case "create":
		_, err := stdout.Write(f.volume)
		return err
	case "extract":
		data, err := io.ReadAll(stdin)
		f.mu.Lock()
		f.extracted = data
		f.mu.Unlock()
		return err
	}
	return nil
}

func (f *fakeExec) setFail(kind string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail == nil {
		f.fail = map[string]error{}
	}
	f.fail[kind] = err
}

func (f *fakeExec) restored() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.extracted
}

type archiveEnv struct {
	d     *Driver
	cs    *fake.Clientset
	store *memStore
	exec  *fakeExec
	// helperPhase is the phase newly created helper pods report.
	helperPhase corev1.PodPhase
}

func archiveFixture(t *testing.T) *archiveEnv {
	t.Helper()
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	store := newMemStore()
	fx := &fakeExec{volume: gzipFriendly()}
	d := NewFromClient(cs, Options{Namespace: "sandboxes", NamePrefix: "sbx-", StorageClass: "fast",
		VolumeArchive: VolumeArchiveOptions{Store: store, After: 30 * time.Minute, Timeout: time.Minute}})
	d.exec = fx.run
	env := &archiveEnv{d: d, cs: cs, store: store, exec: fx, helperPhase: corev1.PodRunning}
	cs.PrependReactor("create", "pods", func(action kt.Action) (bool, runtime.Object, error) {
		action.(kt.CreateAction).GetObject().(*corev1.Pod).Status.Phase = env.helperPhase
		return false, nil, nil
	})
	if _, err := d.Create(ctx, driver.Spec{ID: "keep", Image: "sandbox:1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.scale(ctx, "keep", 0); err != nil {
		t.Fatal(err)
	}
	env.ageStop(t, time.Hour)
	pvc, _ := cs.CoreV1().PersistentVolumeClaims("sandboxes").Get(ctx, "sbx-keep-data", metav1.GetOptions{})
	pvc.UID = "pvc-source"
	pvc.Status.Phase = corev1.ClaimBound
	if _, err := cs.CoreV1().PersistentVolumeClaims("sandboxes").Update(ctx, pvc, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	return env
}

func gzipFriendly() []byte { return bytes.Repeat([]byte("workspace file contents\n"), 4096) }

func (e *archiveEnv) dep(t *testing.T) *appsv1.Deployment {
	t.Helper()
	dep, err := e.cs.AppsV1().Deployments("sandboxes").Get(context.Background(), "sbx-keep", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return dep
}

func (e *archiveEnv) archive(t *testing.T) *volumeArchive {
	t.Helper()
	a, err := readArchive(e.dep(t))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (e *archiveEnv) ageStop(t *testing.T, age time.Duration) {
	t.Helper()
	dep := e.dep(t)
	stampStopped(dep, time.Now().Add(-age))
	if _, err := e.cs.AppsV1().Deployments("sandboxes").Update(context.Background(), dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func (e *archiveEnv) setArchive(t *testing.T, a *volumeArchive) {
	t.Helper()
	dep := e.dep(t)
	writeArchive(dep, a)
	if _, err := e.cs.AppsV1().Deployments("sandboxes").Update(context.Background(), dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func (e *archiveEnv) pvcExists(t *testing.T) bool {
	t.Helper()
	_, err := e.cs.CoreV1().PersistentVolumeClaims("sandboxes").Get(context.Background(), "sbx-keep-data", metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatal(err)
	}
	return err == nil
}

func (e *archiveEnv) helperExists(t *testing.T) bool {
	t.Helper()
	_, err := e.cs.CoreV1().Pods("sandboxes").Get(context.Background(), "sbx-keep-volume", metav1.GetOptions{})
	return err == nil
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestVolumeArchiveReleasesPVCAndStartRestoresIt(t *testing.T) {
	e := archiveFixture(t)
	ctx := context.Background()
	e.d.SweepVolumeArchives(ctx)

	a := e.archive(t)
	if a == nil || a.State != archiveStateArchived || a.SourcePVCUID != "pvc-source" || a.Storage != "25Gi" || a.StorageClass != "fast" || a.Bytes == 0 || len(a.SHA256) != 64 || a.Claim != "" {
		t.Fatalf("archive not recorded: %+v", a)
	}
	if e.pvcExists(t) || e.helperExists(t) || e.store.count() != 1 {
		t.Fatalf("pvc=%v helper=%v objects=%d", e.pvcExists(t), e.helperExists(t), e.store.count())
	}
	if st, err := e.d.Status(ctx, "keep"); err != nil || st != driver.StatusStopped {
		t.Fatalf("archived sandbox status=%s err=%v", st, err)
	}
	// A second sweep leaves an archived sandbox alone.
	e.d.SweepVolumeArchives(ctx)

	if err := e.d.Start(ctx, "keep"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restore", func() bool {
		dep := e.dep(t)
		return dep.Annotations[archiveAnnotation] == "" && dep.Spec.Replicas != nil && *dep.Spec.Replicas == 1
	})
	if !bytes.Equal(e.exec.restored(), e.exec.volume) {
		t.Fatalf("restored %d bytes, want %d", len(e.exec.restored()), len(e.exec.volume))
	}
	pvc, err := e.cs.CoreV1().PersistentVolumeClaims("sandboxes").Get(ctx, "sbx-keep-data", metav1.GetOptions{})
	if err != nil || pvc.Spec.Resources.Requests.Storage().String() != "25Gi" || *pvc.Spec.StorageClassName != "fast" || pvc.Labels["sandbox-gateway.io/id"] != "keep" {
		t.Fatalf("restored pvc %+v err=%v", pvc, err)
	}
	waitFor(t, "consumed archive delete", func() bool { return e.store.count() == 0 })
	if e.helperExists(t) || e.dep(t).Annotations[stoppedAtAnnotation] != "" {
		t.Fatal("helper or stop stamp left after restore")
	}
}

func TestVolumeArchiveSkipsSandboxesThatAreNotDue(t *testing.T) {
	cases := map[string]func(t *testing.T, e *archiveEnv){
		"recent stop": func(t *testing.T, e *archiveEnv) { e.ageStop(t, time.Minute) },
		"running": func(t *testing.T, e *archiveEnv) {
			if err := e.d.scale(context.Background(), "keep", 1); err != nil {
				t.Fatal(err)
			}
		},
		"pod still running": func(t *testing.T, e *archiveEnv) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sbx-keep-abc", Namespace: "sandboxes", Labels: e.d.selector("keep")}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
			if _, err := e.cs.CoreV1().Pods("sandboxes").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
		},
		"pvc not bound": func(t *testing.T, e *archiveEnv) {
			pvc, _ := e.cs.CoreV1().PersistentVolumeClaims("sandboxes").Get(context.Background(), "sbx-keep-data", metav1.GetOptions{})
			pvc.Status.Phase = corev1.ClaimPending
			_, _ = e.cs.CoreV1().PersistentVolumeClaims("sandboxes").Update(context.Background(), pvc, metav1.UpdateOptions{})
		},
		"cold start pending": func(t *testing.T, e *archiveEnv) {
			dep := e.dep(t)
			dep.Annotations[coldAnnotation] = `{"state":"configuring"}`
			_, _ = e.cs.AppsV1().Deployments("sandboxes").Update(context.Background(), dep, metav1.UpdateOptions{})
		},
		"corrupt annotation": func(t *testing.T, e *archiveEnv) {
			dep := e.dep(t)
			dep.Annotations[archiveAnnotation] = "{"
			_, _ = e.cs.AppsV1().Deployments("sandboxes").Update(context.Background(), dep, metav1.UpdateOptions{})
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := archiveFixture(t)
			setup(t, e)
			e.d.SweepVolumeArchives(context.Background())
			if !e.pvcExists(t) || e.store.count() != 0 {
				t.Fatalf("volume released although not due: pvc=%v objects=%d", e.pvcExists(t), e.store.count())
			}
		})
	}
}

func TestVolumeArchiveStampsLegacyStopsBeforeCountingAfter(t *testing.T) {
	e := archiveFixture(t)
	dep := e.dep(t)
	delete(dep.Annotations, stoppedAtAnnotation)
	if _, err := e.cs.AppsV1().Deployments("sandboxes").Update(context.Background(), dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	e.d.SweepVolumeArchives(context.Background())
	at, err := time.Parse(time.RFC3339, e.dep(t).Annotations[stoppedAtAnnotation])
	if err != nil || time.Since(at) > time.Minute || !e.pvcExists(t) {
		t.Fatalf("legacy stop not stamped: %v pvc=%v", err, e.pvcExists(t))
	}
}

func TestVolumeArchiveFailuresRetainThePVC(t *testing.T) {
	cases := map[string]func(e *archiveEnv){
		"tar fails":      func(e *archiveEnv) { e.exec.setFail("create", errors.New("tar: cannot read")) },
		"upload fails":   func(e *archiveEnv) { e.store.set(func(m *memStore) { m.putErr = errors.New("bucket full") }) },
		"size mismatch":  func(e *archiveEnv) { e.store.set(func(m *memStore) { m.sizeDelta = -1 }) },
		"stat fails":     func(e *archiveEnv) { e.store.set(func(m *memStore) { m.sizeErr = errors.New("head failed") }) },
		"helper crashes": func(e *archiveEnv) { e.helperPhase = corev1.PodFailed },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := archiveFixture(t)
			setup(e)
			e.d.SweepVolumeArchives(context.Background())
			if !e.pvcExists(t) || e.archive(t) != nil || e.helperExists(t) {
				t.Fatalf("failed archive changed state: pvc=%v archive=%+v helper=%v", e.pvcExists(t), e.archive(t), e.helperExists(t))
			}
			if name != "upload fails" && e.store.count() != 0 {
				t.Fatal("partial archive object retained")
			}
		})
	}
}

func TestVolumeArchiveLosesToConcurrentStart(t *testing.T) {
	e := archiveFixture(t)
	ctx := context.Background()
	// Start arrives while tar is streaming: the archive must not release the PVC.
	e.exec.onCreate = func() {
		if a := e.archive(t); a == nil || a.State != archiveStateArchiving {
			t.Errorf("tar ran without an archive claim: %+v", a)
		}
		if err := e.d.Start(ctx, "keep"); err != nil {
			t.Error(err)
		}
	}
	e.d.SweepVolumeArchives(ctx)
	if r := e.dep(t).Spec.Replicas; r == nil || *r != 1 {
		t.Fatal("start did not scale the sandbox")
	}
	if !e.pvcExists(t) || e.archive(t) != nil || e.store.count() != 0 {
		t.Fatalf("superseded archive released data: pvc=%v archive=%+v objects=%d", e.pvcExists(t), e.archive(t), e.store.count())
	}
}

func TestStartCancelsInFlightArchive(t *testing.T) {
	e := archiveFixture(t)
	e.setArchive(t, &volumeArchive{State: archiveStateArchiving, Key: "k", Storage: "25Gi", Claim: "other", Lease: time.Now().Add(time.Hour)})
	if err := e.d.Start(context.Background(), "keep"); err != nil {
		t.Fatal(err)
	}
	dep := e.dep(t)
	if dep.Annotations[archiveAnnotation] != "" || *dep.Spec.Replicas != 1 {
		t.Fatalf("archive not cancelled: %v replicas=%d", dep.Annotations, *dep.Spec.Replicas)
	}
}

func TestScaleUpCancelsArchiveClaimedAfterPrepare(t *testing.T) {
	e := archiveFixture(t)
	e.setArchive(t, &volumeArchive{State: archiveStateArchiving, Key: "k", Storage: "25Gi", Claim: "other", Lease: time.Now().Add(time.Hour)})
	if err := e.d.scale(context.Background(), "keep", 1); err != nil {
		t.Fatal(err)
	}
	if e.archive(t) != nil {
		t.Fatal("scale up kept the archive claim")
	}
	e.setArchive(t, &volumeArchive{State: archiveStateArchived, Key: "k", Storage: "25Gi"})
	if err := e.d.scale(context.Background(), "keep", 1); !errors.Is(err, ErrVolumeRestoring) {
		t.Fatalf("scale up without a volume: %v", err)
	}
}

func TestStopWhileRestoringCancelsTheDeferredStart(t *testing.T) {
	e := archiveFixture(t)
	e.d.SweepVolumeArchives(context.Background())
	e.exec.gate = make(chan struct{})
	if err := e.d.Start(context.Background(), "keep"); err != nil {
		t.Fatal(err)
	}
	if st, err := e.d.Status(context.Background(), "keep"); err != nil || st != driver.StatusPending {
		t.Fatalf("restoring start status=%s err=%v", st, err)
	}
	// A second start while restoring is idempotent.
	if err := e.d.Start(context.Background(), "keep"); err != nil {
		t.Fatal(err)
	}
	if err := e.d.scale(context.Background(), "keep", 0); err != nil {
		t.Fatal(err)
	}
	if a := e.archive(t); a == nil || a.StartAfter {
		t.Fatalf("stop did not cancel the deferred start: %+v", a)
	}
	if st, _ := e.d.Status(context.Background(), "keep"); st != driver.StatusStopped {
		t.Fatalf("stopped restore status=%s", st)
	}
	// Starting again re-arms it on the same live claim.
	if err := e.d.Start(context.Background(), "keep"); err != nil {
		t.Fatal(err)
	}
	close(e.exec.gate)
	waitFor(t, "restore", func() bool { r := e.dep(t).Spec.Replicas; return e.archive(t) == nil && r != nil && *r == 1 })
}

func TestVolumeRestoreFailureKeepsTheArchive(t *testing.T) {
	cases := map[string]func(e *archiveEnv){
		"extract fails":  func(e *archiveEnv) { e.exec.setFail("extract", errors.New("tar: no space")) },
		"digest differs": func(e *archiveEnv) { e.store.set(func(m *memStore) { m.getErr = nil }); corruptObjects(e.store) },
		"object missing": func(e *archiveEnv) { e.store.set(func(m *memStore) { m.objects = map[string][]byte{} }) },
		"read fails":     func(e *archiveEnv) { e.store.set(func(m *memStore) { m.getErr = errors.New("get failed") }) },
		"sync fails":     func(e *archiveEnv) { e.exec.setFail("sync", errors.New("io error")) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := archiveFixture(t)
			e.d.SweepVolumeArchives(context.Background())
			objects := e.store.count()
			setup(e)
			if err := e.d.Start(context.Background(), "keep"); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "restore failure", func() bool { a := e.archive(t); return a != nil && a.State == archiveStateArchived })
			a := e.archive(t)
			if a.StartAfter || a.Claim != "" || *e.dep(t).Spec.Replicas != 0 {
				t.Fatalf("failed restore left a start pending: %+v", a)
			}
			if name != "object missing" && e.store.count() != objects {
				t.Fatal("archive deleted after failed restore")
			}
		})
	}
}

func corruptObjects(m *memStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.objects {
		c := append([]byte(nil), v...)
		c[len(c)-1] ^= 0xff
		m.objects[k] = c
	}
}

func TestRestoreReplacesLeftoverPVCs(t *testing.T) {
	e := archiveFixture(t)
	ctx := context.Background()
	e.d.SweepVolumeArchives(ctx)
	// The source PVC survived (lost delete) and must not be reused.
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "sbx-keep-data", Namespace: "sandboxes", UID: "pvc-source", Labels: e.d.selector("keep")}}
	if _, err := e.cs.CoreV1().PersistentVolumeClaims("sandboxes").Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := e.d.Start(ctx, "keep"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restore", func() bool { return e.archive(t) == nil })
	restored, _ := e.cs.CoreV1().PersistentVolumeClaims("sandboxes").Get(ctx, "sbx-keep-data", metav1.GetOptions{})
	if restored.UID == "pvc-source" || len(e.exec.restored()) == 0 {
		t.Fatal("leftover source pvc was reused instead of restoring the archive")
	}
}

func TestColdStartAndReleaseWaitForArchivedVolume(t *testing.T) {
	e := archiveFixture(t)
	ctx := context.Background()
	e.d.SweepVolumeArchives(ctx)
	e.exec.gate = make(chan struct{})
	if _, err := e.d.GetColdStart(ctx, "keep", "req"); !errors.Is(err, ErrVolumeRestoring) {
		t.Fatalf("receipt read on archived volume: %v", err)
	}
	if e.archive(t).State != archiveStateArchived {
		t.Fatal("read-only receipt lookup started a restore")
	}
	if _, err := e.d.PreviewColdStart(ctx, "keep"); !errors.Is(err, ErrVolumeRestoring) {
		t.Fatalf("cold preview on archived volume: %v", err)
	}
	if a := e.archive(t); a.State != archiveStateRestoring || a.StartAfter {
		t.Fatalf("cold preview must restore without starting: %+v", a)
	}
	_, _, coldOpts := coldK8sFixture(t)
	if _, err := e.d.StartConfigured(ctx, "keep", coldOpts); !errors.Is(err, ErrVolumeRestoring) {
		t.Fatalf("cold start on archived volume: %v", err)
	}
	if _, err := e.d.PreviewEnvironmentRelease(ctx, "keep"); !errors.Is(err, driver.ErrEnvironmentReleasePending) {
		t.Fatalf("release preview on archived volume: %v", err)
	}
	if err := e.d.Reinstall(ctx, driver.Spec{ID: "keep", Image: "sandbox:1"}, true); !errors.Is(err, ErrVolumeRestoring) {
		t.Fatalf("preserving reinstall on archived volume: %v", err)
	}
	close(e.exec.gate)
	waitFor(t, "restore", func() bool { return e.archive(t) == nil })
	if r := e.dep(t).Spec.Replicas; *r != 0 {
		t.Fatal("a restore without a start request scaled the sandbox")
	}
	// The fake clientset assigns no UIDs, so only the volume gate is asserted.
	if _, err := e.d.PreviewColdStart(ctx, "keep"); errors.Is(err, ErrVolumeRestoring) {
		t.Fatal(err)
	}
}

func TestReleasePreviewCancelsInFlightArchive(t *testing.T) {
	e := archiveFixture(t)
	e.setArchive(t, &volumeArchive{State: archiveStateArchiving, Key: "k", Storage: "25Gi", Claim: "c", Lease: time.Now().Add(time.Hour)})
	_, err := e.d.PreviewEnvironmentRelease(context.Background(), "keep")
	if errors.Is(err, driver.ErrEnvironmentReleasePending) || e.archive(t) != nil {
		t.Fatalf("release preview kept a running archive: %v", err)
	}
	at, _ := time.Parse(time.RFC3339, e.dep(t).Annotations[stoppedAtAnnotation])
	if time.Since(at) > time.Minute {
		t.Fatal("cancelled archive did not restart the stop clock")
	}
}

func TestDestroyAndWipingReinstallDropTheArchive(t *testing.T) {
	for _, op := range []string{"destroy", "reinstall"} {
		t.Run(op, func(t *testing.T) {
			e := archiveFixture(t)
			ctx := context.Background()
			e.d.SweepVolumeArchives(ctx)
			var err error
			if op == "destroy" {
				err = e.d.Destroy(ctx, "keep")
			} else {
				err = e.d.Reinstall(ctx, driver.Spec{ID: "keep", Image: "sandbox:1"}, false)
			}
			if err != nil || e.store.count() != 0 {
				t.Fatalf("%s kept archive: err=%v objects=%d", op, err, e.store.count())
			}
		})
	}
}

func TestDropArchiveLogsButDoesNotFailTeardown(t *testing.T) {
	e := archiveFixture(t)
	ctx := context.Background()
	e.d.SweepVolumeArchives(ctx)
	e.store.set(func(m *memStore) { m.deleteErr = errors.New("denied") })
	if err := e.d.Destroy(ctx, "keep"); err != nil {
		t.Fatal(err)
	}
	e2 := archiveFixture(t)
	e2.d.SweepVolumeArchives(ctx)
	e2.d.opts.VolumeArchive.Store = nil
	e2.d.dropArchive(ctx, "keep")
	e2.d.dropArchive(ctx, "missing")
}

func TestStaleArchivingClaimIsDiscarded(t *testing.T) {
	e := archiveFixture(t)
	ctx := context.Background()
	e.store.objects["stale"] = []byte("partial")
	e.setArchive(t, &volumeArchive{State: archiveStateArchiving, Key: "stale", Storage: "25Gi", Claim: "dead", Lease: time.Now().Add(-time.Minute)})
	e.d.SweepVolumeArchives(ctx)
	if e.archive(t) != nil || e.store.count() != 0 || !e.pvcExists(t) {
		t.Fatalf("stale claim not discarded: %+v objects=%d", e.archive(t), e.store.count())
	}
}

func TestDiscardKeepsObjectReferencedByArchivedCommit(t *testing.T) {
	e := archiveFixture(t)
	ctx := context.Background()
	e.store.objects["k"] = []byte("data")
	// The archived write succeeded but its response was lost.
	e.setArchive(t, &volumeArchive{State: archiveStateArchived, Key: "k", Storage: "25Gi"})
	e.d.discardArchive(ctx, "keep", volumeArchive{Key: "k", Claim: "mine"})
	if e.store.count() != 1 || e.archive(t) == nil {
		t.Fatal("discard deleted the authoritative archive")
	}
	if err := e.d.Destroy(ctx, "keep"); err != nil {
		t.Fatal(err)
	}
	e.store.objects["k"] = []byte("data")
	e.d.discardArchive(ctx, "keep", volumeArchive{Key: "k", Claim: "mine"})
	if e.store.count() != 0 {
		t.Fatal("unreferenced object kept after deployment is gone")
	}
}

func TestLeaseRenewalAndLoss(t *testing.T) {
	defer func(ttl time.Duration) { archiveLeaseTTL = ttl }(archiveLeaseTTL)
	archiveLeaseTTL = 20 * time.Millisecond
	e := archiveFixture(t)
	before := time.Now().Add(-time.Hour)
	e.setArchive(t, &volumeArchive{State: archiveStateRestoring, Key: "k", Storage: "25Gi", Claim: "mine", Lease: before})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.d.holdLease(ctx, cancel, "keep", "mine"); close(done) }()
	waitFor(t, "renewal", func() bool { return e.archive(t).Lease.After(before) })
	e.setArchive(t, &volumeArchive{State: archiveStateRestoring, Key: "k", Storage: "25Gi", Claim: "other", Lease: time.Now()})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("lost lease did not stop the holder")
	}
	if ctx.Err() == nil {
		t.Fatal("lost lease did not cancel the operation")
	}
}

func TestRestoreClaimRespectsLiveLeaseAndTakesOverExpiredOne(t *testing.T) {
	e := archiveFixture(t)
	ctx := context.Background()
	e.d.SweepVolumeArchives(ctx)
	a := e.archive(t)
	a.State, a.Claim, a.Lease = archiveStateRestoring, "other", time.Now().Add(time.Hour)
	e.setArchive(t, a)
	if err := e.d.requireVolume(ctx, "keep"); !errors.Is(err, ErrVolumeRestoring) {
		t.Fatal(err)
	}
	if e.archive(t).Claim != "other" {
		t.Fatal("live claim was taken over")
	}
	a.Lease = time.Now().Add(-time.Second)
	e.setArchive(t, a)
	if err := e.d.Start(ctx, "keep"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "takeover restore", func() bool { r := e.dep(t).Spec.Replicas; return e.archive(t) == nil && *r == 1 })
}

func TestSweepResumesAbandonedRestore(t *testing.T) {
	e := archiveFixture(t)
	ctx := context.Background()
	e.d.SweepVolumeArchives(ctx)
	a := e.archive(t)
	a.State, a.Claim, a.Lease, a.StartAfter = archiveStateRestoring, "live", time.Now().Add(time.Hour), true
	e.setArchive(t, a)
	e.d.SweepVolumeArchives(ctx)
	if got := e.archive(t); got == nil || got.Claim != "live" {
		t.Fatalf("sweep took over a live restore: %+v", got)
	}
	// The gateway that owned the restore died; its lease lapsed.
	a.Claim, a.Lease = "dead", time.Now().Add(-time.Second)
	e.setArchive(t, a)
	e.d.SweepVolumeArchives(ctx)
	waitFor(t, "resumed restore", func() bool { r := e.dep(t).Spec.Replicas; return e.archive(t) == nil && *r == 1 })
}

func TestWaitVolumeRestored(t *testing.T) {
	t.Run("no archive", func(t *testing.T) {
		e := archiveFixture(t)
		if started, err := e.d.WaitVolumeRestored(context.Background(), "keep"); err != nil || !started {
			t.Fatalf("started=%v err=%v", started, err)
		}
	})
	t.Run("restore completes", func(t *testing.T) {
		e := archiveFixture(t)
		e.d.SweepVolumeArchives(context.Background())
		e.exec.gate = make(chan struct{})
		if err := e.d.Start(context.Background(), "keep"); err != nil {
			t.Fatal(err)
		}
		type result struct {
			started bool
			err     error
		}
		done := make(chan result, 1)
		go func() {
			started, err := e.d.WaitVolumeRestored(context.Background(), "keep")
			done <- result{started, err}
		}()
		select {
		case r := <-done:
			t.Fatalf("returned before the restore finished: %+v", r)
		case <-time.After(20 * time.Millisecond):
		}
		close(e.exec.gate)
		if r := <-done; r.err != nil || !r.started {
			t.Fatalf("%+v", r)
		}
		if r := e.dep(t).Spec.Replicas; *r != 1 {
			t.Fatalf("replicas=%d after restore", *r)
		}
	})
	t.Run("stop withdraws the start", func(t *testing.T) {
		e := archiveFixture(t)
		e.d.SweepVolumeArchives(context.Background())
		e.exec.gate = make(chan struct{})
		defer close(e.exec.gate)
		if err := e.d.Start(context.Background(), "keep"); err != nil {
			t.Fatal(err)
		}
		if err := e.d.scale(context.Background(), "keep", 0); err != nil {
			t.Fatal(err)
		}
		if started, err := e.d.WaitVolumeRestored(context.Background(), "keep"); err != nil || started {
			t.Fatalf("started=%v err=%v", started, err)
		}
	})
	t.Run("restore fails", func(t *testing.T) {
		e := archiveFixture(t)
		e.d.SweepVolumeArchives(context.Background())
		e.exec.setFail("extract", errors.New("tar: no space"))
		if err := e.d.Start(context.Background(), "keep"); err != nil {
			t.Fatal(err)
		}
		if _, err := e.d.WaitVolumeRestored(context.Background(), "keep"); err == nil {
			t.Fatal("failed restore reported as done")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		e := archiveFixture(t)
		e.d.SweepVolumeArchives(context.Background())
		a := e.archive(t)
		a.State, a.Claim, a.Lease, a.StartAfter = archiveStateRestoring, "live", time.Now().Add(time.Hour), true
		e.setArchive(t, a)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := e.d.WaitVolumeRestored(ctx, "keep"); !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("missing deployment and corrupt record", func(t *testing.T) {
		e := archiveFixture(t)
		dep := e.dep(t)
		dep.Annotations[archiveAnnotation] = "{"
		if _, err := e.cs.AppsV1().Deployments("sandboxes").Update(context.Background(), dep, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.d.WaitVolumeRestored(context.Background(), "keep"); err == nil {
			t.Fatal("corrupt record accepted")
		}
		if _, err := e.d.WaitVolumeRestored(context.Background(), "gone"); !apierrors.IsNotFound(err) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestArchivedVolumeWithoutConfiguredStoreIsAnError(t *testing.T) {
	e := archiveFixture(t)
	e.setArchive(t, &volumeArchive{State: archiveStateArchived, Key: "k", Storage: "25Gi"})
	e.d.exec = nil
	if err := e.d.Start(context.Background(), "keep"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("start of archived volume without store: %v", err)
	}
	if ok, err := e.d.volumeArchived(context.Background(), "keep"); !ok || err != nil {
		t.Fatalf("volumeArchived=%v err=%v", ok, err)
	}
}

func TestMutateDeploymentRetriesConflicts(t *testing.T) {
	e := archiveFixture(t)
	conflicts := 2
	e.cs.PrependReactor("update", "deployments", func(action kt.Action) (bool, runtime.Object, error) {
		if conflicts > 0 {
			conflicts--
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "deployments"}, "sbx-keep", errors.New("stale"))
		}
		return false, nil, nil
	})
	if _, err := e.d.mutateDeployment(context.Background(), "keep", func(dep *appsv1.Deployment) (bool, error) {
		dep.Annotations["x"] = "y"
		return true, nil
	}); err != nil || e.dep(t).Annotations["x"] != "y" {
		t.Fatalf("conflict not retried: %v", err)
	}
	e.cs.PrependReactor("update", "deployments", func(action kt.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "deployments"}, "sbx-keep", errors.New("stale"))
	})
	if _, err := e.d.mutateDeployment(context.Background(), "keep", func(*appsv1.Deployment) (bool, error) { return true, nil }); err == nil {
		t.Fatal("endless conflicts must fail")
	}
}

func TestRunVolumeArchiver(t *testing.T) {
	e := archiveFixture(t)
	e.d.opts.VolumeArchive.Interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.d.RunVolumeArchiver(ctx); close(done) }()
	waitFor(t, "archive", func() bool { a := e.archive(t); return a != nil && a.State == archiveStateArchived })
	cancel()
	<-done
	// Disabled archivers return immediately.
	testDriver(t, false).RunVolumeArchiver(context.Background())
	e.cs.PrependReactor("list", "deployments", func(kt.Action) (bool, runtime.Object, error) { return true, nil, errors.New("list failed") })
	e.d.SweepVolumeArchives(context.Background())
}

func TestHelperPodContract(t *testing.T) {
	e := archiveFixture(t)
	e.d.opts.ImagePullSecret = "pull"
	e.d.opts.VolumeArchive.HelperImage = "tools:tar"
	ctx := context.Background()
	stale := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sbx-keep-volume", Namespace: "sandboxes"}}
	if _, err := e.cs.CoreV1().Pods("sandboxes").Create(ctx, stale, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	name, err := e.d.startHelper(ctx, "keep", true)
	if err != nil {
		t.Fatal(err)
	}
	pod, _ := e.cs.CoreV1().Pods("sandboxes").Get(ctx, name, metav1.GetOptions{})
	c := pod.Spec.Containers[0]
	if c.Image != "tools:tar" || !c.VolumeMounts[0].ReadOnly || c.VolumeMounts[0].SubPath != "" || pod.Labels["sandbox-gateway.io/id"] != "" ||
		pod.Labels[volumeForLabel] != "keep" || pod.Spec.ImagePullSecrets[0].Name != "pull" || !*c.SecurityContext.Privileged {
		t.Fatalf("helper pod %+v", pod)
	}
	// Helper pods never make a stopped sandbox look running.
	if st, err := e.d.Status(ctx, "keep"); err != nil || st != driver.StatusStopped {
		t.Fatalf("status with helper=%s err=%v", st, err)
	}
}

func TestHelperWaitAndDeleteFailures(t *testing.T) {
	defer func(d time.Duration) { helperReadyTimeout = d }(helperReadyTimeout)
	helperReadyTimeout = 20 * time.Millisecond
	e := archiveFixture(t)
	e.helperPhase = corev1.PodPending
	if _, err := e.d.startHelper(context.Background(), "keep", false); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("pending helper accepted: %v", err)
	}
	e.cs.PrependReactor("delete", "pods", func(kt.Action) (bool, runtime.Object, error) { return true, nil, errors.New("delete denied") })
	if err := e.d.deleteHelper(context.Background(), "keep"); err == nil {
		t.Fatal("helper delete failure hidden")
	}
	e2 := archiveFixture(t)
	e2.cs.PrependReactor("create", "pods", func(kt.Action) (bool, runtime.Object, error) { return true, nil, errors.New("quota") })
	if _, err := e2.d.startHelper(context.Background(), "keep", false); err == nil {
		t.Fatal("helper create failure hidden")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e3 := archiveFixture(t)
	e3.cs.PrependReactor("delete", "pods", func(kt.Action) (bool, runtime.Object, error) { return true, nil, nil })
	if _, err := e3.cs.CoreV1().Pods("sandboxes").Create(context.Background(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sbx-keep-volume", Namespace: "sandboxes"}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := e3.d.deleteHelper(ctx, "keep"); err == nil {
		t.Fatal("cancelled helper wait succeeded")
	}
}

func TestReadArchiveRejectsInvalidRecords(t *testing.T) {
	for _, raw := range []string{"{", `{"state":"archived","storage":"1Gi"}`, `{"state":"weird","key":"k","storage":"1Gi"}`, `{"state":"archived","key":"k"}`} {
		dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "d", Annotations: map[string]string{archiveAnnotation: raw}}}
		if _, err := readArchive(dep); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	dep := &appsv1.Deployment{}
	writeArchive(dep, &volumeArchive{State: archiveStateArchived, Key: "k", Storage: "1Gi"})
	writeArchive(dep, nil)
	if dep.Annotations[archiveAnnotation] != "" {
		t.Fatal("archive record not removed")
	}
}

func TestTailBufferKeepsLastBytes(t *testing.T) {
	b := &tailBuffer{limit: 4}
	_, _ = fmt.Fprint(b, "abcdef")
	_, _ = fmt.Fprint(b, "gh")
	if b.String() != "efgh" {
		t.Fatalf("tail=%q", b.String())
	}
}

func TestArchiveTimeoutDefault(t *testing.T) {
	if testDriver(t, false).archiveTimeout() != 2*time.Hour {
		t.Fatal("default archive timeout")
	}
}

func TestPodExecutorReportsStreamFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "exec denied", http.StatusForbidden)
	}))
	defer srv.Close()
	cfg := &rest.Config{Host: srv.URL}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	run := newPodExecutor(cfg, cs)
	if err := run(context.Background(), "sandboxes", "sbx-keep-volume", tarCreate, nil, io.Discard); err == nil || !strings.HasPrefix(err.Error(), "tar:") {
		t.Fatalf("exec failure hidden: %v", err)
	}
	bad := &rest.Config{Host: srv.URL, TLSClientConfig: rest.TLSClientConfig{CAFile: "/nonexistent/ca.pem"}}
	if err := newPodExecutor(bad, cs)(context.Background(), "sandboxes", "p", syncVolume, nil, io.Discard); err == nil {
		t.Fatal("invalid transport accepted")
	}
}

func TestVolumeRestoreInfrastructureFailures(t *testing.T) {
	cases := map[string]func(e *archiveEnv){
		"not gzip": func(e *archiveEnv) {
			e.store.set(func(m *memStore) {
				for k := range m.objects {
					m.objects[k] = bytes.Repeat([]byte("x"), len(m.objects[k]))
				}
			})
		},
		"size stat fails": func(e *archiveEnv) { e.store.set(func(m *memStore) { m.sizeErr = errors.New("head failed") }) },
		"pvc create fails": func(e *archiveEnv) {
			e.cs.PrependReactor("create", "persistentvolumeclaims", func(kt.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("quota")
			})
		},
		"pvc read fails": func(e *archiveEnv) {
			e.cs.PrependReactor("get", "persistentvolumeclaims", func(kt.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("apiserver down")
			})
		},
		"helper fails": func(e *archiveEnv) { e.helperPhase = corev1.PodFailed },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := archiveFixture(t)
			e.d.SweepVolumeArchives(context.Background())
			setup(e)
			if err := e.d.Start(context.Background(), "keep"); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "restore failure", func() bool { a := e.archive(t); return a != nil && a.State == archiveStateArchived })
			if e.store.count() != 1 || *e.dep(t).Spec.Replicas != 0 {
				t.Fatal("failed restore consumed the archive or started the sandbox")
			}
		})
	}
}

func TestArchiveGuardsOnRestoringAndCorruptRecords(t *testing.T) {
	e := archiveFixture(t)
	ctx := context.Background()
	e.d.SweepVolumeArchives(ctx)
	e.exec.gate = make(chan struct{})
	defer close(e.exec.gate)
	if err := e.d.requireVolume(ctx, "keep"); !errors.Is(err, ErrVolumeRestoring) {
		t.Fatal(err)
	}
	if err := e.d.cancelArchive(ctx, "keep"); !errors.Is(err, ErrVolumeRestoring) {
		t.Fatalf("cancel of a restore: %v", err)
	}
	if ok, err := e.d.volumeArchived(ctx, "missing"); ok || err == nil {
		t.Fatal("missing deployment reported as archived")
	}

	c := archiveFixture(t)
	dep := c.dep(t)
	dep.Annotations[archiveAnnotation] = "{"
	if _, err := c.cs.AppsV1().Deployments("sandboxes").Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.d.Start(ctx, "keep"); err == nil {
		t.Fatal("start accepted a corrupt archive record")
	}
	if err := c.d.requireVolume(ctx, "keep"); err == nil {
		t.Fatal("volume guard accepted a corrupt archive record")
	}
	if err := c.d.cancelArchive(ctx, "keep"); err == nil {
		t.Fatal("cancel accepted a corrupt archive record")
	}
	if err := c.d.mutateClaim(ctx, "keep", "c", func(*appsv1.Deployment, *volumeArchive) error { return nil }); err == nil {
		t.Fatal("claim mutation accepted a corrupt archive record")
	}
	if err := c.d.cancelArchive(ctx, "missing"); err == nil {
		t.Fatal("cancel of a missing deployment succeeded")
	}
	c.d.stampLegacyStop(ctx, "missing")
}
