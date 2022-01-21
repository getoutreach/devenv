package deployapp

import (
	"github.com/getoutreach/devenv/cmd/devenv/apps/deploy"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func NewCmdDeployApp(log logrus.FieldLogger) *cli.Command {
	return &cli.Command{
		Name:        "deploy-app",
		Usage:       "DEPRECATED: Use 'apps deploy' instead",
		Description: "DEPRECATED: Use 'apps deploy' instead",
		Hidden:      true,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:   "local",
				Hidden: true,
				Usage:  "Deploy an application from local disk --local <path>",
			},
		},
		Action: func(c *cli.Context) error {
			log.Warn("DEPRECATED: Use 'apps deploy' instead")

			cmd, err := deploy.NewOptions(log)
			if err != nil {
				return err
			}
			cmd.App = c.Args().First()
			return cmd.Run(c.Context)
		},
	}
}
