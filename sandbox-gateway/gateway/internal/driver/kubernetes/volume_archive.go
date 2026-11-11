package kubernetes

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"time"

	"github.com/rs/zerolog/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	archiveAnnotation   = "sandbox-gateway.io/volume-archive"
	stoppedAtAnnotation = "sandbox-gateway.io/stopped-at"
	volumeForLabel      = "sandbox-gateway.io/volume-for"
	volumeContainer     = "volume"
	volumeMountPath     = "/data"

	archiveStateArchiving = "archiving"
	archiveStateArchived  = "archived"
	archiveStateRestoring = "restoring"
)

// ErrVolumeRestoring reports that the data volume is archived in object
// storage and a restore has been requested. Retry after it completes.
var ErrVolumeRestoring = errors.New("sandbox volume is archived; restore in progress, retry when it completes")

var errArchiveLost = errors.New("volume archive ownership changed")

// Overridable in tests.
var (
	archiveLeaseTTL     = 10 * time.Minute
	archivePollInterval = 2 * time.Second
	helperReadyTimeout  = 10 * time.Minute
)

// ArchiveStore holds whole-volume archives outside the cluster.
type ArchiveStore interface {
	Put(ctx context.Context, key string, body io.Reader) (int64, error)
	Size(ctx context.Context, key string) (int64, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// VolumeArchiveOptions enables releasing data PVCs of stopped sandboxes.
// A nil Store disables archiving.
type VolumeArchiveOptions struct {
	Store ArchiveStore
	// After is how long a sandbox stays stopped before its PVC is archived.
	After time.Duration
	// Interval is the sweep period.
	Interval time.Duration
	// Timeout bounds one archive or restore.
	Timeout time.Duration
	// HelperImage runs tar against the PVC; empty uses the sandbox image.
	HelperImage string
}

// podExecutor runs command in the volume helper container, streaming stdin
// and stdout. A non-zero exit is an error.
type podExecutor func(ctx context.Context, namespace, pod string, command []string, stdin io.Reader, stdout io.Writer) error

// volumeArchive is the state recorded on the Deployment. While present, the
// object in the archive store (not the PVC) is the authoritative copy once
// State is archived or restoring.
type volumeArchive struct {
	State        string    `json:"state"`
	Key          string    `json:"key"`
	SourcePVCUID string    `json:"sourcePvcUid"`
	Storage      string    `json:"storage"`
	StorageClass string    `json:"storageClass,omitempty"`
	Bytes        int64     `json:"bytes,omitempty"`
	SHA256       string    `json:"sha256,omitempty"`
	Claim        string    `json:"claim,omitempty"`
	Lease        time.Time `json:"lease,omitempty"`
	StartAfter   bool      `json:"startAfter,omitempty"`
}

var (
	tarCreate  = []string{"tar", "--create", "--file=-", "--numeric-owner", "--sparse", "--xattrs", "--xattrs-include=*", "--directory=" + volumeMountPath, "."}
	tarExtract = []string{"tar", "--extract", "--file=-", "--numeric-owner", "--same-owner", "--same-permissions", "--xattrs", "--xattrs-include=*", "--directory=" + volumeMountPath}
	syncVolume = []string{"sync"}
)

func readArchive(dep *appsv1.Deployment) (*volumeArchive, error) {
	raw := dep.Annotations[archiveAnnotation]
	if raw == "" {
		return nil, nil
	}
	var a volumeArchive
	if err := json.Unmarshal([]byte(raw), &a); err != nil || a.Key == "" || a.Storage == "" ||
		(a.State != archiveStateArchiving && a.State != archiveStateArchived && a.State != archiveStateRestoring) {
		return nil, fmt.Errorf("invalid %s annotation on %s", archiveAnnotation, dep.Name)
	}
	return &a, nil
}

func writeArchive(dep *appsv1.Deployment, a *volumeArchive) {
	if a == nil {
		delete(dep.Annotations, archiveAnnotation)
		return
	}
	if dep.Annotations == nil {
		dep.Annotations = map[string]string{}
	}
	encoded, _ := json.Marshal(a)
	dep.Annotations[archiveAnnotation] = string(encoded)
}

func stampStopped(dep *appsv1.Deployment, at time.Time) {
	if dep.Annotations == nil {
		dep.Annotations = map[string]string{}
	}
	dep.Annotations[stoppedAtAnnotation] = at.UTC().Format(time.RFC3339)
}

func newClaim() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (d *Driver) helperName(id string) string { return d.opts.NamePrefix + id + "-volume" }

func (d *Driver) archiveEnabled() bool { return d.opts.VolumeArchive.Store != nil && d.exec != nil }

func (d *Driver) archiveTimeout() time.Duration {
	if t := d.opts.VolumeArchive.Timeout; t > 0 {
		return t
	}
	return 2 * time.Hour
}

// mutateDeployment applies fn to a fresh copy and writes it with the read
// resourceVersion, retrying on conflicts. fn returns false to skip the write.
func (d *Driver) mutateDeployment(ctx context.Context, id string, fn func(*appsv1.Deployment) (bool, error)) (*appsv1.Deployment, error) {
	deps := d.cs.AppsV1().Deployments(d.opts.Namespace)
	for attempt := 0; attempt < 8; attempt++ {
		dep, err := deps.Get(ctx, d.resourceName(id), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		write, err := fn(dep)
		if err != nil || !write {
			return dep, err
		}
		updated, err := deps.Update(ctx, dep, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			continue
		}
		return updated, err
	}
	return nil, fmt.Errorf("deployment %s kept changing", d.resourceName(id))
}

// mutateClaim changes the archive only while claim still owns it. fn may
// abort the write by returning an error.
func (d *Driver) mutateClaim(ctx context.Context, id, claim string, fn func(*appsv1.Deployment, *volumeArchive) error) error {
	_, err := d.mutateDeployment(ctx, id, func(dep *appsv1.Deployment) (bool, error) {
		a, err := readArchive(dep)
		if err != nil {
			return false, err
		}
		if a == nil || a.Claim != claim {
			return false, errArchiveLost
		}
		if err := fn(dep, a); err != nil {
			return false, err
		}
		return true, nil
	})
	return err
}

// holdLease renews the claim until ctx ends. Losing it cancels the operation.
func (d *Driver) holdLease(ctx context.Context, cancel context.CancelFunc, id, claim string) {
	ticker := time.NewTicker(archiveLeaseTTL / 4)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := d.mutateClaim(ctx, id, claim, func(dep *appsv1.Deployment, a *volumeArchive) error {
				a.Lease = time.Now().Add(archiveLeaseTTL)
				writeArchive(dep, a)
				return nil
			})
			if errors.Is(err, errArchiveLost) || apierrors.IsNotFound(err) {
				log.Warn().Str("sandbox_id", id).Msg("volume archive lease lost; cancelling")
				cancel()
				return
			}
		}
	}
}

// prepareStart resolves an archived volume before Start scales up. It returns
// true when a background restore will scale the Deployment itself.
func (d *Driver) prepareStart(ctx context.Context, id string) (bool, error) {
	dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	a, err := readArchive(dep)
	if err != nil || a == nil {
		return false, err
	}
	if a.State == archiveStateArchiving {
		return false, d.cancelArchive(ctx, id)
	}
	return d.requestRestore(ctx, id, true)
}

// requireVolume is the guard for operations that need the PVC in place. An
// archived volume starts restoring (without scaling) and ErrVolumeRestoring
// is returned; a running archive is cancelled so the PVC stays.
func (d *Driver) requireVolume(ctx context.Context, id string) error {
	dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		return err
	}
	a, err := readArchive(dep)
	if err != nil || a == nil {
		return err
	}
	if a.State == archiveStateArchiving {
		return d.cancelArchive(ctx, id)
	}
	pending, err := d.requestRestore(ctx, id, false)
	if err != nil || !pending {
		return err
	}
	return ErrVolumeRestoring
}

