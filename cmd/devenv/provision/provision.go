package provision

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	dockerclient "github.com/docker/docker/client"
	deployapp "github.com/getoutreach/devenv/cmd/devenv/deploy-app"
	"github.com/getoutreach/devenv/cmd/devenv/destroy"
	"github.com/getoutreach/devenv/cmd/devenv/snapshot"
	"github.com/getoutreach/devenv/pkg/aws"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/containerruntime"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/devenv/pkg/snapshoter"
	"github.com/getoutreach/gobox/pkg/async"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/minio/minio-go/v7"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"github.com/jetstack/cert-manager/cmd/ctl/pkg/renew"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"

	cmclient "github.com/jetstack/cert-manager/pkg/client/clientset/versioned"
)

//nolint:gochecknoglobals
var (
	provisionLongDesc = `
		Provision configures everything you need to start a developer environment. 
		Currently this includes Kubernetes, GCR authentication, and more.
	`
	provisionExample = `
		# Create a new development environment with default
		# applications enabled.
		devenv provision

		# Create a new development environment without the flagship
		devenv provision --skip-app flagship

		# Create a new development environment with an application deploy
		devenv provision --deploy-app authz

		# Restore a snapshot
		devenv provision --snapshot <name>
	`

	imagePullSecretPath = filepath.Join(".outreach", ".config", "dev-environment", "image-pull-secret")
	dockerConfigPath    = filepath.Join(".outreach", ".config", "dev-environment", "dockerconfig.json")
	snapshotLocalBucket = "velero-restore"
)

type Options struct {
	DeployApps        []string
	SnapshotTarget    string
	SnapshotChannel   box.SnapshotLockChannel
	KubernetesRuntime kubernetesruntime.Runtime
	Base              bool

	log     logrus.FieldLogger
	d       dockerclient.APIClient
	homeDir string
	b       *box.Config
	k       kubernetes.Interface
	r       *rest.Config
}

func NewOptions(log logrus.FieldLogger) (*Options, error) {
	d, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create docker client")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	b, err := box.LoadBox()
	if err != nil {
		return nil, errors.Wrap(err, "failed to load box configuration")
	}

	return &Options{
		log:        log,
		d:          d,
		b:          b,
		DeployApps: make([]string, 0),
		homeDir:    homeDir,
	}, nil
}

func NewCmdProvision(log logrus.FieldLogger) *cli.Command { //nolint:funlen
	defaultSnapshot := "unknown"
	b, err := box.LoadBox()
	if err == nil && b != nil {
		defaultSnapshot = b.DeveloperEnvironmentConfig.SnapshotConfig.DefaultName
	}

	return &cli.Command{
		Name:        "provision",
		Usage:       "Provision a new development environment",
		Description: cmdutil.NewDescription(provisionLongDesc, provisionExample),
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:  "deploy-app",
				Usage: "Deploy a specific application (e.g authz)",
			},
			&cli.BoolFlag{
				Name:  "base",
				Usage: "Deploy a developer environment with nothing in it",
			},
			&cli.StringFlag{
				Name:  "snapshot-target",
				Usage: "Snapshot target to use",
				Value: defaultSnapshot,
			},
			&cli.StringFlag{
				Name:  "snapshot-channel",
				Usage: "Snapshot channel to use",
				Value: string(box.SnapshotLockChannelStable),
			},
			&cli.StringFlag{
				Name:  "kubernetes-runtime",
				Usage: "Specify which kubernetes runtime to use (options: kind, loft)",
				Value: "kind",
			},
		},
		Action: func(c *cli.Context) error {
			o, err := NewOptions(log)
			if err != nil {
				return err
			}

			cmdutil.CLIStringSliceToStringSlice(c.StringSlice("deploy-app"), &o.DeployApps)

			o.Base = c.Bool("base")
			o.SnapshotTarget = c.String("snapshot-target")
			o.SnapshotChannel = box.SnapshotLockChannel(c.String("snapshot-channel"))

			runtimeName := c.String("kubernetes-runtime")
			k8sRuntime, err := kubernetesruntime.GetRuntime(runtimeName)
			if err != nil {
				return errors.Wrap(err, "failed to load kubernetes runtime")
			}
			o.KubernetesRuntime = k8sRuntime

			return o.Run(c.Context)
		},
	}
}

