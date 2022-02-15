package provision

import (
	"context"
	"encoding/json"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/devenv/pkg/snapshot"
	"github.com/getoutreach/gobox/pkg/app"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// startSnapshotRestore kicks off the snapshot staging job and waits for
// it to finish
//nolint:funlen // Why: most of this is just structs
func (o *Options) stageSnapshot(ctx context.Context, target string, channel box.SnapshotLockChannel) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(o.b.DeveloperEnvironmentConfig.SnapshotConfig.Region))
	if err != nil {
		return errors.Wrap(err, "unable to load SDK config")
	}

	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to retrieve aws credentials")
	}

	conf := &snapshot.Config{
		Dest: snapshot.S3Config{
			S3Host:       "minio.minio:9000",
			Bucket:       "velero-restore",
			Key:          "/",
			AWSAccessKey: "minioaccess",
			AWSSecretKey: "miniosecret",
		},
		Source: snapshot.S3Config{
			S3Host:          o.b.DeveloperEnvironmentConfig.SnapshotConfig.Endpoint,
			Bucket:          o.b.DeveloperEnvironmentConfig.SnapshotConfig.Bucket,
			SnapshotTarget:  target,
			SnapshotChannel: channel,
			AWSAccessKey:    creds.AccessKeyID,
			AWSSecretKey:    creds.SecretAccessKey,
			AWSSessionToken: creds.SessionToken,
			Region:          o.b.DeveloperEnvironmentConfig.SnapshotConfig.Region,
		},
	}

	// marshal the configuration into json so that
	// it can be consumed by the snapshot uploader
	confStr, err := json.Marshal(conf)
	if err != nil {
		return errors.Wrap(err, "failed to marshal snapshot configuration")
	}

	if err := o.deployStage(ctx, "devenv"); err != nil {
		return errors.Wrap(err, "failed to create snapshot manifests")
	}

	o.log.Info("Waiting for snapshot to finish downloading")

	jo, err := o.k.BatchV1().Jobs("devenv").Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "snapshot-stage-",
		},
		Spec: batchv1.JobSpec{
			Completions:  aws.Int32(1),
			BackoffLimit: aws.Int32(5),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "snapshot",
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    "snapshot-stage",
							Image:   "gcr.io/outreach-docker/devenv:" + app.Info().Version,
							Command: []string{"/usr/local/bin/snapshot-uploader"},
							Env: []corev1.EnvVar{
								{
									Name:  "CONFIG",
									Value: string(confStr),
								},
							},
						},
					},
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to create snapshot staging job")
	}

	return kube.StreamJobLogs(ctx, o.k, o.log, jo.Name, jo.Namespace, os.Stderr)
}
