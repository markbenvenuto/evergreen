package agent

import (
	"context"
	"fmt"
	"io/ioutil"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/command"
	"github.com/evergreen-ci/evergreen/model"
	"github.com/evergreen-ci/evergreen/model/task"
	"github.com/evergreen-ci/evergreen/rest/client"
	"github.com/mongodb/jasper"
	"github.com/mongodb/jasper/mock"
)

const (
	versionId = "v1"
)

// LocalAgentRun - run a file
func LocalAgentRun() {

	// var a *Agent
	// var mockCommunicator *client.Mock
	// var tc *taskContext
	// var canceler context.CancelFunc
	// var tmpDirName string

	var err error
	var a = &Agent{
		opts: Options{
			HostID:     "host",
			HostSecret: "secret",
			StatusPort: 2286,
			LogPrefix:  evergreen.LocalLoggingOverride,
		},
		comm: client.NewMock("url"),
	}

	var mockCommunicator = a.comm.(*client.Mock)
	a.jasper, err = jasper.NewSynchronizedManager(true)
	if err != nil {
		panic(err)
	}

	projYml := `
pre:
  - command: shell.exec
    params:
      script: "echo hi"

tasks:
- name: test_task1
  depends_on: []
  commands:
  - command: shell.exec
    params:
      script: "echo Yeah!"

post:
  - command: shell.exec
    params:
      script: "echo hiiii"
`

	var tc = &taskContext{
		task: client.TaskData{
			ID:     "task_id",
			Secret: "task_secret",
		},
		taskConfig: &model.TaskConfig{
			Project: &model.Project{},
			Task:    &task.Task{},
		},
		taskModel:     &task.Task{},
		runGroupSetup: true,
		oomTracker:    &mock.OOMTracker{},
	}
	p := &model.Project{}

	//var pp ParserProject
	_, err = model.LoadProjectInto([]byte(projYml), "", p)
	if err != nil {
		panic(err)
	}

	var tmpDirName string
	tmpDirName, err = ioutil.TempDir("", "agent-command-suite-")
	if err != nil {
		panic(err)
	}

	tc.taskDirectory = tmpDirName

	tc.taskConfig = &model.TaskConfig{
		BuildVariant: &model.BuildVariant{
			Name: "buildvariant_id",
		},
		Task: &task.Task{
			Id:          "task_id",
			Version:     versionId,
			DisplayName: "test_task1",
		},
		Project: p,
		Timeout: &model.Timeout{IdleTimeoutSecs: 15, ExecTimeoutSecs: 15},
		WorkDir: tc.taskDirectory,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc.logger, err = a.comm.GetLoggerProducer(ctx, tc.task, nil)
	if err != nil {
		panic(err)
	}

	factory, ok := command.GetCommandFactory("setup.initial")
	if !ok {
		panic(err)
	}

	tc.setCurrentCommand(factory())

	sender, err := a.GetSender(ctx, evergreen.LocalLoggingOverride)
	if err != nil {
		panic(err)
	}

	a.SetDefaultLogger(sender)

	fmt.Println("Run pre task commands")
	a.runPreTaskCommands(ctx, tc)

	fmt.Println("Run task commands")
	a.runTaskCommands(ctx, tc)

	fmt.Println("Run post task commands")
	a.runPostTaskCommands(ctx, tc)

	fmt.Println("Close logger")
	_ = tc.logger.Close()
	msgs := mockCommunicator.GetMockMessages()["task_id"]

	for i := 0; i < len(msgs); i++ {
		fmt.Printf("msg: %v\n", msgs[i])
	}

}
