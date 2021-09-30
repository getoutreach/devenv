// Copyright 2021 Outreach Corporation. All Rights Reserved.

// Description: This file is the entrypoint for the devenv CLI
// command for devenv.
// Managed: true

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/getoutreach/devenv/cmd/devenv/completion"
	deleteapp "github.com/getoutreach/devenv/cmd/devenv/delete-app"
	deployapp "github.com/getoutreach/devenv/cmd/devenv/deploy-app"
	"github.com/getoutreach/devenv/cmd/devenv/destroy"
	"github.com/getoutreach/devenv/cmd/devenv/expose"
	"github.com/getoutreach/devenv/cmd/devenv/kubectl"
	localapp "github.com/getoutreach/devenv/cmd/devenv/local-app"
	"github.com/getoutreach/devenv/cmd/devenv/provision"
	"github.com/getoutreach/devenv/cmd/devenv/snapshot"
	"github.com/getoutreach/devenv/cmd/devenv/start"
	"github.com/getoutreach/devenv/cmd/devenv/status"
	"github.com/getoutreach/devenv/cmd/devenv/stop"
	"github.com/getoutreach/devenv/cmd/devenv/top"
	"github.com/getoutreach/devenv/cmd/devenv/tunnel"
	updateapp "github.com/getoutreach/devenv/cmd/devenv/update-app"
	"github.com/getoutreach/devenv/pkg/cmdutil"
	oapp "github.com/getoutreach/gobox/pkg/app"
	"github.com/getoutreach/gobox/pkg/box"
	"github.com/getoutreach/gobox/pkg/cfg"
	olog "github.com/getoutreach/gobox/pkg/log"
	"github.com/getoutreach/gobox/pkg/secrets"
	"github.com/getoutreach/gobox/pkg/trace"
	"github.com/getoutreach/gobox/pkg/updater"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v2"

	// Place any extra imports for your startup code here
	///Block(imports)
	cmdcontext "github.com/getoutreach/devenv/cmd/devenv/context"
	///EndBlock(imports)
)

// HoneycombTracingKey gets set by the Makefile at compile-time which is pulled
// down by devconfig.sh.
var HoneycombTracingKey = "NOTSET" //nolint:gochecknoglobals // Why: We can't compile in things as a const.

///Block(global)
var defaultBoxes = []string{
	"git@github.com:getoutreach/box",
}

///EndBlock(global)

// overrideConfigLoaders fakes certain parts of the config that usually get pulled
// in via mechanisms that don't make sense to use in CLIs.
func overrideConfigLoaders() {
	var fallbackSecretLookup func(context.Context, string) ([]byte, error)
	fallbackSecretLookup = secrets.SetDevLookup(func(ctx context.Context, key string) ([]byte, error) {
		if key == "APIKey" {
			return []byte(HoneycombTracingKey), nil
		}

		return fallbackSecretLookup(ctx, key)
	})

	olog.SetOutput(ioutil.Discard)

	fallbackConfigReader := cfg.DefaultReader()
	cfg.SetDefaultReader(func(fileName string) ([]byte, error) {
		if fileName == "trace.yaml" {
			traceConfig := &trace.Config{
				Honeycomb: trace.Honeycomb{
					Enabled: true,
					APIHost: "https://api.honeycomb.io",
					APIKey: cfg.Secret{
						Path: "APIKey",
					},
					///Block(dataset)
					Dataset: "dev-tooling-team",
					///EndBlock(dataset)
					SamplePercent: 100,
				},
			}
			b, err := yaml.Marshal(&traceConfig)
			if err != nil {
				panic(err)
			}
			return b, nil
		}

		return fallbackConfigReader(fileName)
	})
}

