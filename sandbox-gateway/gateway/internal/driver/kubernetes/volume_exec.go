package kubernetes

import (
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// newPodExecutor streams through the pods/exec subresource.
func newPodExecutor(cfg *rest.Config, cs kubernetes.Interface) podExecutor {
	return func(ctx context.Context, namespace, pod string, command []string, stdin io.Reader, stdout io.Writer) error {
		req := cs.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(pod).SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{Container: volumeContainer, Command: command, Stdin: stdin != nil, Stdout: true, Stderr: true}, scheme.ParameterCodec)
		executor, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
		if err != nil {
			return err
		}
		stderr := &tailBuffer{limit: 4096}
		if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin: stdin, Stdout: stdout, Stderr: stderr}); err != nil {
			return fmt.Errorf("%s: %w: %s", command[0], err, strings.TrimSpace(stderr.String()))
		}
		return nil
	}
}

// tailBuffer keeps the last limit bytes written.
type tailBuffer struct {
	limit int
	buf   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		t.buf = append([]byte(nil), t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.buf) }
