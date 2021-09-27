package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/getoutreach/devenv/pkg/snapshot"
	"github.com/getoutreach/gobox/pkg/app"
	"github.com/getoutreach/gobox/pkg/async"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// fetchSnapshot fetches the latest snapshot information from the box configured
// snapshot bucket based on the provided snapshot channel and target. Then a kubernetes
// job is kicked off that runs snapshot-uploader to actually stage the snapshot
// for velero to restore later.
func (o *Options) fetchSnapshot(ctx context.Context) (*box.SnapshotLockListItem, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(o.b.DeveloperEnvironmentConfig.SnapshotConfig.Region))
	if err != nil {
		return nil, errors.Wrap(err, "unable to load SDK config")
	}

	s3client := s3.NewFromConfig(cfg)
	resp, err := s3client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &o.b.DeveloperEnvironmentConfig.SnapshotConfig.Bucket,
		Key:    aws.String("automated-snapshots/v2/latest.yaml"),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch the latest snapshot information")
	}
	defer resp.Body.Close()

	var lockfile *box.SnapshotLock
	err = yaml.NewDecoder(resp.Body).Decode(&lockfile)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse remote snapshot lockfile")
	}

	if _, ok := lockfile.TargetsV2[o.SnapshotTarget]; !ok {
		return nil, fmt.Errorf("unknown snapshot target '%s'", o.SnapshotTarget)
	}

	if _, ok := lockfile.TargetsV2[o.SnapshotTarget].Snapshots[o.SnapshotChannel]; !ok {
		return nil, fmt.Errorf("unknown snapshot channel '%s'", o.SnapshotChannel)
	}

	if len(lockfile.TargetsV2[o.SnapshotTarget].Snapshots[o.SnapshotChannel]) == 0 {
		return nil, fmt.Errorf("no snapshots found for channel '%s'", o.SnapshotChannel)
	}

	latestSnapshotFile := lockfile.TargetsV2[o.SnapshotTarget].Snapshots[o.SnapshotChannel][0]
	return latestSnapshotFile, o.stageSnapshot(ctx, latestSnapshotFile, &cfg)
}

// startSnapshotRestore kicks off the snapshot staging job and waits for
// it to finish
//nolint:funlen // Why: most of this is just structs
func (o *Options) stageSnapshot(ctx context.Context, s *box.SnapshotLockListItem, cfg *aws.Config) error {
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
			// IDEA: probably should put this in our box configuration?
			S3Host:          "s3.amazonaws.com",
			Bucket:          o.b.DeveloperEnvironmentConfig.SnapshotConfig.Bucket,
			Key:             s.URI,
			AWSAccessKey:    creds.AccessKeyID,
			AWSSecretKey:    creds.SecretAccessKey,
			AWSSessionToken: creds.SessionToken,
			Digest:          s.Digest,
			Region:          o.b.DeveloperEnvironmentConfig.SnapshotConfig.Region,
		},
	}

	// marshal the configuration into json so that
	// it can be consumed by the snapshot uploader
	confStr, err := json.Marshal(conf)
	if err != nil {
		return errors.Wrap(err, "failed to marshal snapshot configuration")
	}

	// IDEA: spinner of some sort here?
	o.log.Info("Waiting for snapshot to finish downloading")
	jo, err := o.k.BatchV1().Jobs("devenv").Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "snapshot-stage-",
		},
		Spec: batchv1.JobSpec{
			Completions:  aws.Int32(1),
			BackoffLimit: aws.Int32(3),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
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

	return o.waitForJobToComplete(ctx, jo)
}

func (o *Options) waitForJobToComplete(ctx context.Context, jo *batchv1.Job) error {
	for ctx.Err() == nil {
		jo2, err := o.k.BatchV1().Jobs(jo.Namespace).Get(ctx, jo.Name, metav1.GetOptions{})
		if err == nil {
			// check if the job finished, if so return
			if jo2.Status.CompletionTime != nil && !jo2.Status.CompletionTime.Time.IsZero() {
				return nil
			}

			for i := range jo2.Status.Conditions {
				cond := &jo2.Status.Conditions[i]

				// Exit if we find a complete job condition. In theory we should've hit this
				// above, but it's a special catch all.
				if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
					return nil
				}

				// If we're not failed, or we're false if failed, then skip this condition
				if cond.Type != batchv1.JobFailed || cond.Status != corev1.ConditionTrue {
					continue
				}

				// We check here if we're BackOffLimitExceeded so we can bail out entirely.
				// This works as backoff logic
				if strings.Contains(cond.Reason, "BackoffLimitExceeded") {
					return fmt.Errorf("Snapshot restore entered BackoffLimitExceeded, giving up")
				}
			}
		}

		async.Sleep(ctx, time.Second*10)
	}
	return ctx.Err()
}
