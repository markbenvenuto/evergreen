package operations

import (
	"fmt"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/agent"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

// LocalRun local run test command
func LocalRun() cli.Command {
	const (
		keyNameFlagName      = "name"
		keyFileFlagName      = "file"
		buildVariantFlagName = "build"
	)

	return cli.Command{
		Name:    "local_run",
		Aliases: []string{"v"},
		Usage:   "prints the revision of the current binary",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  keyNameFlagName,
				Usage: "task id to run",
			},
			cli.StringFlag{
				Name:  keyFileFlagName,
				Usage: "specify the path of an evergreen file",
			},
			cli.StringFlag{
				Name:  buildVariantFlagName,
				Usage: "build variant",
			},
		},
		Before: mergeBeforeFuncs(
			setPlainLogger,
			requireFileExists(keyFileFlagName),
			func(c *cli.Context) error {
				keyName := c.String(keyNameFlagName)
				if keyName == "" {
					return errors.New("key name cannot be empty")
				}
				keyName = c.String(buildVariantFlagName)
				if keyName == "" {
					return errors.New("build variant name cannot be empty")
				}
				return nil
			}),

		Action: func(c *cli.Context) error {
			fmt.Println(evergreen.ClientVersion)

			//confPath := c.Parent().Parent().String(confFlagName)
			keyName := c.String(keyNameFlagName)
			keyFile := c.String(keyFileFlagName)
			buildVariant := c.String(buildVariantFlagName)

			agent.LocalAgentRun(keyFile, keyName, buildVariant)
			return nil
		},
	}
}
