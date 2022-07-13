package apps

import (
	"github.com/getoutreach/devenv/cmd/devenv/apps/delete"
	"github.com/getoutreach/devenv/cmd/devenv/apps/deploy"
	"github.com/getoutreach/devenv/cmd/devenv/apps/e2e"
	"github.com/getoutreach/devenv/cmd/devenv/apps/list"
	"github.com/getoutreach/devenv/cmd/devenv/apps/run"
	"github.com/getoutreach/devenv/cmd/devenv/apps/shell"
	"github.com/getoutreach/devenv/cmd/devenv/apps/update"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

// NewCmd returns a cli.Command for the apps command
func NewCmd(log logrus.FieldLogger) *cli.Command {
	desc := "Houses commands for interacting with apps in your developer environment"
	return &cli.Command{
		Name:        "apps",
		Aliases:     []string{"app"},
		Usage:       desc,
		Description: desc,
		Subcommands: []*cli.Command{
			deploy.NewCmd(log),
			update.NewCmd(log),
			delete.NewCmd(log),
			list.NewCmd(log),
			run.NewCmd(log),
			shell.NewCmd(log),
			e2e.NewCmd(log),
		},
	}
}
