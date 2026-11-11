package kubernetes

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/logging"

	"github.com/rs/zerolog/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	managedByLabel   = "sandbox-gateway"
	sandboxContainer = "sandbox"
	defaultLogsTail  = 5000
)

// podLogReader reads a pod container log stream (overridable in unit tests).
// The default implementation uses client-go GetLogs().Stream.
type podLogReader func(ctx context.Context, namespace, podName string, opts *corev1.PodLogOptions) (string, error)

// Options configures the kubernetes driver.
type Options struct {
	InCluster          bool
	Kubeconfig         string
	Namespace          string
	NamePrefix         string
	StorageClass       string
	DataDiskGi         int64 // default PVC size fallback
	PVCAnnotations     map[string]string
	ImagePullSecret    string
	ImagePullPolicy    string
	EnableLoadBalancer bool
	CPUCores           float64 // default CPU limit fallback
	MemoryMB           int64   // default memory limit fallback
	CPURequestCores    float64 // fixed request; >0 preferred over ratio
	MemoryRequestMB    int64
	CPURequestRatio    float64
	MemoryRequestRatio float64
	// PublicPorts are published on the LoadBalancer (session/ide/ssh/app).
	// InternalPorts stay on ClusterIP only (cdp/novnc). Both are required for
	// ConvergePublishSurface so ReconcileOnStartup/Start can heal inventory LBs
	// without a full Spec.
	PublicPorts   []int
	InternalPorts []int
}

// Driver provisions sandboxes as Deployments in Kubernetes and exposes them via
// a MetalLB LoadBalancer Service so clients connect directly to the LB IP.
type Driver struct {
	cs         kubernetes.Interface
	opts       Options
	getPodLogs podLogReader
}

// New builds a kubernetes driver from a kubeconfig or in-cluster config.
func New(o Options) (*Driver, error) {
	o = withDefaults(o)
	cfg, err := loadConfig(o)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create k8s client: %w", err)
	}
	return NewFromClient(cs, o), nil
}

// NewFromClient builds a Driver over an existing client (used by unit tests
// with client-go's fake clientset).
func NewFromClient(cs kubernetes.Interface, o Options) *Driver {
	d := &Driver{cs: cs, opts: withDefaults(o)}
	d.getPodLogs = d.defaultGetPodLogs
	return d
}

func (d *Driver) defaultGetPodLogs(ctx context.Context, namespace, podName string, opts *corev1.PodLogOptions) (string, error) {
	stream, err := d.cs.CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
	if err != nil {
		return "", err
	}
	return drainLogStream(stream)
}

