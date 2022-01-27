// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: See package description

// Package deleteapp is a deprecated command
package deleteapp

import (
	"github.com/getoutreach/devenv/cmd/devenv/apps/delete"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func NewCmdDeleteApp(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "delete-app",
		Usage:       "DEPRECATED: Use 'apps delete' instead",
		Description: "DEPRECATED: Use 'apps delete' instead",
		Hidden:      true,
		Action: func(c *cli.Context) error {
			log.Warn("DEPRECATED: Use 'apps delete' instead")
			o, err := delete.NewOptions(log)
			if err != nil {
				return err
			}

			o.App = c.Args().First()
			return o.Run(c.Context)
		},
	}
}
