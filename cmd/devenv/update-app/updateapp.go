package updateapp

import (
	"context"
	"fmt"
	"strings"

	deployapp "github.com/getoutreach/devenv/cmd/devenv/deploy-app"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/containerruntime"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/devenv/pkg/worker"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/getoutreach/gobox/pkg/trace"
	dockerparser "github.com/novln/docker-parser"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	olog "github.com/getoutreach/gobox/pkg/log"

	dockerclient "github.com/docker/docker/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

//nolint:gochecknoglobals
var (
	updateAppLongDesc = `
		update-app(s) updates your applications running in your developer environment. This is done by finding all pods that have your Docker repository, and do not have a tag.
	`
	updateAppExample = `
		# Update all your applications
		devenv update-apps

		# Update a specific application (based on namespace)
		devenv update-app authz--bento1a (bootstrap)
		devenv update-app flagship (non-standard)
	`
	notUpdatableViaDeployApp = map[string]bool{
		"bento1a": true,
	}
	serviceNameMap = map[string]string{
		"templating-service": "outreach-templating-service",
	}
)

type Options struct {
	log logrus.FieldLogger
	k   kubernetes.Interface
	d   dockerclient.APIClient
	b   *box.Config

	AppName string
}

func NewOptions(log logrus.FieldLogger) *Options {
	b, err := box.LoadBox()
	if err != nil {
		panic(err)
	}

	return &Options{
		log: log,
		b:   b,
	}
}

func NewCmdUpdateApp(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "update-app",
		Aliases:     []string{"update-apps"},
		Usage:       "Update applications in your developer environment",
		Description: cmdutil.NewDescription(updateAppLongDesc, updateAppExample),
		Flags:       []cli.Flag{},
		Action: func(c *cli.Context) error {
			o := NewOptions(log)
			o.AppName = c.Args().First()

			k, err := kube.GetKubeClient()
			if err != nil {
				return errors.Wrap(err, "failed to create kubernetes client")
			}
			o.k = k

			d, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
			if err != nil {
				return errors.Wrap(err, "failed to create docker client")
			}
			o.d = d

			return o.Run(c.Context)
		},
	}
}

type service struct {
	Name   string
	Images []string
	Pods   []*metav1.PartialObjectMetadata
}

func (s *service) GetPodNames() []string {
	names := make([]string, len(s.Pods))
	for i, p := range s.Pods {
		names[i] = fmt.Sprintf("%s/%s", p.Namespace, p.Name)
	}

	return names
}

func (s *service) MarshalLog(addField func(field string, value interface{})) {
	addField("service.name", s.Name)
	addField("service.images", s.Images)
}

//nolint:funlen
func (o *Options) getUpdatableServices(ctx context.Context, namespace string) ([]*service, error) {
	ctx = trace.StartCall(ctx, "kubernetes.GetPods")
	defer trace.EndCall(ctx)

	services := make(map[string]*service)

	o.log.Info("Fetching list of updatable services")
	cursor := ""
	for {
		items, err := o.k.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			Continue: cursor,
		})
		if trace.SetCallStatus(ctx, err) != nil {
			return nil, errors.Wrap(err, "failed to get pods")
		}

		for i := range items.Items {
			p := &items.Items[i]

			serviceName := strings.Replace(p.Namespace, "--bento1a", "", 1)
			if altServiceName, ok := serviceNameMap[serviceName]; ok {
				serviceName = altServiceName
			}

			svc, ok := services[serviceName]
			if !ok {
				svc = &service{
					Name: serviceName,
				}
			}

			for i := range p.Spec.Containers {
				ref, err := dockerparser.Parse(p.Spec.Containers[i].Image)
				if err != nil {
					o.log.WithError(err).WithField("pod", p.Name).Warn("failed to determine if we can update service")
					continue
				}

				// check if the container is in our registry
				if !strings.Contains(ref.Remote(), o.b.DeveloperEnvironmentConfig.ImageRegistry) {
					continue
				}

				// skip containers w/ non-latest tags, we don't update those
				if ref.Tag() != "latest" {
					continue
				}

				svc.Images = append(svc.Images, ref.Repository())
			}

			// if we found an image matching our needs, then we care about this pod
			if len(svc.Images) != 0 {
				svc.Pods = append(svc.Pods, &metav1.PartialObjectMetadata{
					TypeMeta:   p.TypeMeta,
					ObjectMeta: p.ObjectMeta,
				})

				// append the
				services[svc.Name] = svc
			}
		}

		cursor = items.Continue
		if cursor == "" {
			break
		}
	}

	servicesArray := make([]*service, len(services))
	i := 0
	for _, svc := range services {
		servicesArray[i] = svc
		i++
	}

	return servicesArray, nil
}

