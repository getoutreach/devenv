package expose

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/manifoldco/promptui"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v2"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

//nolint:gochecknoglobals
var (
	startLongDesc = `
		Expose exposes a service to the outside world via an external address. This is currently powered by ngrok. This does not currently support TCP tunnels.
	`
	startExample = `
		# Expose the flagship on a ngrok.io subdomain
		devenv expose bento1a/flagship-server:3000 flagship-jaredallard-outreach

		# Expose a service on a custom domain
		devenv expose reactor--bento1a/reactor:8000 reactor-outreach.outreach-dev.com
	`
)

const (
	namespace = "devenv"
)

type NgrokConfig struct {
	AuthToken string `yaml:"authtoken"`
}

type Options struct {
	log logrus.FieldLogger
	k   kubernetes.Interface

	ServiceName      string
	ServiceNamespace string
	ServicePort      int
	ExternalEndpoint string
	EndpointRegion   string
}

func NewOptions(log logrus.FieldLogger) (*Options, error) {
	k, err := kube.GetKubeClient()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create kubernetes client")
	}

	return &Options{
		log: log,
		k:   k,
	}, nil
}

func NewCmdExpose(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:  "expose",
		Usage: "Expose a service to the outside world",

		Description: cmdutil.NewDescription(startLongDesc, startExample),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "region",
				Usage: "ngrok region to use",
				Value: "us",
			},
		},
		Action: func(c *cli.Context) error {
			o, err := NewOptions(log)
			if err != nil {
				return err
			}

			if c.NArg() != 2 {
				return fmt.Errorf("expected exactly 2 args, got %d", c.NArg())
			}

			spl := strings.Split(c.Args().First(), "/")
			if len(spl) != 2 {
				return fmt.Errorf("expected service to be format: namespace/serviceName:port")
			}

			o.ServiceNamespace = spl[0]

			portSpl := strings.Split(spl[1], ":")
			if len(portSpl) != 2 {
				return fmt.Errorf("expected service name to be format: serviceName:port")
			}
			o.ServiceName = portSpl[0]
			o.ServicePort, err = strconv.Atoi(portSpl[1])
			if err != nil {
				return errors.Wrap(err, "expected port to be an integer but failed to convert")
			}

			o.ExternalEndpoint = c.Args().Get(1)
			o.EndpointRegion = c.String("region")

			return o.Run(c.Context)
		},
	}
}

func (o *Options) EnsureAuthenticated(ctx context.Context) (*NgrokConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user's home directory")
	}

	configPath := filepath.Join(homeDir, ".ngrok2", "ngrok.yml")

	var conf *NgrokConfig

	f, err := os.Open(configPath)
	if err == nil {
		// Validate the auth token at some point, for now we ensure it's not null
		err = yaml.NewDecoder(f).Decode(&conf)
		f.Close()
		if err == nil {
			if conf.AuthToken != "" {
				return conf, nil
			}
		}
	}

	// At this point we ask for a new value
	o.log.Info("Please get your auth token from: https://dashboard.ngrok.com/get-started/your-authtoken")
	prompt := promptui.Prompt{
		Label: "Ngrok Auth Token",
		Mask:  '*',
	}

	resp, err := prompt.Run()
	if err != nil {
		return nil, errors.Wrap(err, "failed to prompt for user input")
	}
	if strings.TrimSpace(resp) == "" {
		return nil, errors.Wrap(err, "provided input was empty")
	}

	conf = &NgrokConfig{
		AuthToken: resp,
	}

	b, err := yaml.Marshal(conf)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal ngrok configuration")
	}

	return conf, ioutil.WriteFile(configPath, b, 0600)
}

func (o *Options) CreateNgrokInstance(ctx context.Context, conf *NgrokConfig) error { //nolint:funlen
	podName := fmt.Sprintf("%s-%s-%d-ngrok", o.ServiceNamespace, o.ServiceName, o.ServicePort)

	err := o.k.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if !kerrors.IsNotFound(err) && err != nil {
		o.log.WithError(err).Warn("failed to clean existing pod")
	}

	envVars := []corev1.EnvVar{
		{
			Name:  "NGROK_AUTH",
			Value: conf.AuthToken,
		},
		{
			Name:  "NGROK_PORT",
			Value: fmt.Sprintf("%s.%s.svc.cluster.local:%d", o.ServiceName, o.ServiceNamespace, o.ServicePort),
		},
		{
			Name:  "NGROK_REGION",
			Value: o.EndpointRegion,
		},
	}

	// Naive hostname vs subdomain detection
	if strings.Contains(o.ExternalEndpoint, ".") {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "NGROK_HOSTNAME",
			Value: o.ExternalEndpoint,
		})
	} else {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "NGROK_SUBDOMAIN",
			Value: o.ExternalEndpoint,
		})
	}

	labels := map[string]string{
		"app":     "devenv-expose",
		"service": o.ServiceNamespace + "-" + o.ServiceName,
	}
	_, err = o.k.CoreV1().Pods(namespace).Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   podName,
			Labels: labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "ngrok",
					Image:           "gcr.io/outreach-docker/dev-env/ngrok",
					ImagePullPolicy: "IfNotPresent",
					Env:             envVars,
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 4040,
						},
					},
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to create ngrok pod")
	}

	_, err = o.k.CoreV1().Services(namespace).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       4040,
					TargetPort: intstr.FromString("http"),
				},
			},
			Selector: labels,
		},
	}, metav1.CreateOptions{})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		return errors.Wrap(err, "failed to create service")
	}

	o.log.WithField("pod", namespace+"/"+podName).Info("created ngrok pod")
	return nil
}

func (o *Options) Run(ctx context.Context) error {
	b, err := box.LoadBox()
	if err != nil {
		return err
	}

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	//nolint:govet // Why: err shadow
	if _, err := devenvutil.EnsureDevenvRunning(ctx, conf, b); err != nil {
		return err
	}

	exconf, err := o.EnsureAuthenticated(ctx)
	if err != nil {
		return err
	}

	_, err = o.k.CoreV1().Services(o.ServiceNamespace).Get(ctx, o.ServiceName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to find service '%s', cannot create ngrok pod", fmt.Sprintf("%s/%s", o.ServiceNamespace, o.ServiceName))
	}

	return o.CreateNgrokInstance(ctx, exconf)
}
