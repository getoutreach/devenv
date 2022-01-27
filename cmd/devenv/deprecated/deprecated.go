// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: See package description

// Package deprecated houses all deprecated commands
package deprecated

import (
	deleteapp "github.com/getoutreach/devenv/cmd/devenv/deprecated/delete-app"
	deployapp "github.com/getoutreach/devenv/cmd/devenv/deprecated/deploy-app"
	"github.com/getoutreach/devenv/cmd/devenv/deprecated/top"
	updateapp "github.com/getoutreach/devenv/cmd/devenv/deprecated/update-app"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

// Commands returns all deprecated commands
func Commands(log logrus.FieldLogger) []*cli.Command {
	return []*cli.Command{
		deployapp.NewCmdDeployApp(log),
		deleteapp.NewCmdDeleteApp(log),
		updateapp.NewCmdUpdateApp(log),
		top.NewCmdTop(log),
	}
}
