package kubernetes

import (
	"context"
	"fmt"

	"sandbox-gateway/internal/driver"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PrepareStart refreshes runtime download credentials before scale=1 creates a
// new pod. Config and workspace already live on the retained /root PVC.
func (d *Driver) PrepareStart(ctx context.Context, id string, opts driver.StartOptions) error {
	if opts.RuntimeURL == "" {
		return fmt.Errorf("resume runtime URL is required")
	}
	dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		return err
	}
	if coldPending(dep) {
		return driver.ErrColdStartUnknown
	}
	secrets := d.cs.CoreV1().Secrets(d.opts.Namespace)
	secret, err := secrets.Get(ctx, d.secretName(id), metav1.GetOptions{})
	if err != nil {
		return err
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data["GRASP_RUNTIME_URL"] = []byte(opts.RuntimeURL)
	secret.Data["SANDBOX_INJECT_HEADERS"] = []byte(opts.RuntimeHeaders)
	secret.Data["SANDBOX_INJECT"] = []byte("")
	_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
	return err
}
