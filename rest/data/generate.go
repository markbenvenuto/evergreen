package data

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/evergreen-ci/evergreen/model/task"
	"github.com/evergreen-ci/evergreen/units"
	"github.com/mongodb/amboy"
	"github.com/mongodb/grip"
	"github.com/mongodb/grip/message"
	"github.com/pkg/errors"
)

type GenerateConnector struct{}

// GenerateTasks parses JSON files for `generate.tasks` and creates the new builds and tasks.
func (gc *GenerateConnector) GenerateTasks(ctx context.Context, taskID string, jsonBytes []json.RawMessage, group amboy.QueueGroup) error {
	t, err := task.FindOneId(taskID)
	if err != nil {
		return errors.Wrapf(err, "problem finding task %s", taskID)
	}

	// in the future this operation should receive a context tied
	// to the lifetime of the application (e.g. env.Context())
	// rather than a context tied to the lifetime of the request
	// (e.g. ctx above.)
	q, err := group.Get(context.TODO(), t.Version)
	if err != nil {
		return errors.Wrapf(err, "problem getting queue for version %s", t.Version)
	}
	if err = t.SetGeneratedJSON(jsonBytes); err != nil {
		return errors.Wrapf(err, "problem setting generated json in task document for %s", t.Id)
	}

	// Make sure legacy attempt does not exist. This could be a task restart.
	if t.GenerateAttempt == 0 {
		jobID := fmt.Sprintf("generate-tasks-%s", taskID)
		_, exists := q.Get(ctx, jobID)
		if exists {
			return nil
		}
	}

	if err = t.IncrementGenerateAttempt(); err != nil {
		return errors.Wrapf(err, "problem incrementing generator for %s", t.Id)
	}
	err = q.Put(ctx, units.NewGenerateTasksJob(taskID, t.GenerateAttempt))
	grip.Debug(message.WrapError(err, message.Fields{
		"message": "problem saving new generate tasks job for task",
		"task_id": taskID}))
	return nil
}

func (gc *GenerateConnector) GeneratePoll(ctx context.Context, taskID string, group amboy.QueueGroup) (bool, []string, error) {
	t, err := task.FindOneId(taskID)
	if err != nil {
		return false, nil, errors.Wrapf(err, "problem finding task %s", taskID)
	}

	// in the future this operation should receive a context tied
	// to the lifetime of the application (e.g. env.Context())
	// rather than a context tied to the lifetime of the request
	// (e.g. ctx above.)
	q, err := group.Get(context.TODO(), t.Version)
	if err != nil {
		return false, nil, errors.Wrapf(err, "problem getting queue for version %s", t.Version)
	}

	var jobID string
	var j amboy.Job
	var exists bool
	for {
		if ctx.Err() != nil {
			return false, []string{}, errors.WithStack(errors.New("context canceled"))
		}
		if t.GenerateAttempt == 0 {
			jobID = fmt.Sprintf("generate-tasks-%s", taskID) // legacy job id
		} else {
			jobID = fmt.Sprintf("generate-tasks-%s-%d", taskID, t.GenerateAttempt)
		}
		generateAttempt := t.GenerateAttempt
		j, exists = q.Get(ctx, jobID)
		if !exists {
			return false, nil, errors.Errorf("task %s not in queue", taskID)
		}
		t, err := task.FindOneId(taskID)
		if err != nil {
			return false, nil, errors.Wrapf(err, "problem finding task %s", taskID)
		}
		// If attempt has incremented, the requeue job has raced with the poll job. Try again. Otherwise break.
		if generateAttempt == t.GenerateAttempt {
			break
		}
	}
	return j.Status().Completed, j.Status().Errors, nil
}

type MockGenerateConnector struct{}

func (gc *MockGenerateConnector) GenerateTasks(ctx context.Context, taskID string, jsonBytes []json.RawMessage, group amboy.QueueGroup) error {
	return nil
}

func (gc *MockGenerateConnector) GeneratePoll(ctx context.Context, taskID string, queue amboy.QueueGroup) (bool, []string, error) {
	// no task
	if taskID == "0" {
		return false, nil, errors.New("No task called '0'")
	}
	// finished
	if taskID == "1" {
		return true, nil, nil
	}
	// not yet finished
	return false, nil, nil
}