// volumeArchived reports, without side effects, whether the PVC is absent
// because its data lives in the archive store.
func (d *Driver) volumeArchived(ctx context.Context, id string) (bool, error) {
	dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	a, err := readArchive(dep)
	return a != nil && a.State != archiveStateArchiving, err
}

// WaitVolumeRestored blocks while a start waits on its volume restore. It
// returns false when a stop withdrew the start, and an error when the restore
// gave up.
func (d *Driver) WaitVolumeRestored(ctx context.Context, id string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, d.archiveTimeout()+archiveLeaseTTL)
	defer cancel()
	for {
		dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		a, err := readArchive(dep)
		if err != nil {
			return false, err
		}
		switch {
		case a == nil || a.State == archiveStateArchiving:
			return true, nil
		case a.State != archiveStateRestoring:
			return false, errors.New("volume restore did not complete; archive retained")
		case !a.StartAfter:
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("wait volume restore: %w", ctx.Err())
		case <-time.After(archivePollInterval):
		}
	}
}

// cancelArchive drops an in-flight archive. The archiver then fails its
// claim check and removes its object; the PVC is never deleted.
func (d *Driver) cancelArchive(ctx context.Context, id string) error {
	_, err := d.mutateDeployment(ctx, id, func(dep *appsv1.Deployment) (bool, error) {
		a, err := readArchive(dep)
		if err != nil {
			return false, err
		}
		if a == nil {
			return false, nil
		}
		if a.State != archiveStateArchiving {
			return false, ErrVolumeRestoring
		}
		writeArchive(dep, nil)
		stampStopped(dep, time.Now())
		return true, nil
	})
	if err != nil {
		return err
	}
	return d.deleteHelper(ctx, id)
}

