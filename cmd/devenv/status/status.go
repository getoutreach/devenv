package status

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/containerruntime"
	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/gobox/pkg/app"
	"github.com/getoutreach/gobox/pkg/trace"
	localizerapi "github.com/getoutreach/localizer/api"
	"github.com/getoutreach/localizer/pkg/localizer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	Degraded      = "degraded"
	Unprovisioned = "unprovisioned"
	Running       = "running"
	Stopped       = "stopped"
	Unknown       = "unknown"
)

//nolint:gochecknoglobals
var (
	statusLongDesc = `
		status shows the status of your developer environment.

		This is done by checking, simply, if it is up or down, but also runs a series of health checks to report the health.
	`
	statusExample = `
		# View the status of the developer environment
		devenv status
	`
)

type Options struct {
	log logrus.FieldLogger
	k   kubernetes.Interface
	d   dockerclient.APIClient

	// Quiet denotes if we should output text or not
	Quiet bool

	// Namespaces is a slice of strings which, if not empty, filters
	// the output of the status command.
	Namespaces []string

	// AllNamespaces is a flag that denotes whether or not to display
	// all namespaces in the output of the status command.
	AllNamespaces bool

	// IncludeKubeSystem is a flag that denotes whether or not to
	// include kube-system in the output of the status command.
	IncludeKubeSystem bool
}

func NewOptions(log logrus.FieldLogger) (*Options, error) {
	//nolint:errcheck // Why: We handle errors
	k, _ := kube.GetKubeClient()

	//nolint:errcheck // Why: We handle errors
	d, _ := dockerclient.NewClientWithOpts(dockerclient.FromEnv)

	return &Options{
		d:   d,
		k:   k,
		log: log,
	}, nil
}

func NewCmdStatus(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "status",
		Usage:       "View the status of the developer environment",
		Description: cmdutil.NewDescription(statusLongDesc, statusExample),
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "quiet",
				Aliases: []string{"q"},
				Usage:   "Whether to print a detailed status message",
			},
			&cli.StringSliceFlag{
				Name:    "namespace",
				Aliases: []string{"n"},
				//nolint:lll // Why: Not much we can do here.
				Usage: "Which namespace to print information about, can be duplicated to show multiple namespaces. If omitted, all namespaces will be printed.",
			},
			&cli.BoolFlag{
				Name:  "kube-system",
				Usage: "Include kube-system in the output.",
			},
			&cli.BoolFlag{
				Name:    "all-namespaces",
				Aliases: []string{"a"},
				Usage:   "Displays all namespaces in the output.",
			},
		},
		Action: func(c *cli.Context) error {
			o, err := NewOptions(log)
			if err != nil {
				return err
			}

			o.Quiet = c.Bool("quiet")
			o.Namespaces = c.StringSlice("namespace")
			o.IncludeKubeSystem = c.Bool("kube-system")
			o.AllNamespaces = c.Bool("all-namespaces")

			return o.Run(c.Context)
		},
	}
}

type Status struct {
	// Status is the status of the cluster in text format, eventually
	// will be enum of: running, stopped, unprovisioned, degraded, or unknown
	Status string

	// Reason is included when a status may need potential
	// explanation. For now this is just non-running or stopped statuses
	Reason string

	// KubernetesVersion is the version of the developer environment
	KubernetesVersion string

	// Version is the version of the developer environment
	Version string
}

