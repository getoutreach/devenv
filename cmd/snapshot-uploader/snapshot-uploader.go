// Copyright 2021 Outreach Corporation. All Rights Reserved.

// Description: This file is the entrypoint for the snapshot-uploader CLI
// command for devenv.
// Managed: true

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	oapp "github.com/getoutreach/gobox/pkg/app"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	// Place any extra imports for your startup code here
	///Block(imports)
	///EndBlock(imports)
)

///Block(global)
///EndBlock(global)

func main() { //nolint:funlen // Why: We can't dwindle this down anymore without adding complexity.
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("stacktrace from panic: \n" + string(debug.Stack()))
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	log := logrus.New()

	exitCode := 0
	cli.OsExiter = func(code int) { exitCode = code }
	exit := func() {
		os.Exit(exitCode)
	}
	defer exit()

	oapp.SetName("snapshot-uploader")

	// handle ^C gracefully
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		out := <-c
		log.Debugf("shutting down: %v", out)
		cancel()
	}()

	///Block(init)
	///EndBlock(init)

	app := cli.App{
		Version: oapp.Version,
		Name:    "snapshot-uploader",
		///Block(app)
		Action: func(c *cli.Context) error {
			return (&SnapshotUploader{}).StartFromEnv(ctx, log)
		},
		///EndBlock(app)
	}
	app.Commands = []*cli.Command{
		///Block(commands)
		///EndBlock(commands)
	}

	if err := app.RunContext(ctx, os.Args); err != nil {
		log.Errorf("failed to run: %v", err)
		exitCode = 1
		return
	}
}