func (o *Options) applyPostRestore(ctx context.Context) error { //nolint:funlen
	m, err := snapshoter.NewSnapshotBackend(ctx, o.r, o.k)
	if err != nil {
		return errors.Wrap(err, "failed to create local snapshot storage client")
	}
	defer m.Close()

	obj, err := m.GetObject(ctx, snapshotLocalBucket, "post-restore/manifests.yaml", minio.GetObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).StatusCode == 404 { // If we don't have one, skip this step
			return nil
		}
		return errors.Wrap(err, "failed to fetch post-restore manifests from local snapshot storage")
	}

	manifests, err := ioutil.ReadAll(obj)
	if err != nil {
		return errors.Wrap(err, "failed to read from S3")
	}

	t, err := template.New("post-restore").Delims("[[", "]]").
		Funcs(sprig.TxtFuncMap()).Parse(string(manifests))
	if err != nil {
		return errors.Wrap(err, "failed to parse manifests as go-template")
	}

	u, err := user.Current()
	if err != nil {
		return errors.Wrap(err, "failed to get current user information")
	}

	rawUserEmail, err := exec.CommandContext(ctx, "git", "config", "user.email").CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed to get user email via git: %s", string(rawUserEmail))
	}

	processed, err := os.CreateTemp("", "devenv-post-restore-*")
	if err != nil {
		return errors.Wrap(err, "failed to create temporary file")
	}
	defer os.Remove(processed.Name())

	err = t.Execute(processed, map[string]interface{}{
		"User":           u.Username,
		"Email":          strings.TrimSpace(string(rawUserEmail)),
		"ClusterRuntime": o.KubernetesRuntime.GetConfig(),
	})
	if err != nil {
		return err
	}

	o.log.Info("Applying post-restore manifest(s)")

	return devenvutil.Backoff(ctx, 1*time.Second, 5, func() error {
		return cmdutil.RunKubernetesCommand(ctx, "", false, os.Args[0], "--skip-update", "kubectl", "apply", "-f", processed.Name())
	}, o.log)
}

func (o *Options) snapshotRestore(ctx context.Context) error { //nolint:funlen,gocyclo
	if err := o.deployStage(ctx, "pre-restore"); err != nil {
		return err
	}

	if dir, err := o.extractEmbed(ctx); err != nil {
		return err
	} else if dir != "" {
		defer os.RemoveAll(dir)
	}

	snapshotTarget, err := o.fetchSnapshot(ctx)
	if err != nil {
		return err
	}

	snapshotOpt, err := snapshot.NewOptions(o.log)
	if err != nil {
		return errors.Wrap(err, "failed to create snapshot client")
	}

	// Wait for Velero to load the backup
	err = devenvutil.Backoff(ctx, 30*time.Second, 10, func() error {
		err2 := snapshotOpt.CreateBackupStorage(ctx, "devenv", snapshotLocalBucket)
		if err2 != nil && !kerrors.IsAlreadyExists(err2) {
			o.log.WithError(err2).Debug("Waiting to create backup storage location")
		}

		_, err2 = snapshotOpt.GetSnapshot(ctx, snapshotTarget.VeleroBackupName)
		return err2
	}, o.log)
	if err != nil {
		return errors.Wrap(err, "failed to verify velero loaded snapshot")
	}

	err = snapshotOpt.RestoreSnapshot(ctx, snapshotTarget.VeleroBackupName, false)
	if err != nil {
		return errors.Wrap(err, "failed to restore snapshot")
	}

	err = o.applyPostRestore(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to apply post-restore manifests from local snapshot storage")
	}

	// Sometimes, if we don't preemptively delete all restic-wait containing pods
	// we can end up with a restic-wait attempting to run again, which results
	// in the pod being blocked. This appears to happen whenever a pod is "restarted".
	// Deleting all of these pods prevents that from happening as the restic-wait pod is
	// removed by velero's admission controller.
	o.log.Info("Cleaning up snapshot restore artifacts")
	err = devenvutil.DeleteObjects(ctx, o.log, o.k, o.r, devenvutil.DeleteObjectsObjects{
		Type: &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: corev1.SchemeGroupVersion.Identifier(),
			},
		},
		Validator: func(obj *unstructured.Unstructured) bool {
			var pod *corev1.Pod
			//nolint:govet // Why: we're. OK. Shadowing. err.
			err := kruntime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &pod)
			if err != nil {
				return true
			}

			for i := range pod.Spec.InitContainers {
				cont := &pod.Spec.InitContainers[i]
				if cont.Name == "restic-wait" {
					return false
				}
			}

			return true
		},
	})
	if err != nil {
		return errors.Wrap(err, "failed to cleanup statefulset pods")
	}

	err = o.runProvisionScripts(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to run provision.d scripts")
	}

	o.log.Info("Regenerating certificates with local CA")

	// CA regeneration can sometimes fail, so retry it on failure
	for ctx.Err() == nil {
		// When ropts fails, we need to create a new rest config
		// so just use a fresh one every time here.
		_, k8sConf, err2 := kube.GetKubeClientWithConfig()
		if err2 != nil {
			return err2
		}

		ropts := renew.NewOptions(genericclioptions.IOStreams{In: os.Stdout, Out: os.Stdout, ErrOut: os.Stderr})
		ropts.AllNamespaces = true
		ropts.All = true
		ropts.RESTConfig = k8sConf
		ropts.CMClient, err = cmclient.NewForConfig(k8sConf)
		if err != nil {
			return errors.Wrap(err, "failed to create cert-manager client")
		}

		err2 = ropts.Run(ctx, []string{})
		if err2 != nil && strings.Contains(err2.Error(), "the object has been modified") {
			o.log.WithError(err2).Warn("Retrying certificate regeneration operation ...")
			async.Sleep(ctx, time.Second*5)
			continue
		} else if err2 != nil {
			return errors.Wrap(err2, "failed to trigger certificate regeneration")
		}

		break
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	return devenvutil.WaitForAllPodsToBeReady(ctx, o.k, o.log)
}