// GetStatus determines the status of a developer environment
// nolint:funlen
func (o *Options) GetStatus(ctx context.Context) (*Status, error) {
	ctx = trace.StartCall(ctx, "status.GetStatus")
	defer trace.EndCall(ctx)

	status := &Status{
		Status: Unknown,
	}

	if o.d == nil {
		status.Reason = "Failed to communicate with Docker (client couldn't be created)"
		return status, nil
	}

	if o.k == nil {
		status.Status = Unprovisioned
		return status, nil
	}

	timeout := int64(5)
	_, err := o.k.CoreV1().Pods("default").List(ctx, metav1.ListOptions{Limit: 1, TimeoutSeconds: &timeout})
	if err != nil {
		status.Status = Degraded
		status.Reason = errors.Wrap(err, "failed to reach kubernetes").Error()
		return status, nil
	}

	v, err := o.k.Discovery().ServerVersion()
	if err != nil {
		status.Status = Degraded
		status.Reason = errors.Wrap(err, "failed to get kubernetes version").Error()
		return status, nil
	}

	err = o.CheckLocalDNSResolution(ctx)
	if err != nil {
		status.Status = Degraded
		status.Reason = errors.Wrap(err, "local DNS resolution is failing").Error()
		return status, nil
	}

	// set the server version
	status.KubernetesVersion = v.String()

	// we assume running and healthy at this point
	status.Status = Running
	return status, nil
}

func (o *Options) CheckLocalDNSResolution(ctx context.Context) error { //nolint:funlen
	ctx = trace.StartCall(ctx, "status.CheckLocalDNSResolution")
	defer trace.EndCall(ctx)

	addrs, err := net.LookupHost("localhost")
	if err != nil {
		return errors.Wrap(err, "localhost lookup failed")
	}

	if len(addrs) == 0 {
		return fmt.Errorf("localhost had no addresses")
	}

	return nil
}

func (o *Options) kubernetesInfo(ctx context.Context, w io.Writer) error { //nolint:funlen,gocyclo
	nodes, err := o.k.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	namespaces, err := o.k.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}

	var localizerResp *localizerapi.ListResponse
	if localizer.IsRunning() {
		// Turn off info and warning level logging for gRPC because it's nosiy and we handle it
		// from a higher level anyways.
		grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, io.Discard, os.Stdout))

		// We block on the connection, so only try for 2 seconds before moving on. This should
		// be fine if localizer is actually running because its communicating over the local
		// network.
		gCtx, cancel := context.WithTimeout(ctx, time.Second*2)

		client, closer, err := localizer.Connect(gCtx,
			grpc.WithBlock(), grpc.WithInsecure()) //nolint:govet // Why: It's okay to shadow the error here.
		if err == nil {
			defer closer()

			if localizerResp, err = client.List(ctx, &localizerapi.ListRequest{}); err != nil {
				// Fail silently, we warn below.
				localizerResp = nil
			}
		}

		if localizerResp == nil {
			//nolint:lll // Why: Not much we can do here
			o.log.WithError(err).Warn("failed to call localizer list rpc, will not include localizer information in response.")
			//nolint:lll // Why: Not much we can do here
			o.log.Warn("if you need localizer information, the following and then rerun:\n\tsudo kill $(pgrep localizer)\n\tsudo rm -f /var/run/localizer.sock\n\tdevenv tunnel")
		}

		// Cancel just for the sake of cancelling.
		cancel()
	}

	for i := range nodes.Items {
		if nodes.Items[i].Name != containerruntime.ContainerName {
			continue
		}

		capacity := &nodes.Items[i].Status.Capacity
		allocatable := &nodes.Items[i].Status.Allocatable

		fmt.Fprintf(w, "\nNode \"%s\" Information:\n---\n", containerruntime.ContainerName)

		fmt.Fprintln(w, "Resources (capacity/allocatable):")
		fmt.Fprintf(w, "\tCPU: %s/%s\n", capacity.Cpu(), allocatable.Cpu())
		fmt.Fprintf(w, "\tMemory: %s/%s\n", capacity.Memory(), allocatable.Memory())
		fmt.Fprintf(w, "\tStorage (Ephemeral): %s/%s\n", capacity.StorageEphemeral(), allocatable.StorageEphemeral())
		fmt.Fprintf(w, "\tPods: %s/%s\n", capacity.Pods(), allocatable.Pods())

		fmt.Fprintln(w, "Conditions:")
		for j := range nodes.Items[i].Status.Conditions {
			//nolint:lll // Why: Not much we can do here
			fmt.Fprintf(w, "\t%s: %s (%s)\n", nodes.Items[i].Status.Conditions[j].Type, nodes.Items[i].Status.Conditions[j].Status, nodes.Items[i].Status.Conditions[j].Message)
		}

		fmt.Fprintf(w, "Images Deployed: %d\n", len(nodes.Items[i].Status.Images))
		break
	}

	for i := range namespaces.Items {
		if namespaces.Items[i].Name == "kube-system" {
			if !o.IncludeKubeSystem {
				continue
			}
		}

		var included bool
		if o.AllNamespaces {
			included = true
		} else {
			for j := range o.Namespaces {
				if strings.EqualFold(strings.TrimSpace(o.Namespaces[j]), namespaces.Items[i].Name) {
					included = true
					break
				}
			}
		}

		if !included {
			continue
		}

		deployments, err := o.k.AppsV1().Deployments(namespaces.Items[i].Name).
			List(ctx, metav1.ListOptions{}) //nolint:govet // why: it's okay to shadow the error variable here
		if err != nil {
			return err
		}

		// Skip namespaces who have 0 deployments.
		if len(deployments.Items) == 0 {
			continue
		}

		fmt.Fprintf(w, "\n\nNamespace \"%s\" Deployments:\n---\n", namespaces.Items[i].Name)

		for j := range deployments.Items {
			fmt.Fprintf(w, "%s [ ", deployments.Items[j].Name)

			for k := range deployments.Items[j].Status.Conditions {
				if deployments.Items[j].Status.Conditions[k].Status == v1.ConditionTrue {
					fmt.Fprintf(w, "%s ", deployments.Items[j].Status.Conditions[k].Type)
				}
			}

			fmt.Fprint(w, "]\n")

			if localizerResp != nil {
				for k := range localizerResp.Services {
					if localizerResp.Services[k] == nil {
						// Shouldn't ever happen, but panic insurance.
						continue
					}

					service := localizerResp.Services[k]

					if localizerResp.Services[k].Name == deployments.Items[j].Name {
						fmt.Fprintf(w, "-> Status: %s [%s] <localizer>\n", service.Status, service.StatusReason)
						fmt.Fprintf(w, "-> Endpoint: %s <localizer>\n", service.Endpoint)
						fmt.Fprintf(w, "-> IP: %s <localizer>\n", service.Ip)
						fmt.Fprintf(w, "-> Ports: %s <localizer>\n", service.Ports)
					}
				}
			}
		}
	}

	return nil
}

