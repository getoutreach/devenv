package snapshoter

import (
	"context"
	"net/http"
	"os"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	minioAccessKey = "minioaccess"
	minioSecretKey = "miniosecret"
)

type SnapshotBackend struct {
	*minio.Client

	fw *portforward.PortForwarder
}

// NewSnapshotBackend creates a connection to the snapshot backend
// and returns a client for it
func NewSnapshotBackend(ctx context.Context, r *rest.Config, k kubernetes.Interface) (*SnapshotBackend, error) {
	eps, err := k.CoreV1().Endpoints("minio").Get(ctx, "minio", metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to find minio endpoints")
	}

	pod := &corev1.Pod{}
loop:
	for _, sub := range eps.Subsets {
		for _, addr := range sub.Addresses {
			if addr.TargetRef.Kind != "Pod" {
				continue
			}

			pod.Name = addr.TargetRef.Name
			pod.Namespace = addr.TargetRef.Namespace
			break loop
		}
	}

	transport, upgrader, err := spdy.RoundTripperFor(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to upgrade connection")
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", k.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("portforward").URL())

	fw, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{"61002:9000"}, ctx.Done(), nil, os.Stdin, os.Stderr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create port-forward")
	}

	go fw.ForwardPorts() //nolint:errcheck // Why: Best attempt port-forward creation

	m, err := minio.New("127.0.0.1:61002", &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create minio client")
	}

	return &SnapshotBackend{m, fw}, nil
}

// Close closes the underlying snapshot backend client
func (sb *SnapshotBackend) Close() {
	sb.fw.Close()
}
