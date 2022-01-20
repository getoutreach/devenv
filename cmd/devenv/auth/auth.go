package auth

import (
	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

//nolint:gochecknoglobals
var (
	auth = `
		Manage devenv auth state.
	`
	authExample = `
		# Refresh Vault operator
		devenv auth refresh
	`
)

// NewCmdAuth returns a new instance of the auth command.
func NewCmdAuth(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "auth",
		Description: cmdutil.NewDescription(auth, authExample),
		Flags:       []cli.Flag{},
		Subcommands: []*cli.Command{
			NewCmdAuthRefresh(log),
		},
	}
}