// drainLogStream reads a pod log stream to completion and closes it.
func drainLogStream(stream io.ReadCloser) (string, error) {
	defer func() { _ = stream.Close() }()
	b, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func withDefaults(o Options) Options {
	if o.Namespace == "" {
		o.Namespace = "sandboxes"
	}
	if o.NamePrefix == "" {
		o.NamePrefix = "sbx-"
	}
	if o.ImagePullPolicy == "" {
		o.ImagePullPolicy = "IfNotPresent"
	}
	if o.DataDiskGi == 0 {
		o.DataDiskGi = 25
	}
	if o.CPUCores <= 0 {
		o.CPUCores = 2
	}
	if o.MemoryMB <= 0 {
		o.MemoryMB = 4096
	}
	if o.CPURequestRatio <= 0 {
		o.CPURequestRatio = 0.25
	}
	if o.MemoryRequestRatio <= 0 {
		o.MemoryRequestRatio = 0.25
	}
	if len(o.PublicPorts) > 0 {
		o.PublicPorts = append([]int(nil), o.PublicPorts...)
	}
	if len(o.InternalPorts) > 0 {
		o.InternalPorts = append([]int(nil), o.InternalPorts...)
	}
	return o
}

func loadConfig(o Options) (*rest.Config, error) {
	if o.InCluster {
		return rest.InClusterConfig()
	}
	kubeconfig := o.Kubeconfig
	if kubeconfig == "" {
		if home, err := os.UserHomeDir(); err == nil {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

func (d *Driver) Name() string { return "kubernetes" }

func (d *Driver) resourceName(id string) string { return d.opts.NamePrefix + id }
func (d *Driver) lbName(id string) string       { return d.opts.NamePrefix + id + "-lb" }
func (d *Driver) secretName(id string) string   { return d.opts.NamePrefix + id + "-env" }
func (d *Driver) pvcName(id string) string      { return d.opts.NamePrefix + id + "-data" }

func (d *Driver) selector(id string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": managedByLabel,
		"sandbox-gateway.io/id":        id,
	}
}

func (d *Driver) Create(ctx context.Context, spec driver.Spec) (*driver.Handle, error) {
	ns := d.opts.Namespace
	id := spec.ID
	driver.ApplyK8sPreviewDirect(&spec)
	res := d.resolveResources(spec.Resources)
	step := func(name string, fn func() error) error {
		start := time.Now()
		log.Info().Str("sandbox_id", id).Str("step", name).Msg("k8s create step start")
		err := fn()
		ev := log.Info().Str("sandbox_id", id).Str("step", name).Int64("cost_ms", time.Since(start).Milliseconds())
		if err != nil {
			log.Error().Err(err).Str("sandbox_id", id).Str("step", name).Int64("cost_ms", time.Since(start).Milliseconds()).Msg("k8s create step failed")
			return err
		}
		ev.Msg("k8s create step ok")
		return nil
	}

	if err := step("ensure_namespace", func() error { return d.ensureNamespace(ctx, ns) }); err != nil {
		return nil, err
	}
	if err := step("ensure_secret", func() error {
		return d.ensureSecret(ctx, id, injectConfigEnv(spec.Env, spec.Config))
	}); err != nil {
		return nil, err
	}
	if err := step("ensure_pvc", func() error { return d.ensurePVC(ctx, id, res.DiskGi) }); err != nil {
		return nil, err
	}
	if err := step("ensure_deployment", func() error { return d.ensureDeployment(ctx, spec, res) }); err != nil {
		return nil, err
	}
	if err := step("ensure_clusterip", func() error {
		return d.ensureClusterIPService(ctx, id, listenPorts(spec))
	}); err != nil {
		return nil, err
	}
	if d.opts.EnableLoadBalancer {
		if err := step("ensure_loadbalancer", func() error {
			return d.ensureLoadBalancer(ctx, id, spec.Ports)
		}); err != nil {
			return nil, err
		}
	}
	// Endpoints are backfilled by the service layer once the LB IP is assigned.
	return &driver.Handle{
		ID:        id,
		Name:      d.resourceName(id),
		Namespace: ns,
		Status:    driver.StatusPending,
		Endpoints: map[int]string{},
	}, nil
}

func (d *Driver) resolveResources(r driver.Resources) driver.Resources {
	if r.CPUCores <= 0 {
		r.CPUCores = d.opts.CPUCores
	}
	if r.MemoryMB <= 0 {
		r.MemoryMB = d.opts.MemoryMB
	}
	if r.DiskGi <= 0 {
		r.DiskGi = d.opts.DataDiskGi
	}
	return r
}

func (d *Driver) cpuRequest(limitCores float64) float64 {
	const minRequest = 0.05
	if fixed := d.opts.CPURequestCores; fixed > 0 {
		request := fixed
		if request < minRequest {
			request = minRequest
		}
		if request > limitCores {
			return limitCores
		}
		return request
	}
	ratio := d.opts.CPURequestRatio
	if ratio <= 0 {
		ratio = 0.25
	}
	if ratio > 1 {
		ratio = 1
	}
	request := limitCores * ratio
	if request < minRequest {
		return minRequest
	}
	if request > limitCores {
		return limitCores
	}
	return request
}

func (d *Driver) memoryRequest(limitMB int64) int64 {
	const minRequest int64 = 128
	if fixed := d.opts.MemoryRequestMB; fixed > 0 {
		request := fixed
		if request < minRequest {
			request = minRequest
		}
		if request > limitMB {
			return limitMB
		}
		return request
	}
	ratio := d.opts.MemoryRequestRatio
	if ratio <= 0 {
		ratio = 0.25
	}
	if ratio > 1 {
		ratio = 1
	}
	request := int64(float64(limitMB) * ratio)
	if request < minRequest {
		return minRequest
	}
	if request > limitMB {
		return limitMB
	}
	return request
}

// namespaceLabels for sandbox namespaces. Pod Security Admission must allow
// privileged (DinD needs securityContext.privileged=true).
func namespaceLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by":       managedByLabel,
		"pod-security.kubernetes.io/enforce": "privileged",
		"pod-security.kubernetes.io/audit":   "privileged",
		"pod-security.kubernetes.io/warn":    "privileged",
	}
}

func (d *Driver) ensureNamespace(ctx context.Context, ns string) error {
	want := namespaceLabels()
	existing, err := d.cs.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if err == nil {
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}
		changed := false
		for k, v := range want {
			if existing.Labels[k] != v {
				existing.Labels[k] = v
				changed = true
			}
		}
		if !changed {
			return nil
		}
		_, err = d.cs.CoreV1().Namespaces().Update(ctx, existing, metav1.UpdateOptions{})
		return err
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = d.cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: want},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// injectConfigEnv returns env augmented with the SANDBOX_INJECT contract when
// spec.Config carries a BundleURL. The kubernetes driver has no host bind-mount,
// so HostPath is not supported here; BundleURL rides through env like the docker
// driver's -e SANDBOX_INJECT. Original env is not mutated.
func injectConfigEnv(env map[string]string, cfg *driver.ConfigInject) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	if cfg == nil || cfg.BundleURL == "" {
		return out
	}
	inject := cfg.BundleURL
	if cfg.ConfigRoot != "" {
		inject += "|" + cfg.ConfigRoot
	}
	out["SANDBOX_INJECT"] = inject
	if cfg.Headers != "" {
		out["SANDBOX_INJECT_HEADERS"] = cfg.Headers
	}
	return out
}

