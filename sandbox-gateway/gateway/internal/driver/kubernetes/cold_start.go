package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sandbox-gateway/internal/driver"
)

const coldAnnotation = "sandbox-gateway.io/cold-configuration"

type coldConfigurationProof struct {
	driver.ColdStartReceipt
	SecretUID     string `json:"secretUid"`
	SecretSHA256  string `json:"secretSha256"`
	PVCUID        string `json:"pvcUid"`
	CommandSHA256 string `json:"commandSha256"`
	PodSpecSHA256 string `json:"podSpecSha256"`
}

func coldDataSHA(data map[string][]byte) string {
	stable := map[string][]byte{}
	for k, v := range data {
		if k != "GRASP_RUNTIME_URL" && k != "SANDBOX_INJECT_HEADERS" && k != "SANDBOX_INJECT" {
			stable[k] = v
		}
	}
	encoded, _ := json.Marshal(stable)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func coldPodSpecSHA(spec corev1.PodSpec) string {
	encoded, _ := json.Marshal(spec)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func coldProof(dep *appsv1.Deployment) (coldConfigurationProof, error) {
	var proof coldConfigurationProof
	if dep.Annotations[coldAnnotation] == "" || json.Unmarshal([]byte(dep.Annotations[coldAnnotation]), &proof) != nil || proof.RequestID == "" || !driver.ColdSHA256(proof.InputSHA256) || !driver.ColdSHA256(proof.PayloadSHA256) || proof.NativeIdentity != string(dep.UID) || proof.SecretUID == "" || proof.PVCUID == "" {
		return proof, driver.ErrColdStartConflict
	}
	return proof, nil
}

func coldPending(dep *appsv1.Deployment) bool {
	if dep.Annotations[coldAnnotation] == "" {
		return false
	}
	p, err := coldProof(dep)
	return err != nil || p.State != "started"
}

// StartConfigured updates the original Deployment and its original env Secret.
// Every write retains apiserver resourceVersion CAS; no resource is recreated.
func (d *Driver) StartConfigured(ctx context.Context, id string, opts driver.StartOptions) (*driver.ColdStartReceipt, error) {
	if err := driver.ValidateColdStartOptions(opts); err != nil {
		return nil, err
	}
	command, err := driver.BuildColdStartCommand(opts)
	if err != nil {
		return nil, err
	}
	deps := d.cs.AppsV1().Deployments(d.opts.Namespace)
	dep, err := deps.Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if dep.Annotations[coldAnnotation] != "" {
		return nil, driver.ErrColdStartUnknown
	}
	if opts.ColdConfig.ExpectedNativeIdentity != string(dep.UID) {
		return nil, driver.ErrColdStartConflict
	}
	if err := d.validateColdDeployment(ctx, id, dep); err != nil {
		return nil, err
	}
	secrets := d.cs.CoreV1().Secrets(d.opts.Namespace)
	secret, err := secrets.Get(ctx, d.secretName(id), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if secret.UID == "" || secret.ResourceVersion == "" || secret.DeletionTimestamp != nil || !d.releaseLabels(id, secret.Labels) || (secret.Immutable != nil && *secret.Immutable) {
		return nil, driver.ErrColdStartUnsupported
	}
	pvc, err := d.cs.CoreV1().PersistentVolumeClaims(d.opts.Namespace).Get(ctx, d.pvcName(id), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if pvc.UID == "" || pvc.DeletionTimestamp != nil || !d.releaseLabels(id, pvc.Labels) || pvc.Status.Phase != corev1.ClaimBound {
		return nil, driver.ErrColdStartUnsupported
	}
	root, err := coldOriginalRoot(dep, secret)
	if err != nil {
		return nil, err
	}
	if opts.ColdConfig.Env["WORKSPACE_DIR"] != root {
		return nil, driver.ErrColdStartConflict
	}
	configRoot := opts.ColdConfig.Env["CONFIG_ROOT"]
	for _, mount := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
		if configRoot == mount.MountPath || strings.HasPrefix(configRoot, mount.MountPath+"/") || strings.HasPrefix(mount.MountPath, configRoot+"/") {
			return nil, driver.ErrColdStartUnsupported
		}
	}
	data := make(map[string][]byte)
	for key, value := range opts.ColdConfig.Env {
		data[key] = []byte(value)
	}
	injections, _ := json.Marshal(opts.ColdConfig.Injections)
	data["GRASP_COLD_INJECTIONS"] = injections
	data["GRASP_COLD_REQUEST_ID"] = []byte(opts.ColdConfig.RequestID)
	proof := coldConfigurationProof{ColdStartReceipt: driver.ColdStartReceipt{RequestID: opts.ColdConfig.RequestID, InputSHA256: opts.ColdConfig.InputSHA256, PayloadSHA256: opts.ColdConfig.PayloadSHA256, NativeIdentity: string(dep.UID), State: "configuring"}, SecretUID: string(secret.UID), SecretSHA256: coldDataSHA(data), PVCUID: string(pvc.UID)}
	proof.WorkspaceDir = root
	sum := sha256.Sum256([]byte(command))
	proof.CommandSHA256 = hex.EncodeToString(sum[:])
	if dep.Annotations == nil {
		dep.Annotations = map[string]string{}
	}
	encoded, _ := json.Marshal(proof)
	dep.Annotations[coldAnnotation] = string(encoded)
	reserved, err := deps.Update(ctx, dep, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	secret.Data = data
	secret.StringData = nil
	updatedSecret, err := secrets.Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	// Re-read all identities after publication, before allowing any process.
	current, err := d.waitColdReservationStopped(ctx, id, reserved, string(encoded))
	if err != nil {
		return nil, err
	}
	confirmedSecret, err := secrets.Get(ctx, secret.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if confirmedSecret.UID != updatedSecret.UID || confirmedSecret.ResourceVersion != updatedSecret.ResourceVersion || coldDataSHA(confirmedSecret.Data) != proof.SecretSHA256 {
		return nil, driver.ErrColdStartConflict
	}
	confirmedPVC, err := d.cs.CoreV1().PersistentVolumeClaims(d.opts.Namespace).Get(ctx, pvc.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if confirmedPVC.UID != pvc.UID || confirmedPVC.DeletionTimestamp != nil {
		return nil, driver.ErrColdStartConflict
	}
	container := &current.Spec.Template.Spec.Containers[0]
	container.Env = nil
	container.EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name}}}}
	container.Command = []string{"/bin/bash", "-c", command, "grasp-cold", "fetch"}
	container.Args = nil
	proof.State = "started"
	proof.PodSpecSHA256 = coldPodSpecSHA(current.Spec.Template.Spec)
	encoded, _ = json.Marshal(proof)
	current.Annotations[coldAnnotation] = string(encoded)
	one := int32(1)
	current.Spec.Replicas = &one
	if _, err := deps.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		return nil, err
	}
	return &proof.ColdStartReceipt, nil
}

// Updating a Deployment annotation advances its generation. The controller
// then advances observedGeneration through a status write, which necessarily
// changes resourceVersion. Require the frozen resource/generation/spec/intent semantics,
// wait for an actual stopped observation, and use that fresh RV for final CAS.
func (d *Driver) waitColdReservationStopped(ctx context.Context, id string, reserved *appsv1.Deployment, annotation string) (*appsv1.Deployment, error) {
	var current *appsv1.Deployment
	err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, reserved.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if dep.UID != reserved.UID || dep.Generation != reserved.Generation || dep.DeletionTimestamp != nil || !d.releaseLabels(id, dep.Labels) || !reflect.DeepEqual(dep.Spec, reserved.Spec) || dep.Annotations[coldAnnotation] != annotation {
			return false, driver.ErrColdStartConflict
		}
		stopped, err := d.observedStopped(ctx, id, dep)
		if err != nil || !stopped {
			return false, err
		}
		current = dep
		return true, nil
	})
	return current, err
}

