package devenvutil

import (
	"context"
	"fmt"
	"time"

	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/worker"
	"github.com/getoutreach/gobox/pkg/trace"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

// EnsureDevenvRunning returns an error if the developer
// environment is not running.
func EnsureDevenvRunning(ctx context.Context) error {
	sopt, err := status.NewOptions(logrus.New())
	if err != nil {
		return err
	}

	st, err := sopt.GetStatus(ctx)
	if err != nil {
		return err
	}
	if st.Status != status.Running {
		return fmt.Errorf("expected developer environment status to be %s, got %s instead", status.Running, st.Status)
	}

	return nil
}

// WaitForDevenv waits for the developer environment to be up
// and handle context cancellation. This blocks until finished.
func WaitForDevenv(ctx context.Context, sopt *status.Options, log logrus.FieldLogger) error {
	s, err := sopt.GetStatus(ctx)
	if err == nil {
		if s.Status == status.Running {
			return nil
		}
	}

	ticker := time.NewTicker(5 * time.Second)
	num := 0
loop:
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			num++

			s, err := sopt.GetStatus(ctx)
			if err != nil {
				s = &status.Status{}
			}

			if s.Status == status.Running {
				break loop
			}

			log.WithField("status", s.Status).WithField("attempt", num).
				Infof("Waiting for developer environment to be up ...")

			// Wait for about 75 seconds, 15 tries.
			if num > 15 {
				return fmt.Errorf("timed out waiting for environment to be ready")
			}
		}
	}

	return nil
}

// RunKubernetesCommand runs a command with KUBECONFIG set. This command runs in the
// provided working directory
// Deprecated: Use cmdutil.RunKubernetesCommand instead.
func RunKubernetesCommand(ctx context.Context, wd, name string, args ...string) error {
	return cmdutil.RunKubernetesCommand(ctx, wd, false, name, args...)
}

type ListableType interface {
	List(context.Context, metav1.ListOptions) (interface{}, error)
}

type DeleteObjectsObjects struct {
	Type       runtime.Object
	Namespaces []string
	Validator  func(obj *unstructured.Unstructured) (filter bool)
}

func DeleteObjects(ctx context.Context, log logrus.FieldLogger, k kubernetes.Interface, conf *rest.Config, opts DeleteObjectsObjects) error { //nolint:funlen
	traceCtx := trace.StartCall(ctx, "kubernetes.GetPods")

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(k.Discovery()))

	if opts.Type == nil {
		return fmt.Errorf("missing Type")
	}

	gvk := opts.Type.GetObjectKind().GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}

	dyn, err := dynamic.NewForConfig(conf)
	if err != nil {
		return err
	}

	dr := dyn.Resource(mapping.Resource)

	objs := make([]interface{}, 0)

	cursor := ""
	for {
		items, err := dr.List(traceCtx, metav1.ListOptions{ //nolint:govet // Why: We're OK shadowing err
			Continue: cursor,
		})
		if trace.SetCallStatus(traceCtx, err) != nil {
			return errors.Wrap(err, "failed to get pods")
		}

		for i := range items.Items {
			item := &items.Items[i]

			// Filter out by namespace if we have any
			if len(opts.Namespaces) > 0 {
				found := false
				for _, namespace := range opts.Namespaces {
					if item.GetNamespace() == namespace {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			if filter := opts.Validator(item); !filter {
				objs = append(objs, *item)
			}
		}

		cursor = items.GetContinue()
		if cursor == "" {
			break
		}
	}

	_, err = worker.ProcessArray(traceCtx, objs, func(ctx context.Context, obj interface{}) (interface{}, error) {
		unstruct := obj.(unstructured.Unstructured)

		log.WithField("key", fmt.Sprintf("%s/%s", unstruct.GetNamespace(), unstruct.GetName())).Infof("deleting %s", mapping.Resource.GroupResource().String())
		namespacedDr := dyn.Resource(mapping.Resource).Namespace(unstruct.GetNamespace())
		err := namespacedDr.Delete(ctx, unstruct.GetName(), metav1.DeleteOptions{}) //nolint:govet // Why: We're OK shadowing err
		return errors.Wrap(trace.SetCallStatus(ctx, err), "failed to delete object"), nil
	})
	return err
}
