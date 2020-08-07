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
		keyNameFlagName = "name"
		keyFileFlagName = "file"
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
		},
		Before: mergeBeforeFuncs(
			setPlainLogger,
			requireFileExists(keyFileFlagName),
			func(c *cli.Context) error {
				keyName := c.String(keyNameFlagName)
				if keyName == "" {
					return errors.New("key name cannot be empty")
				}
				return nil
			}),

		Action: func(c *cli.Context) error {
			fmt.Println(evergreen.ClientVersion)

			//confPath := c.Parent().Parent().String(confFlagName)
			keyName := c.String(keyNameFlagName)
			keyFile := c.String(keyFileFlagName)

			agent.LocalAgentRun(keyFile, keyName)
			return nil
		},
	}
}