func (d *Driver) validateColdDeployment(ctx context.Context, id string, dep *appsv1.Deployment) error {
	if dep.UID == "" || dep.ResourceVersion == "" || dep.DeletionTimestamp != nil || !d.releaseLabels(id, dep.Labels) {
		return driver.ErrColdStartConflict
	}
	stopped, err := d.observedStopped(ctx, id, dep)
	if err != nil {
		return err
	}
	if !stopped {
		return driver.ErrColdStartConflict
	}
	ps := dep.Spec.Template.Spec
	if len(ps.Containers) != 1 || ps.Containers[0].Name != sandboxContainer || len(ps.InitContainers) != 0 || len(ps.Volumes) != 1 || ps.Volumes[0].Name != "data" || ps.Volumes[0].PersistentVolumeClaim == nil || ps.Volumes[0].PersistentVolumeClaim.ClaimName != d.pvcName(id) {
		return driver.ErrColdStartUnsupported
	}
	c := ps.Containers[0]
	// Cold review accepts the known environment-only image entrypoint, not
	// arbitrary commands or private credential volumes from another contract.
	if len(c.Command) != 0 || (len(c.Args) != 0 && !reflect.DeepEqual(c.Args, []string{"fetch"})) || len(c.EnvFrom) != 1 || c.EnvFrom[0].Prefix != "" || c.EnvFrom[0].ConfigMapRef != nil || c.EnvFrom[0].SecretRef == nil || c.EnvFrom[0].SecretRef.Name != d.secretName(id) {
		return driver.ErrColdStartUnsupported
	}
	expected := map[string]string{"/root/workspace": "workspace", "/root/.cache": "cache", "/root/.npm": "npm", "/root/.m2": "m2", "/root/go/pkg/mod": "go-mod", "/var/lib/docker": "docker", "/var/lib/buildkit": "buildkit", "/tmp": "tmp"}
	if len(c.VolumeMounts) != len(expected) {
		return driver.ErrColdStartUnsupported
	}
	for _, m := range c.VolumeMounts {
		sub, ok := expected[m.MountPath]
		if !ok || m.Name != "data" || m.SubPath != sub || m.SubPathExpr != "" || m.ReadOnly {
			return driver.ErrColdStartUnsupported
		}
		delete(expected, m.MountPath)
	}
	if len(expected) != 0 {
		return driver.ErrColdStartUnsupported
	}
	return nil
}