func (d *Driver) ensureSecret(ctx context.Context, id string, env map[string]string) error {
	name := d.secretName(id)
	data := map[string]string{}
	for k, v := range env {
		data[k] = v
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: d.opts.Namespace, Labels: d.selector(id)},
		StringData: data,
	}
	_, err := d.cs.CoreV1().Secrets(d.opts.Namespace).Create(ctx, sec, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		_, err = d.cs.CoreV1().Secrets(d.opts.Namespace).Update(ctx, sec, metav1.UpdateOptions{})
	}
	return err
}

func (d *Driver) ensurePVC(ctx context.Context, id string, diskGi int64) error {
	name := d.pvcName(id)
	_, err := d.cs.CoreV1().PersistentVolumeClaims(d.opts.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	if diskGi <= 0 {
		diskGi = d.opts.DataDiskGi
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   d.opts.Namespace,
			Labels:      d.selector(id),
			Annotations: copyStringMap(d.opts.PVCAnnotations),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(fmt.Sprintf("%dGi", diskGi)),
				},
			},
		},
	}
	if d.opts.StorageClass != "" {
		sc := d.opts.StorageClass
		pvc.Spec.StorageClassName = &sc
	}
	_, err = d.cs.CoreV1().PersistentVolumeClaims(d.opts.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (d *Driver) ensureDeployment(ctx context.Context, spec driver.Spec, resLimits driver.Resources) error {
	id := spec.ID
	name := d.resourceName(id)
	labels := d.selector(id)

	listen := listenPorts(spec)
	containerPorts := make([]corev1.ContainerPort, 0, len(listen))
	for _, p := range listen {
		if p < 1 || p > 65535 {
			continue
		}
		containerPorts = append(containerPorts, corev1.ContainerPort{ContainerPort: int32(p)})
	}

	cpuLimit := resource.MustParse(fmt.Sprintf("%.2f", resLimits.CPUCores))
	cpuReq := resource.MustParse(fmt.Sprintf("%.2f", d.cpuRequest(resLimits.CPUCores)))
	memLimit := resource.MustParse(fmt.Sprintf("%dMi", resLimits.MemoryMB))
	memReq := resource.MustParse(fmt.Sprintf("%dMi", d.memoryRequest(resLimits.MemoryMB)))
	res := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    cpuLimit,
			corev1.ResourceMemory: memLimit,
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    cpuReq,
			corev1.ResourceMemory: memReq,
		},
	}

	replicas := int32(1)
	privileged := true
	container := corev1.Container{
		Name:            "sandbox",
		Image:           spec.Image,
		ImagePullPolicy: corev1.PullPolicy(d.opts.ImagePullPolicy),
		EnvFrom:         []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: d.secretName(id)}}}},
		Ports:           containerPorts,
		Resources:       res,
		SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
		// Persist workspace + tool caches + /tmp on the sandbox PVC (subPaths).
		// /tmp must sit on the PVC: the container rootfs is often overlay with
		// multi-hundred-ms fsync, which makes sqlite/go test / mktemp workloads crawl.
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: "/root/workspace", SubPath: "workspace"},
			{Name: "data", MountPath: "/root/.cache", SubPath: "cache"},
			{Name: "data", MountPath: "/root/.npm", SubPath: "npm"},
			{Name: "data", MountPath: "/root/.m2", SubPath: "m2"},
			{Name: "data", MountPath: "/root/go/pkg/mod", SubPath: "go-mod"},
			{Name: "data", MountPath: "/var/lib/docker", SubPath: "docker"},
			{Name: "data", MountPath: "/var/lib/buildkit", SubPath: "buildkit"},
			{Name: "data", MountPath: "/tmp", SubPath: "tmp"},
		},
	}
	if spec.WorkspaceDir != "" {
		container.Env = append(container.Env, corev1.EnvVar{Name: "WORKSPACE_DIR", Value: spec.WorkspaceDir})
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: d.opts.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
					Volumes: []corev1.Volume{
						{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: d.pvcName(id)}}},
					},
				},
			},
		},
	}
	if d.opts.ImagePullSecret != "" {
		dep.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: d.opts.ImagePullSecret}}
	}

	_, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Create(ctx, dep, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		_, err = d.cs.AppsV1().Deployments(d.opts.Namespace).Update(ctx, dep, metav1.UpdateOptions{})
	}
	return err
}

