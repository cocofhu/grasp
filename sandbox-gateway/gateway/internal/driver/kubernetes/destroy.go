package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Destroy confirms removal, including finalizers, before returning the capacity
// slot. A successful DELETE response alone only acknowledges deletion intent.
func (d *Driver) Destroy(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := d.deleteDeployment(ctx, id); err != nil {
		return fmt.Errorf("delete deployment: %w", err)
	}
	if err := d.deleteRemainingPods(ctx, id); err != nil {
		return fmt.Errorf("delete sandbox pods: %w", err)
	}
	ns := d.opts.Namespace
	var errs []error
	if err := d.deletePVC(ctx, id); err != nil {
		errs = append(errs, fmt.Errorf("delete pvc: %w", err))
	}
	for _, name := range []string{d.resourceName(id), d.lbName(id)} {
		if err := ignoreNotFound(d.cs.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{})); err != nil {
			errs = append(errs, fmt.Errorf("delete service %s: %w", name, err))
			continue
		}
		if err := waitDeleted(ctx, func() error {
			_, err := d.cs.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
			return err
		}); err != nil {
			errs = append(errs, fmt.Errorf("service %s deletion unconfirmed: %w", name, err))
		}
	}
	if err := ignoreNotFound(d.cs.CoreV1().Secrets(ns).Delete(ctx, d.secretName(id), metav1.DeleteOptions{})); err != nil {
		errs = append(errs, fmt.Errorf("delete secret: %w", err))
	} else if err := waitDeleted(ctx, func() error {
		_, err := d.cs.CoreV1().Secrets(ns).Get(ctx, d.secretName(id), metav1.GetOptions{})
		return err
	}); err != nil {
		errs = append(errs, fmt.Errorf("secret deletion unconfirmed: %w", err))
	}
	return errors.Join(errs...)
}

func (d *Driver) deleteRemainingPods(ctx context.Context, id string) error {
	for {
		pods, err := d.cs.CoreV1().Pods(d.opts.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Set(d.selector(id)).String()})
		if err != nil {
			return err
		}
		if len(pods.Items) == 0 {
			return nil
		}
		for _, pod := range pods.Items {
			uid := pod.UID
			opts := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}
			if err := ignoreNotFound(d.cs.CoreV1().Pods(d.opts.Namespace).Delete(ctx, pod.Name, opts)); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func waitDeleted(ctx context.Context, get func() error) error {
	for {
		err := get()
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