// requestRestore claims the restore unless a live claim exists, and records
// whether the Deployment should be started when the restore completes. It
// returns false when the volume is already back in place.
func (d *Driver) requestRestore(ctx context.Context, id string, start bool) (bool, error) {
	if !d.archiveEnabled() {
		return false, fmt.Errorf("sandbox %s volume is archived but volume archiving is not configured", id)
	}
	var claimed *volumeArchive
	pending := false
	_, err := d.mutateDeployment(ctx, id, func(dep *appsv1.Deployment) (bool, error) {
		claimed, pending = nil, false
		a, err := readArchive(dep)
		if err != nil || a == nil {
			return false, err
		}
		if a.State == archiveStateArchiving {
			return false, errArchiveLost
		}
		pending = true
		if a.State == archiveStateRestoring && time.Now().Before(a.Lease) {
			if !start || a.StartAfter {
				return false, nil
			}
			a.StartAfter = true
			writeArchive(dep, a)
			return true, nil
		}
		a.State = archiveStateRestoring
		a.Claim = newClaim()
		a.Lease = time.Now().Add(archiveLeaseTTL)
		a.StartAfter = a.StartAfter || start
		writeArchive(dep, a)
		claimed = a
		return true, nil
	})
	if err != nil {
		return false, err
	}
	if claimed != nil {
		go d.restore(id, *claimed)
	}
	return pending, nil
}

func (d *Driver) restore(id string, a volumeArchive) {
	ctx, cancel := context.WithTimeout(context.Background(), d.archiveTimeout())
	defer cancel()
	go d.holdLease(ctx, cancel, id, a.Claim)
	started := time.Now()
	err := d.restoreVolume(ctx, id, a)
	if err == nil {
		log.Info().Str("sandbox_id", id).Int64("bytes", a.Bytes).Dur("cost", time.Since(started)).Msg("volume restored from archive")
		return
	}
	log.Error().Err(err).Str("sandbox_id", id).Msg("volume restore failed; archive retained")
	cleanup, done := context.WithTimeout(context.Background(), 3*time.Minute)
	defer done()
	if err := d.deleteHelper(cleanup, id); err != nil {
		log.Warn().Err(err).Str("sandbox_id", id).Msg("volume restore: delete helper failed")
	}
	releaseErr := d.mutateClaim(cleanup, id, a.Claim, func(dep *appsv1.Deployment, cur *volumeArchive) error {
		cur.State, cur.Claim, cur.Lease, cur.StartAfter = archiveStateArchived, "", time.Time{}, false
		writeArchive(dep, cur)
		return nil
	})
	if releaseErr != nil {
		log.Warn().Err(releaseErr).Str("sandbox_id", id).Msg("volume restore: release claim failed")
	}
}

