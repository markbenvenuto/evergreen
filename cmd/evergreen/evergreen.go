package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/operations"
	_ "github.com/evergreen-ci/evergreen/plugin" // included so that the legacy plugins are built into the binary
	homedir "github.com/mitchellh/go-homedir"
	"github.com/mongodb/grip"
	"github.com/mongodb/grip/level"
	"github.com/mongodb/grip/send"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

func main() {
	// this is where the main action of the program starts. The
	// command line interface is managed by the cli package and
	// its objects/structures. This, plus the basic configuration
	// in buildApp(), is all that's necessary for bootstrapping the
	// environment.
	app := buildApp()
	err := app.Run(os.Args)
	grip.EmergencyFatal(err)
}

func buildApp() *cli.App {
	app := cli.NewApp()
	app.Name = "evergreen"
	app.Usage = "MongoDB Continuous Integration Platform"
	app.Version = evergreen.ClientVersion

	// Register sub-commands here.
	app.Commands = []cli.Command{
		// Version and auto-update
		operations.Version(),
		operations.Update(),
		operations.LocalRun(),

		// Sub-Commands
		operations.Service(),
		operations.Agent(),
		operations.Admin(),
		operations.Host(),
		operations.Volume(),
		operations.Notification(),
		operations.Buildlogger(),

		// Top-level commands.
		operations.Keys(),
		operations.Fetch(),
		operations.Pull(),
		operations.Evaluate(),
		operations.Validate(),
		operations.List(),
		operations.LastGreen(),
		operations.Subscriptions(),
		operations.CommitQueue(),
		operations.Export(),

		// Patch creation and management commands (top-level)
		operations.Patch(),
		operations.PatchFile(),
		operations.PatchList(),
		operations.PatchSetModule(),
		operations.PatchRemoveModule(),
		operations.PatchFinalize(),
		operations.PatchCancel(),
		operations.CreateVersion(),
	}

	userHome, err := homedir.Dir()
	if err != nil {
		// workaround for cygwin if we're on windows but couldn't get a homedir
		if runtime.GOOS == "windows" && len(os.Getenv("HOME")) > 0 {
			userHome = os.Getenv("HOME")
		}
	}
	confPath := filepath.Join(userHome, evergreen.DefaultEvergreenConfig)

	// These are global options. Use this to configure logging or
	// other options independent from specific sub commands.
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "level",
			Value: "info",
			Usage: "Specify lowest visible log level as string: 'emergency|alert|critical|error|warning|notice|info|debug|trace'",
		},
		cli.StringFlag{
			Name:  "conf, config, c",
			Usage: "specify the path for the evergreen CLI config",
			Value: confPath,
		},
	}

	app.Before = func(c *cli.Context) error {
		conf := c.String("conf")
		if conf != "" {
			if err := checkConfPath(conf); err != nil {
				return err
			}
		}
		return loggingSetup(app.Name, c.String("level"))
	}

	return app
}

func checkConfPath(conf string) error {
	if _, err := os.Stat(conf); os.IsNotExist(err) {
		return errors.Errorf("configuration file `%s` does not exist", conf)
	}
	return nil
}

func loggingSetup(name, l string) error {
	if err := grip.SetSender(send.MakeErrorLogger()); err != nil {
		return err
	}
	grip.SetName(name)

	sender := grip.GetSender()
	info := sender.Level()
	info.Threshold = level.FromString(l)

	return sender.SetLevel(info)
}
