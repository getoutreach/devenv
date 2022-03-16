// Package registry implements the registry devenv command
package registry

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"path"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/devenv/pkg/config"
	"github.com/getoutreach/devenv/pkg/devenvutil"
	"github.com/getoutreach/devenv/pkg/kubernetesruntime"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/getoutreach/gobox/pkg/region"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

//nolint:gochecknoglobals
var (
	longDesc = `
		Registry provides tools for working with development docker registries if they are enabled
	`
)

type Options struct {
	log logrus.FieldLogger
}

func NewOptions(log logrus.FieldLogger) (*Options, error) {
	return &Options{
		log: log,
	}, nil
}

func NewCmdRegistry(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "registry",
		Usage:       "Commands to interact with development docker image registries",
		Description: cmdutil.NewDescription(longDesc, ""),
		Flags:       []cli.Flag{},
		Subcommands: []*cli.Command{
			{
				Name:  "get",
				Usage: "Get your developer environment docker image registry instance",
				Description: cmdutil.NewDescription(
					"Returns the URL/path for the closest docker image registry, or the one configured. The path is used to namespace docker images.",
					"",
				),
				Action: func(c *cli.Context) error {
					o, err := NewOptions(log)
					if err != nil {
						return err
					}

					return o.RunGet(c.Context)
				},
			},
		},
	}
}

func (o *Options) parseRegistryPath(conf *box.DevelopmentRegistries, devenvName string) (string, error) {
	str, err := template.New("render").Parse(conf.Path)
	if err != nil {
		return "", errors.Wrap(err,
			"failed to parse endpoint as a go-template string, this is an error in box configuration. Report this to the owning team",
		)
	}

	var buf bytes.Buffer
	if err := str.Execute(&buf, map[string]string{
		"DevenvName": devenvName,
	}); err != nil {
		return "", errors.Wrap(err, "failed to execute go-template endpoint string")
	}

	return buf.String(), nil
}

func (o *Options) getDevenvType(ctx context.Context, b *box.Config) (kubernetesruntime.RuntimeType, error) {
	conf, err := config.LoadConfig(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to load config")
	}
	kr, err := devenvutil.EnsureDevenvRunning(ctx, conf, b)

	if err != nil {
		return "", errors.Wrap(err, "failed to lookup current devenv type")
	}

	krConf := kr.GetConfig()
	return krConf.Type, nil
}

func (o *Options) getDevenvName(ctx context.Context, b *box.Config) (string, error) {
	conf, err := config.LoadConfig(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to load config")
	}

	var devenvName string
	if kr, err := devenvutil.EnsureDevenvRunning(ctx, conf, b); err == nil {
		krConf := kr.GetConfig()
		if krConf.Type != kubernetesruntime.RuntimeTypeRemote {
			return "", fmt.Errorf("This command is only supported for cloud devenvs")
		}
		if krConf.Name != "loft" {
			return "", fmt.Errorf("This command is only supported for loft environments (cloud devenvs)")
		}

		devenvName = kr.GetConfig().ClusterName
	} else {
		return "", errors.Wrap(err, "failed to lookup current devenv to validate is cloud/loft")
	}

	return devenvName, nil
}

// RunGet runs the get command
func (o *Options) RunGet(ctx context.Context) error { //nolint:funlen // Why: acceptable size atm
	b, err := box.LoadBox()
	if err != nil {
		return errors.Wrap(err, "failed to load box configuration")
	}

	devenvType, err := o.getDevenvType(ctx, b)
	if err != nil {
		return err
	}

	if devenvType == kubernetesruntime.RuntimeTypeLocal {
		fmt.Println("devenv.local")
		return nil
	}

	devenvName, err := o.getDevenvName(ctx, b)
	if err != nil {
		return err
	}

	runtimeConf := &b.DeveloperEnvironmentConfig.RuntimeConfig
	developmentRegistries := &runtimeConf.DevelopmentRegistries
	loftConf := &runtimeConf.Loft

	// find the cloud that we're configured to use
	cloudName := loftConf.DefaultCloud
	cloud := region.CloudFromCloudName(cloudName)
	if cloud == nil {
		return fmt.Errorf("unknown cloud '%s'", cloudName)
	}

	registries, ok := developmentRegistries.Clouds[cloudName]
	if !ok {
		return fmt.Errorf("no image registries configured for cloud '%s'", cloudName)
	}
	regions := registries.Regions()

	// find the best region for us, based on the available regions from box
	regionName, err := cloud.Regions(ctx).Filter(regions).Nearest(ctx, o.log)
	if err != nil {
		regionName = loftConf.DefaultRegion
		o.log.WithError(err).Warn("Failed to find nearest region, falling back to us")
	}

	imageRegistryBase := ""
	for _, e := range registries {
		if e.Region == regionName {
			// use the first one configured for the region
			imageRegistryBase = e.Endpoint
			break
		}
	}
	if imageRegistryBase == "" {
		return fmt.Errorf("failed to find a development image registry")
	}

	registryPath, err := o.parseRegistryPath(&runtimeConf.DevelopmentRegistries, devenvName)
	if err != nil {
		return err
	}

	// print out the endpoint based on the templated path output + base endpoint
	// from box
	fmt.Println(path.Join(imageRegistryBase, registryPath))
	return nil
}