func (o *Options) removeImage(ctx context.Context, image string) error {
	ctx = trace.StartCall(ctx, "updateapp.removeImage", olog.F{"image": image})
	defer trace.EndCall(ctx)

	// TODO: we exec docker because there is no clear way for
	// us to use cred helpers (gcr) via the API. We'll need to
	// figure out if that's worth doing.
	// Note: Now we're talking to crictl, so... much harder to do.
	//nolint:gosec
	err := cmdutil.RunKubernetesCommand(
		ctx,
		"",
		true,
		"/bin/bash",
		"-c",
		// TODO: Replace this with a containerd call
		fmt.Sprintf("docker exec %s crictl rmi %s >/dev/null 2>&1", containerruntime.ContainerName, image),
	)
	return trace.SetCallStatus(ctx, err)
}

func (o *Options) removeImages(ctx context.Context, svc *service) error {
	ctx = trace.StartCall(ctx, "updateapp.removeImages", svc)
	defer trace.EndCall(ctx)

	for _, image := range svc.Images {
		err := o.removeImage(ctx, image)
		if err != nil {
			// TODO: Distinguish error messages one day, for now we can't
			// really do much due to execing
			continue
		}
	}

	return nil
}

func (o *Options) removePods(ctx context.Context, svc *service) error {
	ctx = trace.StartCall(ctx, "updateapp.removePods", svc)
	defer trace.EndCall(ctx)
	gracePeriod := int64(1)

	infPods := make([]interface{}, len(svc.Pods))
	for i, p := range svc.Pods {
		infPods[i] = p
	}

	_, err := worker.ProcessArray(ctx, infPods, func(ctx context.Context, infPod interface{}) (interface{}, error) {
		po := infPod.(*metav1.PartialObjectMetadata)
		key := fmt.Sprintf("%s/%s", po.Namespace, po.Name)
		ctx = trace.StartCall(ctx, "updateapp.removePod", olog.F{"pod": key})
		defer trace.EndCall(ctx)

		err := o.k.CoreV1().Pods(po.Namespace).Delete(ctx, po.Name, metav1.DeleteOptions{
			GracePeriodSeconds: &gracePeriod,
		})
		return errors.Wrap(trace.SetCallStatus(ctx, err), "failed to delete pod"), nil
	})
	return trace.SetCallError(ctx, err)
}

func (o *Options) deployApp(ctx context.Context, svc *service) error {
	ctx = trace.StartCall(ctx, "updateapp.deployApp", svc)
	trace.AddInfo(ctx, olog.F{"updateapp.deploy-app.skipped": false})
	defer trace.EndCall(ctx)

	// don't run deploy-app, these are vendored inside of the devenv
	// and require reprovisioning
	if _, ok := notUpdatableViaDeployApp[svc.Name]; ok {
		trace.AddInfo(ctx, olog.F{"updateapp.deploy-app.skipped": true})
		return nil
	}

	opt, err := deployapp.NewOptions(o.log)
	if err != nil {
		return err
	}

	opt.App = svc.Name
	return opt.Run(ctx)
}

func (o *Options) updateService(ctx context.Context, svc *service) error {
	ctx = trace.StartCall(ctx, "updateapp.updateService", svc)
	defer trace.EndCall(ctx)

	err := o.removeImages(ctx, svc)
	if err != nil {
		return err
	}

	err = o.deployApp(ctx, svc)
	if err != nil {
		return err
	}

	return o.removePods(ctx, svc)
}

func (o *Options) updateServices(ctx context.Context, services []*service) error {
	ctx = trace.StartCall(ctx, "updateapp.updateServices")
	defer trace.EndCall(ctx)

	for _, svc := range services {
		o.log.WithFields(logrus.Fields{
			"service": svc.Name,
		}).Info("Updating Service")
		err := o.updateService(ctx, svc)
		if err != nil {
			o.log.WithError(err).Warn("failed to update service")
			continue
		}
	}

	return nil
}

func (o *Options) Run(ctx context.Context) error {
	b, err := box.LoadBox()
	if err != nil {
		return err
	}

	//nolint:govet // Why: err shadow
	if _, err := devenvutil.EnsureDevenvRunning(ctx, b); err != nil {
		return err
	}

	namespace := metav1.NamespaceAll
	if o.AppName != "" {
		namespace = o.AppName
	}

	services, err := o.getUpdatableServices(ctx, namespace)
	if err != nil {
		return err
	}

	o.log.Infof("Updating %d services", len(services))
	err = o.updateServices(ctx, services)
	if err != nil {
		return err
	}

	if len(services) != 0 {
		o.log.Info("Updated all services, please note it may take time for the services to become ready")
	} else {
		o.log.Info("Found no services to update")
	}

	return nil
}
