package provision

import (
	"context" //nolint:gosec // Why: We're just doing digest checking
	"encoding/json"
	"fmt"
	"io"
	"os"
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

// startSnapshotRestore kicks off the snapshot staging job and streams
// its output to stdout
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
			// TODO: probably should put this in our box configuration?
			S3Host:       "s3.amazonaws.com",
			Bucket:       o.b.DeveloperEnvironmentConfig.SnapshotConfig.Bucket,
			Key:          s.URI,
			AWSAccessKey: creds.AccessKeyID,
			AWSSecretKey: creds.SecretAccessKey,
		},
	}

	// marshal the configuration into json so that
	// it can be consumed by the snapshot uploader
	confStr, err := json.Marshal(conf)
	if err != nil {
		return errors.Wrap(err, "failed to marshal snapshot configuration")
	}

	jo, err := o.k.BatchV1().Jobs("devenv").Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "snapshot-stage",
		},
		Spec: batchv1.JobSpec{
			Completions: aws.Int32(1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "snapshot-stage",
							Image: "gcr.io/outreach-docker/devenv:" + app.Info().Version,
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

	// find a job pod and stream the logs, if any step fails
	// find a new pod and stream the logs
	for ctx.Err() == nil {
		po, err := o.findJobPod(ctx, jo)
		if err == nil {
			err := o.streamPodLogs(ctx, po, jo) //nolint:govet // Why: we're OK shadowing err
			if err == nil {
				break
			}
		}

		o.log.WithError(err).Warn("failed to stream pod logs, or job didn't finish successfully")
		async.Sleep(ctx, time.Second*10)
	}

	return nil
}

// streamPodLogs streams a pod's logs to stdout
func (o *Options) streamPodLogs(ctx context.Context, po *corev1.Pod, jo *batchv1.Job) error {
	req := o.k.CoreV1().Pods(po.Namespace).GetLogs(po.Name, &corev1.PodLogOptions{
		Follow: true,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to create pod logs stream")
	}

	_, err = io.Copy(os.Stdout, stream)
	if err != nil {
		return errors.Wrap(err, "failed to stream pod logs")
	}

	jo2, err := o.k.BatchV1().Jobs(jo.Namespace).Get(ctx, jo.Name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to get job status")
	}

	// check if the job finished
	if jo2.Status.CompletionTime == nil || jo2.Status.CompletionTime.Time.IsZero() {
		return fmt.Errorf("job status was not completed")
	}

	return nil
}

// findJobPod finds a pod for a given job
func (o *Options) findJobPod(ctx context.Context, jo *batchv1.Job) (*corev1.Pod, error) {
	pods, err := o.k.CoreV1().Pods(jo.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list pods")
	}

	var pod *corev1.Pod
loop:
	for i := range pods.Items {
		po := &pods.Items[i]

		// iterate over the ownerreferences of each pod we find to see if it
		// belongs to our job. TODO: It might also be possible to lookup the
		// job and see if a field has this information.
		for ii := range po.OwnerReferences {
			or := &po.OwnerReferences[ii]

			if or.Kind != "Job" {
				continue
			}

			if or.UID != jo.UID {
				continue
			}

			// found a pod for our job
			pod = po
			break loop
		}
	}
	if pod == nil {
		return nil, fmt.Errorf("no pods found")
	}

	return pod, nil
}
