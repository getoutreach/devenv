package devenvutil

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/devenv/pkg/worker"
	"github.com/getoutreach/gobox/pkg/async"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/getoutreach/gobox/pkg/trace"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
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
func EnsureDevenvRunning(ctx context.Context, conf *config.Config, b *box.Config) (kubernetesruntime.Runtime, error) {
	r, err := kubernetesruntime.GetRuntimeFromContext(conf, b)
	if err != nil {
		return nil, fmt.Errorf("No active kubernetes runtime found, investigate with 'devenv status' or provision one")
	}

	return r, nil
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

type ListableType interface {
	List(context.Context, metav1.ListOptions) (interface{}, error)
}

type DeleteObjectsObjects struct {
	// Type is the type of object that should be deleted.
	Type runtime.Object

	// Namespaces is a list of namespaces that objects should be deleted in
	Namespaces []string

	// Validator is a function that returns if an item should be filtered
	// or not.
	Validator func(obj *unstructured.Unstructured) (filter bool)
}

func DeleteObjects(ctx context.Context, log logrus.FieldLogger,
	k kubernetes.Interface, conf *rest.Config, opts DeleteObjectsObjects) error { //nolint:funlen
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

		log.WithField("key", fmt.Sprintf("%s/%s", unstruct.GetNamespace(),
			unstruct.GetName())).Infof("deleting %s", mapping.Resource.GroupResource().String())
		namespacedDr := dyn.Resource(mapping.Resource).Namespace(unstruct.GetNamespace())
		err := namespacedDr.Delete(ctx, unstruct.GetName(), metav1.DeleteOptions{}) //nolint:govet // Why: We're OK shadowing err
		return errors.Wrap(trace.SetCallStatus(ctx, err), "failed to delete object"), nil
	})
	return err
}

// FindUnreadyPods checks all namespaces to find pods that are unready, they are
// then returned. If an error occurs, err is returned.
func FindUnreadyPods(ctx context.Context, k kubernetes.Interface) ([]string, []*corev1.Pod, error) {
	pods, err := k.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to list pods")
	}

	unreadyPodNames := []string{}
	unreadyPods := []*corev1.Pod{}
	for i := range pods.Items {
		po := &pods.Items[i]
		ready := false

		// Skip completed pods
		if po.Status.Phase == corev1.PodSucceeded {
			continue
		}

		// Special case for strimzi which is broken currently.
		// TODO(jaredallard): Need to figure out what to do here long term.
		if strings.HasPrefix(po.Name, "strimzi-topic-operator") {
			continue
		}

		// Check that a pod is ready (e.g. the ready checks passed)
		for ii := range po.Status.Conditions {
			cond := &po.Status.Conditions[ii]
			if cond.Type == corev1.PodReady { // Ready
				if cond.Status == corev1.ConditionTrue { // True
					ready = true
					break
				}
			}
		}

		// if ready, skip it
		if ready {
			continue
		}

		unreadyPodNames = append(unreadyPodNames, po.Namespace+"/"+po.Name)
	}

	// no unready pods, not an error
	if len(unreadyPodNames) == 0 {
		return nil, nil, nil
	}

	return unreadyPodNames, unreadyPods, fmt.Errorf("not all pods were ready")
}

// WaitForAllPodsToBeReady waits for all pods to be unready.
func WaitForAllPodsToBeReady(ctx context.Context, k kubernetes.Interface, log logrus.FieldLogger) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute*20)
	defer cancel()

	var unreadyPodNames []string
	var unreadyPods []*corev1.Pod
	var err error
	for ctx.Err() == nil {
		unreadyPodNames, unreadyPods, err = FindUnreadyPods(ctx, k)
		if err == nil {
			log.Info("All pods were ready")
			break
		}

		log.WithError(err).WithField("pods", unreadyPodNames).
			Info("Waiting for pods to be ready")

		async.Sleep(ctx, 30*time.Second)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("timed out waiting for pods to be ready: %v", PodsStateInfo(unreadyPods))
	}

	// All pods were ready, so no error
	return nil
}

// PodsStateInfo returns a string per pod with the state
func PodsStateInfo(pods []*corev1.Pod) map[string]interface{} {
	podDetails := map[string]interface{}{}
	for _, podValue := range pods {
		podDetails[podValue.Name] = PodStateInfo(podValue)
	}
	return podDetails
}

// PodStateInfo writes the pod info to a string
func PodStateInfo(p *corev1.Pod) map[string]interface{} {
	podDetails := map[string]interface{}{
		"Phase": p.Status.Phase,
	}

	if p.Status.Message != "" {
		podDetails["Message"] = p.Status.Message
	}
	for i := range p.Status.ContainerStatuses {
		d := ContainerStatusInfo(&p.Status.ContainerStatuses[i])
		podDetails[p.Status.ContainerStatuses[i].Name] = d
	}
	return podDetails
}

// ContainerStatusInfo writes the container status to a string
func ContainerStatusInfo(cs *corev1.ContainerStatus) map[string]interface{} {
	containerDetails := map[string]interface{}{
		"Ready":   cs.Ready,
		"Restart": cs.RestartCount,
	}

	if cs.State.Waiting != nil {
		containerDetails["State"] = "waiting"
		containerDetails["Reason"] = cs.State.Waiting.Reason
	}
	if cs.State.Running != nil {
		containerDetails["State"] = "running"
	}
	if cs.State.Terminated != nil {
		containerDetails["State"] = "terminated"
		containerDetails["ExitCode"] = cs.State.Terminated.ExitCode
		containerDetails["Reason"] = cs.State.Terminated.Reason
	}
	return containerDetails
}