func (o *Options) checkPrereqs(ctx context.Context) error {
	// Setup the runtime
	o.KubernetesRuntime.Configure(o.log, o.b)

	// Run the pre-create command
	if err := o.KubernetesRuntime.PreCreate(ctx); err != nil {
		return err
	}

	// Don't need AWS credentials not using a snapshot
	if o.Base {
		return nil
	}

	copts := aws.DefaultCredentialOptions()
	copts.Log = o.log
	if o.b.DeveloperEnvironmentConfig.SnapshotConfig.ReadAWSRole != "" {
		copts.Role = o.b.DeveloperEnvironmentConfig.SnapshotConfig.ReadAWSRole
	}
	return aws.EnsureValidCredentials(ctx, copts)
}

func (o *Options) runProvisionScripts(ctx context.Context) error {
	dir, err := o.extractEmbed(ctx)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	shellDir := filepath.Join(dir, "shell")
	files, err := os.ReadDir(shellDir)
	if err != nil {
		return errors.Wrap(err, "failed to list provision.d scripts")
	}

	o.log.Info("Running post-up steps")

	ingressControllerIP := devenvutil.GetIngressControllerIP(ctx, o.k, o.log)
	for _, f := range files {
		// Skip non-scripts
		if !strings.HasSuffix(f.Name(), ".sh") {
			continue
		}

		o.log.WithField("script", f.Name()).Info("Running provision.d script")

		// HACK: In the future we should just expose setting env vars
		err2 := cmdutil.RunKubernetesCommand(ctx, shellDir, false, filepath.Join(shellDir, f.Name()), ingressControllerIP)
		if err2 != nil {
			return errors.Wrapf(err2, "failed to run provision.d script '%s'", f.Name())
		}
	}

	return nil
}

func (o *Options) deployBaseManifests(ctx context.Context) error {
	if err := o.deployStage(ctx, "pre-restore"); err != nil {
		return err
	}

	return o.runProvisionScripts(ctx)
}

func (o *Options) removeServiceImages(ctx context.Context) error {
	// Only run this on local clusters
	if o.KubernetesRuntime.GetConfig().Type != kubernetesruntime.RuntimeTypeLocal {
		return nil
	}

	//nolint:gosec // Why: We're passing a constant
	cmd := exec.CommandContext(ctx, "docker", "exec",
		kubernetesruntime.KindClusterName+"-control-plane", "ctr", "--namespace", "k8s.io", "images", "ls")
	b, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed to list docker images: %s", string(b))
	}

	images := make(map[string]bool)
	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		text := scanner.Text()

		split := strings.Split(text, " ")
		if len(split) < 1 {
			continue
		}

		img := split[0]
		if !strings.HasPrefix(img, o.b.DeveloperEnvironmentConfig.ImageRegistry) {
			continue
		}

		if !strings.HasSuffix(img, ":latest") {
			continue
		}

		images[img] = true
	}

	for img := range images {
		o.log.WithField("image", img).Infoln("Removing docker image")
		if err2 := containerruntime.RemoveImage(ctx, img); err2 != nil {
			o.log.WithField("image", img).Warn("Failed to remove docker image")
		}
	}

	return nil
}

// generateDockerConfig generates a docker configuration file that is used
// to authenticate image pulls by KinD
func (o *Options) generateDockerConfig() error {
	imgPullSec, err := ioutil.ReadFile(filepath.Join(o.homeDir, imagePullSecretPath))
	if err != nil {
		return err
	}

	dockerConf, err := os.Create(filepath.Join(o.homeDir, dockerConfigPath))
	if err != nil {
		return err
	}
	defer dockerConf.Close()

	return json.NewEncoder(dockerConf).Encode(map[string]interface{}{
		"auths": map[string]interface{}{
			"gcr.io": map[string]interface{}{
				"auth": base64.StdEncoding.EncodeToString([]byte("_json_key:" + string(imgPullSec))),
			},
		},
	})
}