func (o *Options) Run(ctx context.Context) error { //nolint:funlen,gocyclo
	target := io.Writer(os.Stdout)
	if o.Quiet {
		target = ioutil.Discard
	}

	w := tabwriter.NewWriter(target, 10, 0, 5, ' ', 0)

	status, err := o.GetStatus(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintln(w, "Overall Status:\n---")
	fmt.Fprintf(w, "Status: %s\n", status.Status)
	fmt.Fprintf(w, "Devenv Version: %s\n", app.Info().Version)
	if status.Reason != "" {
		fmt.Fprintf(w, "Reason: %s\n", status.Reason)
	}

	if status.Version != "" {
		fmt.Fprintf(w, "Running devenv Version: %s\n", status.Version)
	}
	if status.KubernetesVersion != "" {
		fmt.Fprintf(w, "Kubernetes Version: %s\n", status.KubernetesVersion)
	}
	// Only show Kubernetes info if we were able to make a client
	if o.k != nil {
		fmt.Fprintln(w, "\ndevenv kubectl top nodes output:\n---")

		err = cmdutil.RunKubernetesCommand(ctx, "", false, "kubectl", "top", "nodes")
		if err != nil {
			o.log.WithError(err).Warn("kubectl metrics unavailable currently, check again later")
		}

		err = o.kubernetesInfo(ctx, w)
		if err != nil {
			return err
		}
	}

	if err := w.Flush(); err != nil { //nolint:govet // We're. OK. Shadowing. Error.
		return err
	}

	if status.Status != "running" {
		os.Exit(1) //nolint:gocritic
	}

	return err
}