func main() { //nolint:funlen // Why: We can't dwindle this down anymore without adding complexity.
	ctx, cancel := context.WithCancel(context.Background())
	log := logrus.New()

	exitCode := 0
	cli.OsExiter = func(code int) { exitCode = code }

	oapp.SetName("devenv")
	overrideConfigLoaders()

	// handle ^C gracefully
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		out := <-c
		log.Debugf("shutting down: %v", out)
		cancel()
	}()

	if err := trace.InitTracer(ctx, "devenv"); err != nil {
		log.WithError(err).Debugf("failed to start tracer")
	}
	ctx = trace.StartTrace(ctx, "devenv")

	///Block(init)
	///EndBlock(init)

	// optional cleanup function for use after NeedsUpdate
	// this function is re-defined later when NeedsUpdate is true
	cleanup := func() {}

	exit := func() {
		trace.End(ctx)
		trace.CloseTracer(ctx)
		///Block(exit)
		///EndBlock(exit)
		cleanup()
		os.Exit(exitCode)
	}
	defer exit()

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("stacktrace from panic: \n" + string(debug.Stack()))
		}
	}()

	ctx = trace.StartCall(ctx, "main")
	defer trace.EndCall(ctx)

	app := cli.App{
		Version: oapp.Version,
		Name:    "devenv",
		///Block(app)
		Description: cmdutil.Normalize(`
			devenv manages your Outreach developer environment
		`),
		EnableBashCompletion: true,
		///EndBlock(app)
	}
	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:  "skip-update",
			Usage: "skips the updater check",
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "enables debug logging for all components (i.e updater)",
		},
		&cli.BoolFlag{
			Name:  "enable-prereleases",
			Usage: "Enable considering pre-releases when checking for updates",
		},
		&cli.BoolFlag{
			Name:  "force-update-check",
			Usage: "Force checking for an update",
		},
		///Block(flags)
		///EndBlock(flags)
	}
	app.Commands = []*cli.Command{
		///Block(commands)
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
		///EndBlock(commands)
	}

	app.Before = func(c *cli.Context) error {
		///Block(before)
		// ensure our storage directory exists
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return errors.Wrap(err, "failed to get user home dir")
		}

		err = os.MkdirAll(filepath.Join(homeDir, ".local", "dev-environment"), 0755)
		if err != nil {
			return err
		}

		_, err = box.EnsureBoxWithOptions(ctx, box.WithLogger(log), box.WithMinVersion(1))
		if err != nil {
			return err
		}
		///EndBlock(before)

		// add info to the root trace about our command and args
		cargs := c.Args().Slice()
		command := ""
		args := make([]string, 0)
		if len(cargs) > 0 {
			command = c.Args().Slice()[0]
		}
		if len(cargs) > 1 {
			args = cargs[1:]
		}

		userName := ""
		if u, err := user.Current(); err == nil {
			userName = u.Username
		}
		trace.AddInfo(ctx, olog.F{
			"devenv.subcommand": command,
			"devenv.args":       strings.Join(args, " "),
			"os.user":           userName,
			"os.name":           runtime.GOOS,
			///Block(trace)
			///EndBlock(trace)
		})

		// restart when updated
		traceCtx := trace.StartCall(c.Context, "updater.NeedsUpdate")
		defer trace.EndCall(traceCtx)

		// restart when updated
		if updater.NeedsUpdate(traceCtx, log, "", oapp.Version, c.Bool("skip-update"), c.Bool("debug"), c.Bool("enable-prereleases"), c.Bool("force-update-check")) {
			// replace running process(execve)
			switch runtime.GOOS {
			case "linux", "darwin":
				cleanup = func() {
					log.Infof("devenv has been updated")
					osarg0 := os.Args[0]
					err := syscall.Exec(osarg0, os.Args, os.Environ())
					if err != nil {
						log.WithError(err).Error("failed to execute updated binary")
					}
				}
			default:
				log.Infof("devenv has been updated, please re-run your command")
			}

			exitCode = 5
			trace.EndCall(traceCtx)
			exit()
			return nil
		}

		return nil
	}

	if err := app.RunContext(ctx, os.Args); err != nil {
		log.Errorf("failed to run: %v", err)
		//nolint:errcheck // Why: We're attaching the error to the trace.
		trace.SetCallStatus(ctx, err)
		exitCode = 1

		return
	}
}
