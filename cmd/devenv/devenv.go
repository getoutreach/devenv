// Copyright 2022 Outreach Corporation. All Rights Reserved.

// Description: This file is the entrypoint for the devenv CLI
// command for devenv.
// Managed: true

package main

import (
	"context"
	"os"
	"path/filepath"

	oapp "github.com/getoutreach/gobox/pkg/app"
	"github.com/getoutreach/gobox/pkg/box"
	gcli "github.com/getoutreach/gobox/pkg/cli"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	// Place any extra imports for your startup code here
	///Block(imports)
	"github.com/getoutreach/devenv/cmd/devenv/auth"
	"github.com/getoutreach/devenv/cmd/devenv/completion"
	cmdcontext "github.com/getoutreach/devenv/cmd/devenv/context"
	deleteapp "github.com/getoutreach/devenv/cmd/devenv/delete-app"
	deployapp "github.com/getoutreach/devenv/cmd/devenv/deploy-app"
	"github.com/getoutreach/devenv/cmd/devenv/destroy"
	"github.com/getoutreach/devenv/cmd/devenv/expose"
	"github.com/getoutreach/devenv/cmd/devenv/kubectl"
	localapp "github.com/getoutreach/devenv/cmd/devenv/local-app"
	"github.com/getoutreach/devenv/cmd/devenv/provision"
	"github.com/getoutreach/devenv/cmd/devenv/registry"
	"github.com/getoutreach/devenv/cmd/devenv/snapshot"
	"github.com/getoutreach/devenv/cmd/devenv/start"
	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/getoutreach/devenv/cmd/devenv/stop"
	"github.com/getoutreach/devenv/cmd/devenv/top"
	"github.com/getoutreach/devenv/cmd/devenv/tunnel"
	updateapp "github.com/getoutreach/devenv/cmd/devenv/update-app"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	///EndBlock(imports)
)

// HoneycombTracingKey gets set by the Makefile at compile-time which is pulled
// down by devconfig.sh.
var HoneycombTracingKey = "NOTSET" //nolint:gochecknoglobals // Why: We can't compile in things as a const.

///Block(honeycombDataset)
const HoneycombDataset = ""

///EndBlock(honeycombDataset)

///Block(global)
///EndBlock(global)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	log := logrus.New()

	///Block(init)
	///EndBlock(init)

	app := cli.App{
		Version: oapp.Version,
		Name:    "devenv",
		///Block(app)
		Description: cmdutil.Normalize(`
			devenv manages your Outreach developer environment
		`),
		EnableBashCompletion: true,
		Before: func(c *cli.Context) error {
			// NOTE: You would think you can check the the c.Command.Name == "completion"
			// in before to see if that command is being run, you would be wrong
			// Using the args passed in to see if the completion command
			// was provided. Other global flags are just ignored.
			if c.Args().First() == "completion" {
				return nil
			}

			homeDir, err := os.UserHomeDir()
			if err != nil {
				return errors.Wrap(err, "failed to get user home dir")
			}

			err = os.MkdirAll(filepath.Join(homeDir, ".local", "dev-environment"), 0755)
			if err != nil {
				return err
			}

			_, err = box.EnsureBoxWithOptions(ctx, box.WithLogger(log), box.WithMinVersion(2))
			return err
		},
		///EndBlock(app)
	}
	app.Flags = []cli.Flag{
		///Block(flags)
		///EndBlock(flags)
	}
	app.Commands = []*cli.Command{
		///Block(commands)
		auth.NewCmdAuth(log),
		provision.NewCmdProvision(log),
		deployapp.NewCmdDeployApp(log),
		deleteapp.NewCmdDeleteApp(log),
		destroy.NewCmdDestroy(log),
		status.NewCmdStatus(log),
		localapp.NewCmdLocalApp(log),
		tunnel.NewCmdTunnel(log),
		kubectl.NewCmdKubectl(log),
		start.NewCmdStart(log),
		stop.NewCmdStop(log),
		completion.NewCmdCompletion(),
		top.NewCmdTop(log),
		updateapp.NewCmdUpdateApp(log),
		snapshot.NewCmdSnapshot(log),
		expose.NewCmdExpose(log),
		cmdcontext.NewCmdContext(log),
		registry.NewCmdRegistry(log),
		///EndBlock(commands)
	}

	///Block(postApp)
	///EndBlock(postApp)

	// Insert global flags, tracing, updating and start the application.
	gcli.HookInUrfaveCLI(ctx, cancel, &app, log, HoneycombTracingKey, HoneycombDataset)
}
