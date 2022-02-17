// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: Contains helpers for looking at pod logs
package kube

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/getoutreach/gobox/pkg/async"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// JobSucceeded returns a bool if the given job has/hasn't succeeded.
// If an error is returned, this is considered unrecoverable.
func JobSucceeded(ctx context.Context, k kubernetes.Interface, name, namespace string) (bool, error) {
	j, err := k.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	// check if the job finished, if so return
	if j.Status.CompletionTime != nil && !j.Status.CompletionTime.Time.IsZero() {
		return true, nil
	}

	for i := range j.Status.Conditions {
		cond := &j.Status.Conditions[i]

		// Exit if we find a complete job condition. In theory we should've hit this
		// above, but it's a special catch all.
		if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
			return true, nil
		}

		// If we're not failed, or we're false if failed, then skip this condition
		if cond.Type != batchv1.JobFailed || cond.Status != corev1.ConditionTrue {
			continue
		}

		// We check here if we're BackOffLimitExceeded so we can bail out entirely.
		// This works as backoff logic
		if strings.Contains(cond.Reason, "BackoffLimitExceeded") {
			return false, fmt.Errorf("Snapshot restore entered BackoffLimitExceeded, giving up")
		}
	}

	return false, nil
}

// StreamJobLogs streams pod logs to the provided io.Writer
func StreamJobLogs(ctx context.Context, k kubernetes.Interface,
	log logrus.FieldLogger, name, namespace string, w io.Writer) error {
	log.Info("Streaming job logs")

	reason := ""
	for ctx.Err() == nil {
		if reason != "" {
			log.WithField("reason", reason).Info("Unable to grab pod logs, waiting 5 seconds to try again ...")
			async.Sleep(ctx, time.Second*5)
		}

		// check if we hit backoff or some other unrecoverable condition
		if _, err := JobSucceeded(ctx, k, name, namespace); err != nil {
			return errors.Wrap(err, "snapshot stage job failed")
		}

		pods, err := k.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + name})
		if err != nil {
			reason = errors.Wrap(err, "failed to get pods for job").Error()
			continue
		}

		if len(pods.Items) == 0 {
			reason = "no pods found for job"
			continue
		}

		pod := pods.Items[0]

		r, err := StreamPodLogs(ctx, k, log, pod.Name, pod.Namespace)
		if err != nil {
			reason = err.Error()
			continue
		}
		io.Copy(w, r) //nolint:errcheck // Why: OK not returning error here
		r.Close()     //nolint:errcheck // Why: best effort

		// check success to prevent needing to wait later
		if ready, err := JobSucceeded(ctx, k, name, namespace); err != nil {
			return errors.Wrap(err, "snapshot stage job failed")
		} else if ready {
			break
		}

		reason = "pod exited unsuccessful"
	}

	log.Info("Job finished")
	return nil
}

// StreamPodLogs streams pod logs from a given pod
func StreamPodLogs(ctx context.Context, k kubernetes.Interface,
	log logrus.FieldLogger, name, namespace string) (io.ReadCloser, error) {
	log = log.WithField("pod", namespace+"/"+name)

	pod, err := k.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get pods for job")
	}

	if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodSucceeded {
		return nil, fmt.Errorf("pod status not running, got: %s", pod.Status.Phase)
	}

	req := k.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{Follow: true})
	r, err := req.Stream(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to stream pod logs, conditions: %v", pod.Status.Conditions)
	}

	return r, nil
}
