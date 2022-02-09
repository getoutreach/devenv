package apps

import (
	"github.com/getoutreach/devenv/cmd/devenv/apps/delete"
	"github.com/getoutreach/devenv/cmd/devenv/apps/deploy"
	"github.com/getoutreach/devenv/cmd/devenv/apps/list"
	"github.com/getoutreach/devenv/cmd/devenv/apps/update"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

// NewCmd returns a cli.Command for the apps command
func NewCmd(log logrus.FieldLogger) *cli.Command {
	desc := "Houses commands for interacting with apps in your developer environment"
	return &cli.Command{
		Name:        "apps",
		Usage:       desc,
		Description: desc,
		Subcommands: []*cli.Command{
			deploy.NewCmd(log),
			update.NewCmd(log),
			delete.NewCmd(log),
			list.NewCmd(log),
		},
	}
}