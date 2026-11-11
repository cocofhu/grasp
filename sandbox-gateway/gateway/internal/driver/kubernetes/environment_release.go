package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"sandbox-gateway/internal/driver"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

func (d *Driver) PreviewEnvironmentRelease(ctx context.Context, id string) (driver.EnvironmentReleasePlan, error) {
	p := driver.EnvironmentReleasePlan{Version: driver.EnvironmentReleaseVersion, Resource: id, Namespace: d.opts.Namespace}
	// The release plan names the live PVC, so an archived volume is restored
	// first and the caller retries.
	if err := d.requireVolume(ctx, id); err != nil {
		if errors.Is(err, ErrVolumeRestoring) {
			return p, errors.Join(driver.ErrEnvironmentReleasePending, err)
		}
		return p, err
	}
	dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		return p, err
	}
	if !d.releaseLabels(id, dep.Labels) {
		return p, driver.ErrEnvironmentReleaseConflict
	}
	stopped, err := d.observedStopped(ctx, id, dep)
	if err != nil {
		return p, err
	}
	if !stopped {
		return p, driver.ErrEnvironmentReleasePending
	}
	add := func(kind string, meta metav1.ObjectMeta, observe bool) {
		p.Objects = append(p.Objects, driver.EnvironmentReleaseObject{Kind: kind, Namespace: meta.Namespace, Name: meta.Name, UID: string(meta.UID), ObserveOnly: observe})
	}
	add("Deployment", dep.ObjectMeta, false)
	pvc, err := d.cs.CoreV1().PersistentVolumeClaims(d.opts.Namespace).Get(ctx, d.pvcName(id), metav1.GetOptions{})
	if err != nil {
		return p, err
	}
	if !d.releaseLabels(id, pvc.Labels) || pvc.Spec.VolumeName == "" || pvc.Status.Phase != corev1.ClaimBound {
		return p, driver.ErrEnvironmentReleaseUnsupported
	}
	pv, err := d.cs.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return p, err
	}
	ref := pv.Spec.ClaimRef
	if pv.Spec.PersistentVolumeReclaimPolicy != corev1.PersistentVolumeReclaimDelete || ref == nil || ref.UID != pvc.UID || ref.Name != pvc.Name || ref.Namespace != pvc.Namespace {
		return p, driver.ErrEnvironmentReleaseUnsupported
	}
	add("PersistentVolumeClaim", pvc.ObjectMeta, false)
	add("PersistentVolume", pv.ObjectMeta, true)
	for _, name := range []string{d.resourceName(id), d.lbName(id)} {
		item, e := d.cs.CoreV1().Services(d.opts.Namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(e) {
			continue
		}
		if e != nil {
			return p, e
		}
		if !d.releaseLabels(id, item.Labels) {
			return p, driver.ErrEnvironmentReleaseConflict
		}
		add("Service", item.ObjectMeta, false)
	}
	secret, e := d.cs.CoreV1().Secrets(d.opts.Namespace).Get(ctx, d.secretName(id), metav1.GetOptions{})
	if e == nil {
		if !d.releaseLabels(id, secret.Labels) {
			return p, driver.ErrEnvironmentReleaseConflict
		}
		add("Secret", secret.ObjectMeta, false)
	} else if !apierrors.IsNotFound(e) {
		return p, e
	}
	selector := labels.Set(d.selector(id)).String()
	sets, err := d.cs.AppsV1().ReplicaSets(d.opts.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return p, err
	}
	parents := map[types.UID]bool{}
	for _, set := range sets.Items {
		if !ownedBy(set.OwnerReferences, "Deployment", dep.UID) || set.Spec.Replicas == nil || *set.Spec.Replicas != 0 {
			return p, driver.ErrEnvironmentReleaseConflict
		}
		parents[set.UID] = true
		add("ReplicaSet", set.ObjectMeta, false)
	}
	pods, err := d.cs.CoreV1().Pods(d.opts.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return p, err
	}
	for _, pod := range pods.Items {
		owned := false
		for _, ref := range pod.OwnerReferences {
			owned = owned || (ref.Kind == "ReplicaSet" && parents[ref.UID])
		}
		if !owned || (pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed) {
			return p, driver.ErrEnvironmentReleaseConflict
		}
		add("Pod", pod.ObjectMeta, false)
	}
	if len(p.Objects) > 128 {
		return p, driver.ErrEnvironmentReleaseUnsupported
	}
	sort.Slice(p.Objects, func(i, j int) bool { return p.Objects[i].Kind+p.Objects[i].Name < p.Objects[j].Kind+p.Objects[j].Name })
	p.SHA256 = p.Digest()
	return p, nil
}
func ownedBy(refs []metav1.OwnerReference, kind string, uid types.UID) bool {
	for _, r := range refs {
		if r.Kind == kind && r.UID == uid && r.Controller != nil && *r.Controller {
			return true
		}
	}
	return false
}
func (d *Driver) releaseLabels(id string, actual map[string]string) bool {
	for k, v := range d.selector(id) {
		if actual[k] != v {
			return false
		}
	}
	return true
}
func (d *Driver) CheckEnvironmentRelease(ctx context.Context, p driver.EnvironmentReleasePlan) error {
	if !d.validReleasePlan(p) {
		return driver.ErrEnvironmentReleaseConflict
	}
	actual, err := d.PreviewEnvironmentRelease(ctx, p.Resource)
	if err != nil {
		return err
	}
	if actual.SHA256 != p.SHA256 {
		return driver.ErrEnvironmentReleaseConflict
	}
	return nil
}
func (d *Driver) validReleasePlan(p driver.EnvironmentReleasePlan) bool {
	if !p.Valid() || p.Namespace != d.opts.Namespace {
		return false
	}
	dep, pvc, pv := 0, 0, 0
	for _, o := range p.Objects {
		switch o.Kind {
		case "Deployment":
			if o.Name != d.resourceName(p.Resource) {
				return false
			}
			dep++
		case "PersistentVolumeClaim":
			if o.Name != d.pvcName(p.Resource) {
				return false
			}
			pvc++
		case "PersistentVolume":
			pv++
		case "Service":
			if o.Name != d.resourceName(p.Resource) && o.Name != d.lbName(p.Resource) {
				return false
			}
		case "Secret":
			if o.Name != d.secretName(p.Resource) {
				return false
			}
		case "ReplicaSet", "Pod":
			if len(o.Name) <= len(d.resourceName(p.Resource))+1 || o.Name[:len(d.resourceName(p.Resource))+1] != d.resourceName(p.Resource)+"-" {
				return false
			}
		default:
			return false
		}
	}
	return dep == 1 && pvc == 1 && pv == 1
}
func releaseDeleteOptions(uid string) metav1.DeleteOptions {
	u := types.UID(uid)
	policy := metav1.DeletePropagationForeground
	return metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &u}, PropagationPolicy: &policy}
}
func (d *Driver) DeleteEnvironmentRelease(ctx context.Context, p driver.EnvironmentReleasePlan, guard func(context.Context) error) error {
	if guard == nil || !d.validReleasePlan(p) {
		return driver.ErrEnvironmentReleaseConflict
	}
	// The entire plan is revalidated before the first side effect. Later
	// individual requests retain UID preconditions even after partial failure.
	if err := guard(ctx); err != nil {
		return err
	}
	if err := d.CheckEnvironmentRelease(ctx, p); err != nil {
		return err
	}
	for _, kind := range []string{"Deployment", "Pod", "ReplicaSet", "Service", "Secret", "PersistentVolumeClaim"} {
		for _, o := range p.Objects {
			if o.Kind != kind || o.ObserveOnly {
				continue
			}
			if err := guard(ctx); err != nil {
				return err
			}
			opts := releaseDeleteOptions(o.UID)
			var err error
			switch kind {
			case "Deployment":
				err = d.cs.AppsV1().Deployments(p.Namespace).Delete(ctx, o.Name, opts)
			case "Pod":
				err = d.cs.CoreV1().Pods(p.Namespace).Delete(ctx, o.Name, opts)
			case "ReplicaSet":
				err = d.cs.AppsV1().ReplicaSets(p.Namespace).Delete(ctx, o.Name, opts)
			case "Service":
				err = d.cs.CoreV1().Services(p.Namespace).Delete(ctx, o.Name, opts)
			case "Secret":
				err = d.cs.CoreV1().Secrets(p.Namespace).Delete(ctx, o.Name, opts)
			case "PersistentVolumeClaim":
				err = d.cs.CoreV1().PersistentVolumeClaims(p.Namespace).Delete(ctx, o.Name, opts)
			}
			if err != nil && !apierrors.IsNotFound(err) {
				if apierrors.IsConflict(err) {
					return errors.Join(driver.ErrEnvironmentReleaseConflict, err)
				}
				return err
			}
		}
	}
	return guard(ctx)
}
func (d *Driver) releaseObject(ctx context.Context, o driver.EnvironmentReleaseObject) (metav1.Object, error) {
	switch o.Kind {
	case "Deployment":
		return d.cs.AppsV1().Deployments(o.Namespace).Get(ctx, o.Name, metav1.GetOptions{})
	case "Pod":
		return d.cs.CoreV1().Pods(o.Namespace).Get(ctx, o.Name, metav1.GetOptions{})
	case "ReplicaSet":
		return d.cs.AppsV1().ReplicaSets(o.Namespace).Get(ctx, o.Name, metav1.GetOptions{})
	case "Service":
		return d.cs.CoreV1().Services(o.Namespace).Get(ctx, o.Name, metav1.GetOptions{})
	case "Secret":
		return d.cs.CoreV1().Secrets(o.Namespace).Get(ctx, o.Name, metav1.GetOptions{})
	case "PersistentVolumeClaim":
		return d.cs.CoreV1().PersistentVolumeClaims(o.Namespace).Get(ctx, o.Name, metav1.GetOptions{})
	case "PersistentVolume":
		return d.cs.CoreV1().PersistentVolumes().Get(ctx, o.Name, metav1.GetOptions{})
	}
	return nil, fmt.Errorf("invalid release object")
}
func (d *Driver) EnvironmentReleaseGone(ctx context.Context, p driver.EnvironmentReleasePlan) (bool, error) {
	if !d.validReleasePlan(p) {
		return false, driver.ErrEnvironmentReleaseConflict
	}
	gone := true
	for _, o := range p.Objects {
		item, err := d.releaseObject(ctx, o)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if string(item.GetUID()) != o.UID {
			return false, driver.ErrEnvironmentReleaseConflict
		}
		gone = false
	}
	if !gone {
		return false, nil
	}
	selector := labels.Set(d.selector(p.Resource)).String()
	pods, err := d.cs.CoreV1().Pods(p.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return false, err
	}
	sets, err := d.cs.AppsV1().ReplicaSets(p.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return false, err
	}
	return len(pods.Items) == 0 && len(sets.Items) == 0, nil
}

var _ driver.VerifiedEnvironmentReleaser = (*Driver)(nil)