// GetColdStart never scales, rewrites secrets, or installs configuration.
func (d *Driver) GetColdStart(ctx context.Context, id, request string) (*driver.ColdStartReceipt, error) {
	dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	proof, err := coldProof(dep)
	if err != nil || proof.RequestID != request || !d.releaseLabels(id, dep.Labels) {
		return nil, driver.ErrColdStartConflict
	}
	secret, err := d.cs.CoreV1().Secrets(d.opts.Namespace).Get(ctx, d.secretName(id), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	pvc, err := d.cs.CoreV1().PersistentVolumeClaims(d.opts.Namespace).Get(ctx, d.pvcName(id), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(secret.UID) != proof.SecretUID || string(pvc.UID) != proof.PVCUID || pvc.DeletionTimestamp != nil || secret.DeletionTimestamp != nil || !d.releaseLabels(id, secret.Labels) || !d.releaseLabels(id, pvc.Labels) {
		return nil, driver.ErrColdStartConflict
	}
	if proof.State != "started" {
		proof.State = "unknown"
	} else if len(dep.Spec.Template.Spec.Containers) != 1 || len(dep.Spec.Template.Spec.Containers[0].Command) != 5 || dep.Spec.Template.Spec.Containers[0].Command[3] != "grasp-cold" || coldDataSHA(secret.Data) != proof.SecretSHA256 {
		return nil, fmt.Errorf("%w: bootstrap contract changed", driver.ErrColdStartConflict)
	}
	if proof.State == "started" {
		c := dep.Spec.Template.Spec.Containers[0]
		sum := sha256.Sum256([]byte(c.Command[2]))
		if hex.EncodeToString(sum[:]) != proof.CommandSHA256 || coldPodSpecSHA(dep.Spec.Template.Spec) != proof.PodSpecSHA256 || len(c.Env) != 0 || len(c.EnvFrom) != 1 || c.EnvFrom[0].SecretRef == nil || c.EnvFrom[0].SecretRef.Name != d.secretName(id) || c.EnvFrom[0].Prefix != "" || c.EnvFrom[0].ConfigMapRef != nil {
			return nil, driver.ErrColdStartConflict
		}
		// The original accepted Start is never replayed. A pending or later
		// stopped Deployment cannot be represented as a currently started one.
		if deploymentStatus(dep) != driver.StatusRunning {
			proof.State = "unknown"
		}
	}
	return &proof.ColdStartReceipt, nil
}

func coldOriginalRoot(dep *appsv1.Deployment, secret *corev1.Secret) (string, error) {
	root := string(secret.Data["WORKSPACE_DIR"])
	if root == "" {
		root = "/root/workspace"
	}
	for _, v := range dep.Spec.Template.Spec.Containers[0].Env {
		if v.Name == "WORKSPACE_DIR" {
			if v.ValueFrom != nil || v.Value == "" {
				return "", driver.ErrColdStartUnsupported
			}
			root = v.Value
		}
	}
	if !path.IsAbs(root) || root != path.Clean(root) || strings.ContainsAny(root, "\x00\r\n") || (root != "/root/workspace" && !strings.HasPrefix(root, "/root/workspace/")) {
		return "", driver.ErrColdStartUnsupported
	}
	return root, nil
}

func (d *Driver) PreviewColdStart(ctx context.Context, id string) (*driver.ColdStartTarget, error) {
	dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if dep.Annotations[coldAnnotation] != "" {
		return nil, driver.ErrColdStartUnknown
	}
	if err := d.validateColdDeployment(ctx, id, dep); err != nil {
		return nil, err
	}
	secret, err := d.cs.CoreV1().Secrets(d.opts.Namespace).Get(ctx, d.secretName(id), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if secret.UID == "" || secret.ResourceVersion == "" || !d.releaseLabels(id, secret.Labels) {
		return nil, driver.ErrColdStartUnsupported
	}
	root, err := coldOriginalRoot(dep, secret)
	if err != nil {
		return nil, err
	}
	return &driver.ColdStartTarget{NativeIdentity: string(dep.UID), WorkspaceDir: root}, nil
}