func servicePorts(ports []int) []corev1.ServicePort {
	out := make([]corev1.ServicePort, 0, len(ports))
	for _, p := range ports {
		if p < 1 || p > 65535 {
			continue
		}
		out = append(out, corev1.ServicePort{
			Name:       fmt.Sprintf("p-%d", p),
			Port:       int32(p),
			TargetPort: intstr.FromInt32(int32(p)),
		})
	}
	return out
}

func servicePortSet(ports []corev1.ServicePort) map[int32]struct{} {
	out := make(map[int32]struct{}, len(ports))
	for _, p := range ports {
		out[p.Port] = struct{}{}
	}
	return out
}

func samePortSet(a, b []corev1.ServicePort) bool {
	sa, sb := servicePortSet(a), servicePortSet(b)
	if len(sa) != len(sb) {
		return false
	}
	for p := range sa {
		if _, ok := sb[p]; !ok {
			return false
		}
	}
	return true
}

func selectorEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func (d *Driver) ensureClusterIPService(ctx context.Context, id string, ports []int) error {
	labels := d.selector(id)
	wantPorts := servicePorts(ports)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: d.resourceName(id), Namespace: d.opts.Namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports:    wantPorts,
		},
	}
	_, err := d.cs.CoreV1().Services(d.opts.Namespace).Create(ctx, svc, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, getErr := d.cs.CoreV1().Services(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if getErr != nil {
		return getErr
	}
	if samePortSet(existing.Spec.Ports, wantPorts) && selectorEqual(existing.Spec.Selector, labels) {
		return nil
	}
	existing.Spec.Ports = wantPorts
	existing.Spec.Selector = labels
	_, err = d.cs.CoreV1().Services(d.opts.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (d *Driver) ensureLoadBalancer(ctx context.Context, id string, ports []int) error {
	labels := d.selector(id)
	wantPorts := servicePorts(ports)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: d.lbName(id), Namespace: d.opts.Namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeLoadBalancer,
			Selector: labels,
			Ports:    wantPorts,
		},
	}
	_, err := d.cs.CoreV1().Services(d.opts.Namespace).Create(ctx, svc, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, getErr := d.cs.CoreV1().Services(d.opts.Namespace).Get(ctx, d.lbName(id), metav1.GetOptions{})
	if getErr != nil {
		return getErr
	}
	if existing.Spec.Type == corev1.ServiceTypeLoadBalancer &&
		samePortSet(existing.Spec.Ports, wantPorts) &&
		selectorEqual(existing.Spec.Selector, labels) {
		return nil
	}
	existing.Spec.Type = corev1.ServiceTypeLoadBalancer
	existing.Spec.Ports = wantPorts
	existing.Spec.Selector = labels
	_, err = d.cs.CoreV1().Services(d.opts.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// ConvergePublishSurface heals ClusterIP + LoadBalancer Spec.Ports from driver
// Options (no Spec required). ReconcileOnStartup / Start / Get use this so
// inventory *-lb drop unauthenticated 9222/6080 after upgrade. Empty Options
// are a no-op so unit tests without Public/Internal do not wipe Services.
func (d *Driver) ConvergePublishSurface(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("empty sandbox id")
	}
	public := append([]int(nil), d.opts.PublicPorts...)
	internal := append([]int(nil), d.opts.InternalPorts...)
	listen := listenPorts(driver.Spec{Ports: public, InternalPorts: internal})
	if len(listen) == 0 {
		return nil
	}
	if err := d.ensureClusterIPService(ctx, id, listen); err != nil {
		return fmt.Errorf("converge clusterip: %w", err)
	}
	if !d.opts.EnableLoadBalancer || len(public) == 0 {
		return nil
	}
	if err := d.ensureLoadBalancer(ctx, id, public); err != nil {
		return fmt.Errorf("converge loadbalancer: %w", err)
	}
	return nil
}

func (d *Driver) Start(ctx context.Context, id string) error {
	if err := d.scale(ctx, id, 1); err != nil {
		return err
	}
	if err := d.ConvergePublishSurface(ctx, id); err != nil {
		log.Warn().Err(err).Str("sandbox_id", id).Msg("k8s start: converge publish surface failed")
		return err
	}
	return nil
}
func (d *Driver) Stop(ctx context.Context, id string) error { return d.scale(ctx, id, 0) }

func (d *Driver) scale(ctx context.Context, id string, replicas int32) error {
	name := d.resourceName(id)
	dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	dep.Spec.Replicas = &replicas
	_, err = d.cs.AppsV1().Deployments(d.opts.Namespace).Update(ctx, dep, metav1.UpdateOptions{})
	return err
}

func (d *Driver) Destroy(ctx context.Context, id string) error {
	ns := d.opts.Namespace
	name := d.resourceName(id)
	fg := metav1.DeletePropagationForeground
	delOpts := metav1.DeleteOptions{PropagationPolicy: &fg}
	// Best effort: NotFound is ignored; other errors are recorded.
	logging.WarnErr(ignoreNotFound(d.cs.AppsV1().Deployments(ns).Delete(ctx, name, delOpts)), "k8s destroy deployment", map[string]any{"sandbox_id": id, "name": name})
	logging.WarnErr(ignoreNotFound(d.cs.CoreV1().Services(ns).Delete(ctx, name, delOpts)), "k8s destroy service", map[string]any{"sandbox_id": id, "name": name})
	logging.WarnErr(ignoreNotFound(d.cs.CoreV1().Services(ns).Delete(ctx, d.lbName(id), delOpts)), "k8s destroy lb service", map[string]any{"sandbox_id": id, "name": d.lbName(id)})
	logging.WarnErr(ignoreNotFound(d.cs.CoreV1().Secrets(ns).Delete(ctx, d.secretName(id), delOpts)), "k8s destroy secret", map[string]any{"sandbox_id": id, "name": d.secretName(id)})
	logging.WarnErr(ignoreNotFound(d.cs.CoreV1().PersistentVolumeClaims(ns).Delete(ctx, d.pvcName(id), delOpts)), "k8s destroy pvc", map[string]any{"sandbox_id": id, "name": d.pvcName(id)})
	return nil
}

// Reinstall rebuilds the Deployment (and optionally the data PVC), keeping
// Services/LB so endpoints stay stable. Shared config bind-mounts are not used
// on this driver; ConfigInject rides through env/SANDBOX_INJECT on recreate.
func (d *Driver) Reinstall(ctx context.Context, spec driver.Spec, preserveData bool) error {
	id := spec.ID
	driver.ApplyK8sPreviewDirect(&spec)
	res := d.resolveResources(spec.Resources)

	if err := d.deleteDeployment(ctx, id); err != nil {
		return fmt.Errorf("delete deployment: %w", err)
	}
	if !preserveData {
		if err := d.deletePVC(ctx, id); err != nil {
			return fmt.Errorf("delete pvc: %w", err)
		}
	}

	if err := d.ensureSecret(ctx, id, injectConfigEnv(spec.Env, spec.Config)); err != nil {
		return err
	}
	if err := d.ensurePVC(ctx, id, res.DiskGi); err != nil {
		return err
	}
	if err := d.ensureDeployment(ctx, spec, res); err != nil {
		return err
	}
	// Keep existing Services when present; Update Spec.Ports so a stale LB
	// that still exposes 9222/6080 is converged (AlreadyExists must not no-op).
	if err := d.ensureClusterIPService(ctx, id, listenPorts(spec)); err != nil {
		return err
	}
	if d.opts.EnableLoadBalancer {
		if err := d.ensureLoadBalancer(ctx, id, spec.Ports); err != nil {
			return err
		}
	}
	return nil
}

// PublishPort adds port to ClusterIP and (when enabled) LoadBalancer Services
// so set_preview can map an app port that was not in the create-time app[] list.
func (d *Driver) PublishPort(ctx context.Context, id string, port int) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("empty sandbox id")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d", port)
	}
	cip, err := d.cs.CoreV1().Services(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		return err
	}
	listen := portsFromService(cip)
	listen = driver.AppendPort(listen, port)
	if err := d.ensureClusterIPService(ctx, id, listen); err != nil {
		return err
	}
	if !d.opts.EnableLoadBalancer {
		return nil
	}
	lb, err := d.cs.CoreV1().Services(d.opts.Namespace).Get(ctx, d.lbName(id), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return d.ensureLoadBalancer(ctx, id, []int{port})
		}
		return err
	}
	public := portsFromService(lb)
	public = driver.AppendPort(public, port)
	return d.ensureLoadBalancer(ctx, id, public)
}

