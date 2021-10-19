package context

import (
	gocontext "context"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/embed"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Options struct {
	log            logrus.FieldLogger
	DesiredContext string
}

func NewOptions(log logrus.FieldLogger) *Options {
	return &Options{
		log: log,
	}
}

func NewCmdContext(log logrus.FieldLogger) *cli.Command {
	o := NewOptions(log)

	return &cli.Command{
		Name:    "context",
		Aliases: []string{"c"},
		Usage:   "Change which devenv you're currently using (much like kubectl config use-context).",
		Description: `
Use the current, running, KinD devenv: 
	devenv context kind:dev-environment

Display all available contexts:
	devenv context
`,
		Action: func(c *cli.Context) error {
			o.DesiredContext = c.Args().First()
			return o.Run(c.Context)
		},
	}
}

func (o *Options) displayContexts(_ gocontext.Context, conf *config.Config, clusters []*kubernetesruntime.RuntimeCluster) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CURRENT\tCLUSTER NAME\tRUNTIME\tCONTEXT NAME")

	foundCurrent := false
	for _, c := range clusters {
		var current string
		if runtime, name := conf.ParseContext(); c.RuntimeName == runtime && c.Name == name {
			current = "*"
			foundCurrent = true
		}

		fmt.Fprintln(w, current+"\t"+c.Name+"\t"+c.RuntimeName+"\t"+c.RuntimeName+":"+c.Name)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if !foundCurrent {
		fmt.Println()
		fmt.Printf(color.YellowString("Warning:") + " current context wasn't found in list, to set a context run: devenv context <runtime:clusterName>\n")
	}

	return nil
}

func (o *Options) setContext(ctx gocontext.Context, conf *config.Config, clusters []*kubernetesruntime.RuntimeCluster) error { //nolint:funlen
	newConfig := &config.Config{CurrentContext: o.DesiredContext}

	newRuntime, newClusterName := newConfig.ParseContext()
	var cluster *kubernetesruntime.RuntimeCluster
	for _, c := range clusters {
		if c.RuntimeName == newRuntime && c.Name == newClusterName {
			cluster = c
			break
		}
	}
	if cluster == nil {
		return fmt.Errorf("unknown context '%s', check current contexts by running 'devenv context'", o.DesiredContext)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(err, "failed to get user's home directory")
	}

	o.log.Infof("Setting context to %s", o.DesiredContext)
	conf.CurrentContext = o.DesiredContext

	// Write the config first so we don't run into issues with a step below
	// failing and then not deterministically changing the context.
	if err := config.SaveConfig(ctx, conf); err != nil { //nolint:govet // Why: err shadow
		return errors.Wrap(err, "failed to save devenv config")
	}

	// Create a Kubernetes client for the new context
	ccc := clientcmd.NewDefaultClientConfig(*cluster.KubeConfig, &clientcmd.ConfigOverrides{})
	rconf, err := ccc.ClientConfig()
	if err != nil {
		return errors.Wrap(err, "failed to create rest config for context")
	}
	k, err := kubernetes.NewForConfig(rconf)
	if err != nil {
		return errors.Wrap(err, "failed to create kubernetes client for context")
	}

	// Update /etc/hosts to point to the new ingress controller
	dir, err := embed.ExtractAllToTempDir(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to extract bundled shell scripts to a temporary directory")
	}
	defer os.RemoveAll(dir)
	shellDir := filepath.Join(dir, "shell")
	ingressControllerIP := devenvutil.GetIngressControllerIP(ctx, k, o.log)

	// Update the kube config to point to the new cluster
	err = clientcmd.WriteToFile(*cluster.KubeConfig, filepath.Join(homeDir, ".outreach", "kubeconfig.yaml"))
	if err != nil {
		return errors.Wrap(err, "failed to write kubeconfig")
	}

	// HACK: In the future we should just expose setting env vars
	err = cmdutil.RunKubernetesCommand(ctx, shellDir, false, filepath.Join(shellDir, "30-etc-hosts.sh"), ingressControllerIP)
	if err != nil {
		return errors.Wrap(err, "failed to run script to setup /etc/hosts to point to context")
	}

	return nil
}

func (o *Options) Run(ctx gocontext.Context) error {
	b, err := box.LoadBox()
	if err != nil {
		return err
	}

	conf, err := config.LoadConfig(ctx)
	if err != nil {
		conf = &config.Config{}
		o.log.WithError(err).Warn("failed to read devenv configuration")
	}

	runtimes := kubernetesruntime.GetEnabledRuntimes(b)

	clusters := make([]*kubernetesruntime.RuntimeCluster, 0)
	for _, r := range runtimes {
		r.Configure(o.log, b)
		if err := r.PreCreate(ctx); err != nil {
			o.log.WithError(err).Warnf("Failed to setup runtime %s, skipping", r.GetConfig().Name)
			continue
		}

		newClusters, err := r.GetClusters(ctx)
		if err != nil {
			o.log.WithError(err).Warnf("Failed to get clusters from runtime %s, skipping", r.GetConfig().Name)
			continue
		}

		clusters = append(clusters, newClusters...)
	}

	if o.DesiredContext != "" {
		return o.setContext(ctx, conf, clusters)
	}

	return o.displayContexts(ctx, conf, clusters)
}