func (o *Options) Run(ctx context.Context) error { //nolint:funlen,gocyclo
	if o.KubernetesRuntime.GetConfig().Type == kubernetesruntime.RuntimeTypeLocal {
		if runtime.GOOS == "darwin" {
			if err := o.configureDockerForMac(ctx); err != nil {
				return err
			}
		}
	}

	if err := o.checkPrereqs(ctx); err != nil { //nolint:govet // Why: OK w/ err shadow
		return errors.Wrap(err, "pre-req check failed")
	}

	// Ensure that we don't try to provision a devenv when the default one already exists
	clusters, err := o.KubernetesRuntime.GetClusters(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to ensure devenv didn't already exist")
	}

	// Iterate over the clusters that currently exist, if it's equal to the default cluster
	// then throw an error -- it already exists and must be deleted with 'devenv destroy'
	for _, c := range clusters {
		if c.Name == o.KubernetesRuntime.GetConfig().ClusterName {
			return fmt.Errorf("devenv already exists, run 'devenv destroy' to be able to run provision again")
		}
	}

	if err := o.ensureImagePull(ctx); err != nil { //nolint:govet // Why: OK w/ err shadow
		return errors.Wrap(err, "failed to setup image pull secret")
	}

	if err := o.generateDockerConfig(); err != nil { //nolint:govet // Why: OK w/ err shadow
		return errors.Wrap(err, "failed to setup image pull secret")
	}

	o.log.WithField("runtime", o.KubernetesRuntime.GetConfig().Name).
		Info("Creating Kubernetes cluster")
	if err := o.KubernetesRuntime.Create(ctx); err != nil { //nolint:govet // Why: OK w/ err shadow
		return errors.Wrap(err, "failed to create kind cluster")
	}

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		conf = &config.Config{}
	}

	// HACK: If we ever add support for running multiple clusters (which makes sense because of context support)
	// we will need to update this
	conf.CurrentContext = o.KubernetesRuntime.GetConfig().Name + ":" + o.KubernetesRuntime.GetConfig().ClusterName

	err = config.SaveConfig(ctx, conf)
	if err != nil {
		return errors.Wrap(err, "failed to save devenv config")
	}

	kconf, err := o.KubernetesRuntime.GetKubeConfig(ctx)
	if err != nil { //nolint:govet // Why: OK w/ err shadow
		return errors.Wrap(err, "failed to create kind cluster")
	}

	//nolint:govet // Why: OK w/ err shadow
	if err := clientcmd.WriteToFile(*kconf, filepath.Join(o.homeDir, ".outreach", "kubeconfig.yaml")); err != nil {
		return errors.Wrap(err, "failed to write kubeconfig")
	}

	k8sClient, k8sRestConf, err := kube.GetKubeClientWithConfig()
	if err != nil {
		return err
	}
	o.k = k8sClient
	o.r = k8sRestConf

	//nolint:govet // Why: OK w/ err shadow
	if err := o.removeServiceImages(ctx); err != nil {
		return errors.Wrap(err, "failed to remove docker images from cache")
	}

	if !o.Base {
		// Restore using a snapshot
		err = o.snapshotRestore(ctx)
		if err != nil { // remove the environment because it's a half baked environment used just for this
			o.log.WithError(err).Error("failed to provision from snapshot, destroying intermediate environment")
			dopts, err2 := destroy.NewOptions(o.log)
			if err2 != nil {
				o.log.WithError(err).Error("failed to remove intermediate environment")
				return err2
			}
			dopts.KubernetesRuntime = o.KubernetesRuntime
			dopts.CurrentClusterName = o.KubernetesRuntime.GetConfig().ClusterName

			cctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
			defer cancel()
			err2 = dopts.Run(cctx)
			if err2 != nil {
				o.log.WithError(err).Error("failed to remove intermediate environment")
				return err2
			}

			return errors.Wrap(err, "failed to provision from snapshot")
		}
	} else {
		o.log.Info("Deploying base manifests")
		// Deploy the base manifests
		//nolint:govet // Why: We're OK shadowing err
		err := o.deployBaseManifests(ctx)
		if err != nil {
			return err
		}
	}

	dopts, err := deployapp.NewOptions(o.log)
	if err != nil {
		return err
	}

	for _, app := range o.DeployApps {
		dopts.App = app
		err2 := dopts.Run(ctx)
		if err2 != nil {
			o.log.WithError(err2).WithField("app.name", app).Warn("failed to deploy application")
		}
	}

	o.log.Info("🎉🎉🎉 devenv is ready 🎉🎉🎉")
	return nil
}
