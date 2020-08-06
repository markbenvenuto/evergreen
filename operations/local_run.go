package operations

import (
	"fmt"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/agent"
	"github.com/urfave/cli"
)

// LocalRun local run test command
func LocalRun() cli.Command {
	return cli.Command{
		Name:    "local_run",
		Aliases: []string{"v"},
		Usage:   "prints the revision of the current binary",
		Action: func(c *cli.Context) error {
			fmt.Println(evergreen.ClientVersion)
			agent.LocalAgentRun()
			return nil
		},
	}
}