func portsFromService(svc *corev1.Service) []int {
	if svc == nil {
		return nil
	}
	out := make([]int, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		out = append(out, int(p.Port))
	}
	return out
}

func (d *Driver) deleteDeployment(ctx context.Context, id string) error {
	ns := d.opts.Namespace
	name := d.resourceName(id)
	fg := metav1.DeletePropagationForeground
	err := d.cs.AppsV1().Deployments(ns).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &fg})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return d.waitDeploymentGone(ctx, name, 2*time.Minute)
}

func (d *Driver) deletePVC(ctx context.Context, id string) error {
	ns := d.opts.Namespace
	name := d.pvcName(id)
	err := d.cs.CoreV1().PersistentVolumeClaims(ns).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return d.waitPVCGone(ctx, name, 2*time.Minute)
}

func (d *Driver) waitDeploymentGone(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("deployment %s still present after %s", name, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (d *Driver) waitPVCGone(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, err := d.cs.CoreV1().PersistentVolumeClaims(d.opts.Namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("pvc %s still present after %s", name, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (d *Driver) Get(ctx context.Context, id string) (*driver.Handle, error) {
	st, err := d.Status(ctx, id)
	if err != nil {
		return nil, err
	}
	switch st {
	case driver.StatusRunning, driver.StatusStopped, driver.StatusPending:
		if convErr := d.ConvergePublishSurface(ctx, id); convErr != nil {
			log.Warn().Err(convErr).Str("sandbox_id", id).Msg("k8s get: converge publish surface failed")
		}
	}
	h := &driver.Handle{ID: id, Name: d.resourceName(id), Namespace: d.opts.Namespace, Status: st}
	eps, err := d.Endpoints(ctx, id)
	if err == nil {
		h.Endpoints = eps
	}
	return h, nil
}

func (d *Driver) List(ctx context.Context) ([]*driver.Handle, error) {
	sel := fmt.Sprintf("app.kubernetes.io/managed-by=%s", managedByLabel)
	ns := d.opts.Namespace
	opts := metav1.ListOptions{LabelSelector: sel}

	deps, err := d.cs.AppsV1().Deployments(ns).List(ctx, opts)
	if err != nil {
		return nil, err
	}
	// Also scan PVCs so orphan data volumes (deployment already gone) are
	// visible to SweepOrphans and get cleaned via Destroy.
	pvcs, err := d.cs.CoreV1().PersistentVolumeClaims(ns).List(ctx, opts)
	if err != nil {
		return nil, err
	}

	byID := map[string]*driver.Handle{}
	remember := func(id, name string, st driver.Status, created time.Time) {
		if id == "" {
			return
		}
		if h, ok := byID[id]; ok {
			if h.Name == "" && name != "" {
				h.Name = name
			}
			if h.Status == "" && st != "" {
				h.Status = st
			}
			if !created.IsZero() && (h.CreatedAt.IsZero() || created.Before(h.CreatedAt)) {
				h.CreatedAt = created
			}
			return
		}
		if name == "" {
			name = d.resourceName(id)
		}
		byID[id] = &driver.Handle{
			ID:        id,
			Name:      name,
			Namespace: ns,
			Status:    st,
			CreatedAt: created,
		}
	}

	for i := range deps.Items {
		dep := &deps.Items[i]
		id := dep.Labels["sandbox-gateway.io/id"]
		if id == "" {
			id = strings.TrimPrefix(dep.Name, d.opts.NamePrefix)
		}
		remember(id, dep.Name, deploymentStatus(dep), dep.CreationTimestamp.Time)
	}
	for i := range pvcs.Items {
		pvc := &pvcs.Items[i]
		id := pvc.Labels["sandbox-gateway.io/id"]
		if id == "" {
			id = strings.TrimSuffix(strings.TrimPrefix(pvc.Name, d.opts.NamePrefix), "-data")
		}
		remember(id, d.resourceName(id), "", pvc.CreationTimestamp.Time)
	}

	handles := make([]*driver.Handle, 0, len(byID))
	for _, h := range byID {
		handles = append(handles, h)
	}
	return handles, nil
}

func (d *Driver) Status(ctx context.Context, id string) (driver.Status, error) {
	dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return driver.StatusNotFound, nil
		}
		return driver.StatusError, err
	}
	return deploymentStatus(dep), nil
}

func deploymentStatus(dep *appsv1.Deployment) driver.Status {
	desired := int32(0)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	if desired == 0 {
		return driver.StatusStopped
	}
	if dep.Status.ReadyReplicas > 0 {
		return driver.StatusRunning
	}
	return driver.StatusPending
}

func listenPorts(spec driver.Spec) []int {
	seen := map[int]struct{}{}
	var out []int
	for _, p := range append(append([]int{}, spec.Ports...), spec.InternalPorts...) {
		if p < 1 || p > 65535 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func clusterDNS(svcName, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", svcName, namespace)
}

func (d *Driver) Endpoints(ctx context.Context, id string) (map[int]string, error) {
	out := map[int]string{}
	clusterHost := clusterDNS(d.resourceName(id), d.opts.Namespace)

	if !d.opts.EnableLoadBalancer {
		svc, err := d.cs.CoreV1().Services(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
		if err != nil {
			return out, err
		}
		for _, p := range svc.Spec.Ports {
			out[int(p.Port)] = fmt.Sprintf("%s:%d", clusterHost, p.Port)
		}
		return out, nil
	}

	lb, err := d.cs.CoreV1().Services(d.opts.Namespace).Get(ctx, d.lbName(id), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return out, nil
		}
		return out, err
	}
	ip := loadBalancerIP(lb)
	lbPorts := map[int]struct{}{}
	for _, p := range lb.Spec.Ports {
		port := int(p.Port)
		lbPorts[port] = struct{}{}
		if ip != "" {
			out[port] = fmt.Sprintf("%s:%d", ip, port)
		}
	}

	cip, cipErr := d.cs.CoreV1().Services(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if cipErr != nil {
		if ip == "" {
			return out, nil
		}
		if apierrors.IsNotFound(cipErr) {
			return out, nil
		}
		return out, cipErr
	}
	for _, p := range cip.Spec.Ports {
		port := int(p.Port)
		if _, ok := lbPorts[port]; ok {
			continue
		}
		out[port] = fmt.Sprintf("%s:%d", clusterHost, port)
	}
	return out, nil
}

// Logs returns combined sandbox-container stdout/stderr via client-go GetLogs
// (non-follow). Semantics align with the docker driver: merge streams, honor
// tail (≤0 → 5000), empty success vs not-found/API failure are distinguishable.
func (d *Driver) Logs(ctx context.Context, id string, tail int) (string, error) {
	if tail <= 0 {
		tail = defaultLogsTail
	}
	ns := d.opts.Namespace
	list, err := d.cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(d.selector(id)).String(),
	})
	if err != nil {
		return "", fmt.Errorf("list pods for sandbox %s: %w", id, err)
	}
	pod := pickPodForLogs(list.Items)
	if pod == nil {
		return "", fmt.Errorf("sandbox %s not found", id)
	}
	tailLines := int64(tail)
	opts := &corev1.PodLogOptions{
		Container: sandboxContainer,
		Follow:    false,
		TailLines: &tailLines,
	}
	out, err := d.getPodLogs(ctx, ns, pod.Name, opts)
	if err != nil {
		return "", fmt.Errorf("kubernetes logs: %w", err)
	}
	return out, nil
}

// pickPodForLogs prefers a Running pod; otherwise picks the newest by
// CreationTimestamp. Returns nil when the list is empty.
func pickPodForLogs(pods []corev1.Pod) *corev1.Pod {
	if len(pods) == 0 {
		return nil
	}
	var best *corev1.Pod
	for i := range pods {
		p := &pods[i]
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		if best == nil || p.CreationTimestamp.After(best.CreationTimestamp.Time) {
			best = p
		}
	}
	if best != nil {
		return best
	}
	best = &pods[0]
	for i := 1; i < len(pods); i++ {
		p := &pods[i]
		if p.CreationTimestamp.After(best.CreationTimestamp.Time) {
			best = p
		}
	}
	return best
}

// WaitLoadBalancerIP polls the LB Service until an ingress IP is assigned.
func (d *Driver) WaitLoadBalancerIP(ctx context.Context, id string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		svc, err := d.cs.CoreV1().Services(d.opts.Namespace).Get(ctx, d.lbName(id), metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		if ip := loadBalancerIP(svc); ip != "" {
			return ip, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("load balancer IP not assigned for %s", id)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func loadBalancerIP(svc *corev1.Service) string {
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		if ing.IP != "" {
			return ing.IP
		}
		if ing.Hostname != "" {
			return ing.Hostname
		}
	}
	return ""
}

func ignoreNotFound(err error) error {
	if err == nil || apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