// restoreVolume recreates the PVC from the archive. Any PVC still present is
// either the already-archived source or a partial earlier restore; both are
// superseded by the verified archive and are deleted first.
func (d *Driver) restoreVolume(ctx context.Context, id string, a volumeArchive) error {
	size, err := d.opts.VolumeArchive.Store.Size(ctx, a.Key)
	if err != nil {
		return err
	}
	if size != a.Bytes {
		return fmt.Errorf("volume archive %s has %d bytes, expected %d", a.Key, size, a.Bytes)
	}
	pvcs := d.cs.CoreV1().PersistentVolumeClaims(d.opts.Namespace)
	if old, err := pvcs.Get(ctx, d.pvcName(id), metav1.GetOptions{}); err == nil {
		if old.DeletionTimestamp == nil {
			uid := old.UID
			if err := pvcs.Delete(ctx, old.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); ignoreNotFound(err) != nil {
				return err
			}
		}
		if err := d.waitPVCGone(ctx, old.Name, 5*time.Minute); err != nil {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	quantity, err := resource.ParseQuantity(a.Storage)
	if err != nil {
		return err
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: d.pvcName(id), Namespace: d.opts.Namespace, Labels: d.selector(id), Annotations: copyStringMap(d.opts.PVCAnnotations)},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: quantity}},
		},
	}
	if a.StorageClass != "" {
		class := a.StorageClass
		pvc.Spec.StorageClassName = &class
	}
	if _, err := pvcs.Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create restored pvc: %w", err)
	}
	pod, err := d.startHelper(ctx, id, false)
	if err != nil {
		return err
	}
	if err := d.extractArchive(ctx, pod, a); err != nil {
		return err
	}
	if err := d.deleteHelper(ctx, id); err != nil {
		return err
	}
	var start bool
	err = d.mutateClaim(ctx, id, a.Claim, func(dep *appsv1.Deployment, cur *volumeArchive) error {
		writeArchive(dep, nil)
		stampStopped(dep, time.Now())
		start = cur.StartAfter && !coldPending(dep)
		if start {
			one := int32(1)
			dep.Spec.Replicas = &one
			delete(dep.Annotations, stoppedAtAnnotation)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// The lease holder cancels ctx once the claim is gone; finish with a new one.
	post, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if start {
		if err := d.ConvergePublishSurface(post, id); err != nil {
			log.Warn().Err(err).Str("sandbox_id", id).Msg("volume restore: converge publish surface failed")
		}
	}
	if err := d.opts.VolumeArchive.Store.Delete(post, a.Key); err != nil {
		log.Warn().Err(err).Str("sandbox_id", id).Str("key", a.Key).Msg("volume restore: delete consumed archive failed")
	}
	return nil
}

func (d *Driver) extractArchive(ctx context.Context, pod string, a volumeArchive) error {
	body, err := d.opts.VolumeArchive.Store.Get(ctx, a.Key)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	digest := sha256.New()
	counter := &countingWriter{}
	tee := io.TeeReader(body, io.MultiWriter(digest, counter))
	gz, err := gzip.NewReader(tee)
	if err != nil {
		return fmt.Errorf("open volume archive: %w", err)
	}
	if err := d.exec(ctx, d.opts.Namespace, pod, tarExtract, gz, io.Discard); err != nil {
		return fmt.Errorf("extract volume archive: %w", err)
	}
	// Drain so the gzip trailer CRC and the whole-object digest are checked.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return fmt.Errorf("verify volume archive: %w", err)
	}
	if _, err := io.Copy(io.Discard, tee); err != nil {
		return fmt.Errorf("verify volume archive: %w", err)
	}
	if counter.n != a.Bytes || hex.EncodeToString(digest.Sum(nil)) != a.SHA256 {
		return fmt.Errorf("volume archive %s does not match its recorded size and digest", a.Key)
	}
	return d.exec(ctx, d.opts.Namespace, pod, syncVolume, nil, io.Discard)
}

// RunVolumeArchiver sweeps stopped sandboxes until ctx ends.
func (d *Driver) RunVolumeArchiver(ctx context.Context) {
	if !d.archiveEnabled() {
		return
	}
	interval := d.opts.VolumeArchive.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	log.Info().Dur("after", d.opts.VolumeArchive.After).Dur("interval", interval).Msg("volume archiver started")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		d.SweepVolumeArchives(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// SweepVolumeArchives archives every sandbox stopped longer than After,
// abandons archives whose owner stopped renewing them, and resumes restores
// whose owner stopped renewing them.
func (d *Driver) SweepVolumeArchives(ctx context.Context) {
	deps, err := d.cs.AppsV1().Deployments(d.opts.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/managed-by=" + managedByLabel})
	if err != nil {
		log.Warn().Err(err).Msg("volume archiver: list deployments failed")
		return
	}
	for i := range deps.Items {
		if ctx.Err() != nil {
			return
		}
		dep := &deps.Items[i]
		id := dep.Labels["sandbox-gateway.io/id"]
		if id == "" || dep.Name != d.resourceName(id) {
			continue
		}
		a, err := readArchive(dep)
		if err != nil {
			log.Warn().Err(err).Str("sandbox_id", id).Msg("volume archiver: skip")
			continue
		}
		if a != nil {
			switch {
			case !time.Now().After(a.Lease):
			case a.State == archiveStateArchiving:
				d.discardArchive(ctx, id, *a)
			case a.State == archiveStateRestoring:
				if _, err := d.requestRestore(ctx, id, false); err != nil {
					log.Warn().Err(err).Str("sandbox_id", id).Msg("volume archiver: resume restore failed")
				}
			}
			continue
		}
		if scaledToZero(dep) && dep.Annotations[stoppedAtAnnotation] == "" {
			d.stampLegacyStop(ctx, id)
			continue
		}
		if !d.archiveDue(dep) {
			continue
		}
		if err := d.archiveVolume(ctx, id); err != nil {
			log.Warn().Err(err).Str("sandbox_id", id).Msg("volume archive failed; pvc retained")
		}
	}
}

func scaledToZero(dep *appsv1.Deployment) bool {
	return dep.DeletionTimestamp == nil && dep.Spec.Replicas != nil && *dep.Spec.Replicas == 0
}

// archiveDue reports whether dep has stayed stopped for at least After.
func (d *Driver) archiveDue(dep *appsv1.Deployment) bool {
	if !scaledToZero(dep) || coldPending(dep) {
		return false
	}
	at, err := time.Parse(time.RFC3339, dep.Annotations[stoppedAtAnnotation])
	return err == nil && time.Since(at) >= d.opts.VolumeArchive.After
}

// stampLegacyStop starts the clock for sandboxes stopped before stop time was
// recorded, so they are archived only after a full After period.
func (d *Driver) stampLegacyStop(ctx context.Context, id string) {
	_, err := d.mutateDeployment(ctx, id, func(cur *appsv1.Deployment) (bool, error) {
		if !scaledToZero(cur) || cur.Annotations[stoppedAtAnnotation] != "" {
			return false, nil
		}
		stampStopped(cur, time.Now())
		return true, nil
	})
	if err != nil {
		log.Warn().Err(err).Str("sandbox_id", id).Msg("volume archiver: record stop time failed")
	}
}

// archiveVolume copies the stopped sandbox's PVC to the archive store and
// deletes the PVC only after the upload is verified and recorded.
func (d *Driver) archiveVolume(ctx context.Context, id string) error {
	dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		return err
	}
	stopped, err := d.observedStopped(ctx, id, dep)
	if err != nil || !stopped {
		return err
	}
	pvc, err := d.cs.CoreV1().PersistentVolumeClaims(d.opts.Namespace).Get(ctx, d.pvcName(id), metav1.GetOptions{})
	if err != nil {
		return err
	}
	if pvc.DeletionTimestamp != nil || pvc.UID == "" || pvc.Status.Phase != corev1.ClaimBound || !d.releaseLabels(id, pvc.Labels) {
		return fmt.Errorf("pvc %s is not a bound sandbox volume", pvc.Name)
	}
	storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	a := volumeArchive{
		State:        archiveStateArchiving,
		Key:          fmt.Sprintf("%s/%s/%s-%d.tar.gz", d.opts.Namespace, id, pvc.UID, time.Now().UnixNano()),
		SourcePVCUID: string(pvc.UID),
		Storage:      storage.String(),
		Claim:        newClaim(),
		Lease:        time.Now().Add(archiveLeaseTTL),
	}
	if pvc.Spec.StorageClassName != nil {
		a.StorageClass = *pvc.Spec.StorageClassName
	}
	_, err = d.mutateDeployment(ctx, id, func(cur *appsv1.Deployment) (bool, error) {
		if cur.UID != dep.UID || cur.Annotations[archiveAnnotation] != "" || !d.archiveDue(cur) {
			return false, errArchiveLost
		}
		writeArchive(cur, &a)
		return true, nil
	})
	if err != nil {
		return err
	}
	opCtx, cancel := context.WithTimeout(ctx, d.archiveTimeout())
	defer cancel()
	go d.holdLease(opCtx, cancel, id, a.Claim)
	started := time.Now()
	if err := d.captureVolume(opCtx, id, &a); err != nil {
		d.discardArchive(context.WithoutCancel(ctx), id, a)
		return err
	}
	err = d.mutateClaim(opCtx, id, a.Claim, func(dep *appsv1.Deployment, cur *volumeArchive) error {
		if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 0 || coldPending(dep) || cur.State != archiveStateArchiving {
			return errArchiveLost
		}
		cur.State, cur.Bytes, cur.SHA256, cur.Claim, cur.Lease = archiveStateArchived, a.Bytes, a.SHA256, "", time.Time{}
		writeArchive(dep, cur)
		return nil
	})
	if err != nil {
		d.discardArchive(context.WithoutCancel(ctx), id, a)
		return err
	}
	cancel()
	uid := types.UID(a.SourcePVCUID)
	err = d.cs.CoreV1().PersistentVolumeClaims(d.opts.Namespace).Delete(ctx, d.pvcName(id), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if ignoreNotFound(err) != nil && !apierrors.IsConflict(err) {
		// The archive is authoritative; restore deletes a leftover source PVC.
		log.Warn().Err(err).Str("sandbox_id", id).Msg("volume archived but pvc delete failed")
	}
	log.Info().Str("sandbox_id", id).Int64("bytes", a.Bytes).Dur("cost", time.Since(started)).Msg("volume archived; pvc released")
	return nil
}

// discardArchive undoes a failed or superseded archive without touching the
// PVC. The object is deleted only once no annotation can reference it: a lost
// response may hide an archived commit that already made it authoritative.
func (d *Driver) discardArchive(parent context.Context, id string, a volumeArchive) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	if err := d.deleteHelper(ctx, id); err != nil {
		log.Warn().Err(err).Str("sandbox_id", id).Msg("volume archive: delete helper failed")
	}
	_, err := d.mutateDeployment(ctx, id, func(dep *appsv1.Deployment) (bool, error) {
		cur, err := readArchive(dep)
		if err != nil {
			return false, err
		}
		if cur == nil || cur.Key != a.Key {
			return false, nil
		}
		if cur.State != archiveStateArchiving || cur.Claim != a.Claim {
			return false, fmt.Errorf("archive %s is referenced as %s", a.Key, cur.State)
		}
		writeArchive(dep, nil)
		return true, nil
	})
	if err != nil && !apierrors.IsNotFound(err) {
		log.Warn().Err(err).Str("sandbox_id", id).Str("key", a.Key).Msg("volume archive: object retained")
		return
	}
	if err := d.opts.VolumeArchive.Store.Delete(ctx, a.Key); err != nil {
		log.Warn().Err(err).Str("sandbox_id", id).Str("key", a.Key).Msg("volume archive: delete partial archive failed")
	}
}

// captureVolume streams tar from the helper through gzip into the store and
// verifies the stored size.
func (d *Driver) captureVolume(ctx context.Context, id string, a *volumeArchive) error {
	pod, err := d.startHelper(ctx, id, true)
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	digest := sha256.New()
	counter := &countingWriter{}
	gz, _ := gzip.NewWriterLevel(io.MultiWriter(pw, digest, counter), gzip.BestSpeed)
	type putResult struct {
		n   int64
		err error
	}
	done := make(chan putResult, 1)
	go func() {
		n, err := d.opts.VolumeArchive.Store.Put(ctx, a.Key, pr)
		_ = pr.CloseWithError(errors.Join(err, errors.New("archive upload stopped")))
		done <- putResult{n, err}
	}()
	execErr := d.exec(ctx, d.opts.Namespace, pod, tarCreate, nil, gz)
	if execErr == nil {
		execErr = gz.Close()
	}
	_ = pw.CloseWithError(execErr)
	put := <-done
	if execErr != nil {
		return fmt.Errorf("archive volume: %w", execErr)
	}
	if put.err != nil {
		return put.err
	}
	if put.n != counter.n {
		return fmt.Errorf("archive upload stored %d of %d bytes", put.n, counter.n)
	}
	size, err := d.opts.VolumeArchive.Store.Size(ctx, a.Key)
	if err != nil {
		return err
	}
	if size != counter.n {
		return fmt.Errorf("archive object has %d bytes, expected %d", size, counter.n)
	}
	a.Bytes, a.SHA256 = counter.n, hexDigest(digest)
	return d.deleteHelper(ctx, id)
}

func hexDigest(h hash.Hash) string { return hex.EncodeToString(h.Sum(nil)) }

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// startHelper runs a pod that mounts the whole PVC so tar can read or write
// every subPath. It never carries the sandbox selector labels.
func (d *Driver) startHelper(ctx context.Context, id string, readOnly bool) (string, error) {
	image := d.opts.VolumeArchive.HelperImage
	if image == "" {
		dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		if len(dep.Spec.Template.Spec.Containers) == 0 {
			return "", fmt.Errorf("deployment %s has no container image", dep.Name)
		}
		image = dep.Spec.Template.Spec.Containers[0].Image
	}
	if err := d.deleteHelper(ctx, id); err != nil {
		return "", err
	}
	privileged := true
	grace := int64(1)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: d.helperName(id), Namespace: d.opts.Namespace,
			Labels: map[string]string{"app.kubernetes.io/managed-by": managedByLabel, volumeForLabel: id},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			TerminationGracePeriodSeconds: &grace,
			Containers: []corev1.Container{{
				Name:            volumeContainer,
				Image:           image,
				ImagePullPolicy: corev1.PullPolicy(d.opts.ImagePullPolicy),
				Command:         []string{"/bin/sh", "-c", "trap 'exit 0' TERM; sleep 86400 & wait"},
				// Privileged so tar can read and restore trusted.* xattrs and
				// device nodes that nested Docker keeps under /var/lib/docker.
				SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
				VolumeMounts:    []corev1.VolumeMount{{Name: "data", MountPath: volumeMountPath, ReadOnly: readOnly}},
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi"),
				}},
			}},
			Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: d.pvcName(id), ReadOnly: readOnly},
			}}},
		},
	}
	if d.opts.ImagePullSecret != "" {
		pod.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: d.opts.ImagePullSecret}}
	}
	if _, err := d.cs.CoreV1().Pods(d.opts.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return "", fmt.Errorf("create volume helper: %w", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, helperReadyTimeout)
	defer cancel()
	for {
		current, err := d.cs.CoreV1().Pods(d.opts.Namespace).Get(waitCtx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		switch current.Status.Phase {
		case corev1.PodRunning:
			return pod.Name, nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return "", fmt.Errorf("volume helper %s exited before use", pod.Name)
		}
		select {
		case <-waitCtx.Done():
			return "", fmt.Errorf("volume helper %s not running: %w", pod.Name, waitCtx.Err())
		case <-time.After(archivePollInterval):
		}
	}
}

func (d *Driver) deleteHelper(ctx context.Context, id string) error {
	pods := d.cs.CoreV1().Pods(d.opts.Namespace)
	name := d.helperName(id)
	if err := pods.Delete(ctx, name, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		_, err := pods.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("volume helper %s still present", name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(archivePollInterval):
		}
	}
}

// dropArchive removes the stored archive and helper when the sandbox's data
// is being deleted anyway (Destroy or Reinstall without preserved data).
func (d *Driver) dropArchive(ctx context.Context, id string) {
	dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		return
	}
	a, err := readArchive(dep)
	if err != nil || a == nil {
		return
	}
	if err := d.deleteHelper(ctx, id); err != nil {
		log.Warn().Err(err).Str("sandbox_id", id).Msg("drop volume archive: delete helper failed")
	}
	if d.opts.VolumeArchive.Store == nil {
		log.Warn().Str("sandbox_id", id).Str("key", a.Key).Msg("drop volume archive: store not configured; object retained")
		return
	}
	if err := d.opts.VolumeArchive.Store.Delete(ctx, a.Key); err != nil {
		log.Warn().Err(err).Str("sandbox_id", id).Str("key", a.Key).Msg("drop volume archive: delete object failed")
	}
}
