// Registry provides tools for working with development docker registries
package apps

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"text/template"

	"github.com/getoutreach/gobox/pkg/box"
	"github.com/getoutreach/gobox/pkg/region"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func parseRegistryPath(conf *box.DevelopmentRegistries, devenvName string) (string, error) {
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

// DevImageRegistry returns the closest development image registry
func DevImageRegistry(ctx context.Context, log logrus.FieldLogger, b *box.Config, devenvName string) (string, error) {
	runtimeConf := &b.DeveloperEnvironmentConfig.RuntimeConfig
	developmentRegistries := &runtimeConf.DevelopmentRegistries
	loftConf := &runtimeConf.Loft

	// find the cloud that we're configured to use
	cloudName := loftConf.DefaultCloud
	cloud := region.CloudFromCloudName(cloudName)
	if cloud == nil {
		return "", fmt.Errorf("unknown cloud '%s'", cloudName)
	}

	registries, ok := developmentRegistries.Clouds[cloudName]
	if !ok {
		return "", fmt.Errorf("no image registries configured for cloud '%s'", cloudName)
	}
	regions := registries.Regions()

	// find the best region for us, based on the available regions from box
	regionName, err := cloud.Regions(ctx).Filter(regions).Nearest(ctx, log)
	if err != nil {
		regionName = loftConf.DefaultRegion
		log.WithError(err).Warn("Failed to find nearest region, falling back to us")
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
		return "", fmt.Errorf("failed to find a development image registry")
	}

	registryPath, err := parseRegistryPath(&runtimeConf.DevelopmentRegistries, devenvName)
	if err != nil {
		return "", err
	}

	return path.Join(imageRegistryBase, registryPath), nil
}
