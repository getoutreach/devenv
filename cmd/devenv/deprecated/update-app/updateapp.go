// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: See package description

// Package updateapp is a deprecated command
package updateapp

import (
	"github.com/getoutreach/devenv/cmd/devenv/apps/update"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func NewCmdUpdateApp(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "update-app",
		Aliases:     []string{"update-apps"},
		Usage:       "DEPRECATED: Use 'apps update' instead",
		Description: "DEPRECATED: Use 'apps update' instead",
		Hidden:      true,
		Flags:       []cli.Flag{},
		Action: func(c *cli.Context) error {
			log.Warn("DEPRECATED: Use 'apps update' instead")
			o := update.NewOptions(log)
			o.AppName = c.Args().First()
			return o.Run(c.Context)
		},
	}
}
