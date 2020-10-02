package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/apimodels"
	"github.com/evergreen-ci/evergreen/cloud"
	"github.com/evergreen-ci/evergreen/command"
	"github.com/evergreen-ci/evergreen/model"
	"github.com/evergreen-ci/evergreen/model/artifact"
	"github.com/evergreen-ci/evergreen/model/distro"
	"github.com/evergreen-ci/evergreen/model/event"
	"github.com/evergreen-ci/evergreen/model/host"
	"github.com/evergreen-ci/evergreen/model/manifest"
	patchmodel "github.com/evergreen-ci/evergreen/model/patch"

	serviceModel "github.com/evergreen-ci/evergreen/model"
	"github.com/evergreen-ci/evergreen/model/task"
	"github.com/evergreen-ci/evergreen/rest/client"
	restmodel "github.com/evergreen-ci/evergreen/rest/model"
	"github.com/evergreen-ci/evergreen/util"
	"github.com/mongodb/grip"
	"github.com/mongodb/jasper"
	"github.com/mongodb/jasper/mock"
	"github.com/pkg/errors"
)

const (
	versionId = "v1"
)

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// LocalRunMock mocks EvergreenREST for testing.
type LocalRunMock struct {
	maxAttempts  int
	timeoutStart time.Duration
	timeoutMax   time.Duration
	serverURL    string

	// these fields have setters
	hostID     string
	hostSecret string
	apiUser    string
	apiKey     string

	// mock behavior
	NextTaskShouldFail          bool
	NextTaskShouldConflict      bool
	GetPatchFileShouldFail      bool
	loggingShouldFail           bool
	NextTaskResponse            *apimodels.NextTaskResponse
	NextTaskIsNil               bool
	EndTaskResponse             *apimodels.EndTaskResponse
	EndTaskShouldFail           bool
	EndTaskResult               endTaskResult
	ShellExecFilename           string
	TimeoutFilename             string
	HeartbeatShouldAbort        bool
	HeartbeatShouldErr          bool
	HeartbeatShouldSometimesErr bool
	TaskExecution               int
	GetSubscriptionsFail        bool
	CreatedHost                 apimodels.CreateHost

	AttachedFiles    map[string][]*artifact.File
	LogID            string
	LocalTestResults *task.LocalTestResults
	TestLogs         []*serviceModel.TestLog
	TestLogCount     int

	// data collected by mocked methods
	logMessages     map[string][]apimodels.LogMessage
	PatchFiles      map[string]string
	keyVal          map[string]*serviceModel.KeyVal
	LastMessageSent time.Time

	mu sync.RWMutex
}

type endTaskResult struct {
	Detail   *apimodels.TaskEndDetail
	TaskData client.TaskData
}

const (
	defaultMaxAttempts  = 10
	defaultTimeoutStart = time.Second * 2
	defaultTimeoutMax   = time.Minute * 10
	heartbeatTimeout    = time.Minute * 1

	defaultLogBufferTime = 15 * time.Second
	defaultLogBufferSize = 1000
)

// NewMock returns a Communicator for testing.
func NewLocalRunMock(serverURL string) *LocalRunMock {
	return &LocalRunMock{
		maxAttempts:   defaultMaxAttempts,
		timeoutStart:  defaultTimeoutStart,
		timeoutMax:    defaultTimeoutMax,
		logMessages:   make(map[string][]apimodels.LogMessage),
		PatchFiles:    make(map[string]string),
		keyVal:        make(map[string]*serviceModel.KeyVal),
		AttachedFiles: make(map[string][]*artifact.File),
		serverURL:     serverURL,
	}
}

func (c *LocalRunMock) Close() {}

func (c *LocalRunMock) LastMessageAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.LastMessageSent
}

func (c *LocalRunMock) UpdateLastMessageTime() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastMessageSent = time.Now()
}

// nolint
func (c *LocalRunMock) SetTimeoutStart(timeoutStart time.Duration) { c.timeoutStart = timeoutStart }
func (c *LocalRunMock) SetTimeoutMax(timeoutMax time.Duration)     { c.timeoutMax = timeoutMax }
func (c *LocalRunMock) SetMaxAttempts(attempts int)                { c.maxAttempts = attempts }
func (c *LocalRunMock) SetHostID(hostID string)                    { c.hostID = hostID }
func (c *LocalRunMock) SetHostSecret(hostSecret string)            { c.hostSecret = hostSecret }
func (c *LocalRunMock) GetHostID() string                          { return c.hostID }
func (c *LocalRunMock) GetHostSecret() string                      { return c.hostSecret }
func (c *LocalRunMock) SetAPIUser(apiUser string)                  { c.apiUser = apiUser }
func (c *LocalRunMock) SetAPIKey(apiKey string)                    { c.apiKey = apiKey }

// nolint
func (c *LocalRunMock) GetAgentSetupData(ctx context.Context) (*apimodels.AgentSetupData, error) {
	return &apimodels.AgentSetupData{}, nil
}

// nolint
func (c *LocalRunMock) StartTask(ctx context.Context, td client.TaskData) error { return nil }

// EndTask returns a mock EndTaskResponse.
func (c *LocalRunMock) EndTask(ctx context.Context, detail *apimodels.TaskEndDetail, td client.TaskData) (*apimodels.EndTaskResponse, error) {
	if c.EndTaskShouldFail {
		return nil, errors.New("end task should fail")
	}
	if c.EndTaskResponse != nil {
		return c.EndTaskResponse, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.EndTaskResult.Detail = detail
	c.EndTaskResult.TaskData = td
	return &apimodels.EndTaskResponse{}, nil
}

// GetEndTaskDetail returns the task end detail saved in the mock.
func (c *LocalRunMock) GetEndTaskDetail() *apimodels.TaskEndDetail {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.EndTaskResult.Detail
}

// GetTask returns a mock Task.
func (c *LocalRunMock) GetTask(ctx context.Context, td client.TaskData) (*task.Task, error) {
	return &task.Task{
		Id:           "mock_task_id",
		Secret:       "mock_task_secret",
		BuildVariant: "mock_build_variant",
		DisplayName:  "build",
		Execution:    c.TaskExecution,
		Version:      "mock_version_id",
	}, nil
}

// GetProjectRef returns a mock ProjectRef.
func (c *LocalRunMock) GetProjectRef(ctx context.Context, td client.TaskData) (*serviceModel.ProjectRef, error) {
	return &serviceModel.ProjectRef{
		Owner:  "mock_owner",
		Repo:   "mock_repo",
		Branch: "mock_branch",
	}, nil
}

// GetDistro returns a mock Distro.
func (c *LocalRunMock) GetDistro(ctx context.Context, td client.TaskData) (*distro.Distro, error) {
	return &distro.Distro{
		Id:      "mock_distro_id",
		WorkDir: ".",
	}, nil
}

func (c *LocalRunMock) GetProject(ctx context.Context, td client.TaskData) (*serviceModel.Project, error) {
	var err error
	var data []byte
	_, file, _, _ := runtime.Caller(0)

	data, err = ioutil.ReadFile(filepath.Join(filepath.Dir(file), "testdata", fmt.Sprintf("%s.yaml", td.ID)))
	if err != nil {
		grip.Error(err)
	}
	proj := &serviceModel.Project{}
	_, err = serviceModel.LoadProjectInto(data, "", proj)
	return proj, err
}

func (c *LocalRunMock) GetExpansions(ctx context.Context, taskData client.TaskData) (util.Expansions, error) {
	e := util.Expansions{
		"foo": "bar",
	}
	return e, nil
}

// Heartbeat returns false, which indicates the heartbeat has succeeded.
func (c *LocalRunMock) Heartbeat(ctx context.Context, td client.TaskData) (bool, error) {
	if c.HeartbeatShouldAbort {
		return true, nil
	}
	if c.HeartbeatShouldSometimesErr {
		if c.HeartbeatShouldErr {
			c.HeartbeatShouldErr = false
			return false, errors.New("mock heartbeat error")
		}
		c.HeartbeatShouldErr = true
		return false, nil
	}
	if c.HeartbeatShouldErr {
		return false, errors.New("mock heartbeat error")
	}
	return false, nil
}

// FetchExpansionVars returns a mock ExpansionVars.
func (c *LocalRunMock) FetchExpansionVars(ctx context.Context, td client.TaskData) (*apimodels.ExpansionVars, error) {
	return &apimodels.ExpansionVars{
		Vars: map[string]string{
			"shellexec_fn":   c.ShellExecFilename,
			"timeout_fn":     c.TimeoutFilename,
			"my_new_timeout": "2",
		},
	}, nil
}

// GetNextTask returns a mock NextTaskResponse.
func (c *LocalRunMock) GetNextTask(ctx context.Context, details *apimodels.GetNextTaskDetails) (*apimodels.NextTaskResponse, error) {
	if c.NextTaskIsNil {
		return &apimodels.NextTaskResponse{
				TaskId: "",
			},
			nil
	}
	if c.NextTaskShouldFail {
		return nil, errors.New("NextTaskShouldFail is true")
	}
	if c.NextTaskShouldConflict {
		return nil, errors.WithStack(client.HTTPConflictError)
	}
	if c.NextTaskResponse != nil {
		return c.NextTaskResponse, nil
	}

	return &apimodels.NextTaskResponse{
		TaskId:     "mock_task_id",
		TaskSecret: "mock_task_secret",
		ShouldExit: false,
	}, nil
}

// GetBuildloggerInfo returns mock buildlogger service information.
func (c *LocalRunMock) GetBuildloggerInfo(ctx context.Context) (*apimodels.BuildloggerInfo, error) {
	return &apimodels.BuildloggerInfo{
		BaseURL:  "base_url",
		RPCPort:  "1000",
		Username: "user",
		Password: "password",
	}, nil
}

// SendTaskLogMessages posts tasks messages to the api server
func (c *LocalRunMock) SendLogMessages(ctx context.Context, td client.TaskData, msgs []apimodels.LogMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.loggingShouldFail {
		return errors.New("logging failed")
	}

	c.logMessages[td.ID] = append(c.logMessages[td.ID], msgs...)

	for i := 0; i < len(msgs); i++ {
		//fmt.Printf("msg: %v\n", msgs[i])
		fmt.Printf("%v\n", msgs[i].Message)
	}

	return nil
}

// GetMockMessages returns the mock's logs.
func (c *LocalRunMock) GetMockMessages() map[string][]apimodels.LogMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := map[string][]apimodels.LogMessage{}
	for k, v := range c.logMessages {
		out[k] = []apimodels.LogMessage{}
		for _, i := range v {
			new := apimodels.LogMessage{
				Type:      i.Type,
				Severity:  i.Severity,
				Message:   i.Message,
				Timestamp: i.Timestamp,
				Version:   i.Version,
			}
			out[k] = append(out[k], new)
		}
	}

	return out
}

// GetLoggerProducer constructs a single channel log producer.
func (c *LocalRunMock) GetLoggerProducer(ctx context.Context, td client.TaskData, config *client.LoggerConfig) (client.LoggerProducer, error) {
	return client.NewSingleChannelLogHarness(td.ID, client.NewEvergreenLogSender2(ctx, c, apimodels.AgentLogPrefix, td, defaultLogBufferSize, defaultLogBufferTime)), nil
}
func (c *LocalRunMock) GetLoggerMetadata() client.LoggerMetadata {
	return client.LoggerMetadata{
		Agent: []client.LogkeeperMetadata{{
			Build: "build1",
			Test:  "test1",
		}},
		System: []client.LogkeeperMetadata{{
			Build: "build1",
			Test:  "test2",
		}},
		Task: []client.LogkeeperMetadata{{
			Build: "build1",
			Test:  "test3",
		}},
	}
}

func (c *LocalRunMock) GetPatchFile(ctx context.Context, td client.TaskData, patchFileID string) (string, error) {
	if c.GetPatchFileShouldFail {
		return "", errors.New("operation run in fail mode.")
	}

	out, ok := c.PatchFiles[patchFileID]

	if !ok {
		return "", errors.Errorf("patch file %s not found", patchFileID)
	}

	return out, nil
}

func (c *LocalRunMock) GetTaskPatch(ctx context.Context, td client.TaskData) (*patchmodel.Patch, error) {
	patch, ok := ctx.Value("patch").(*patchmodel.Patch)
	if !ok {
		return &patchmodel.Patch{}, nil
	}

	return patch, nil
}

// CreateSpawnHost will return a mock host that would have been intended
func (*LocalRunMock) CreateSpawnHost(ctx context.Context, spawnRequest *restmodel.HostRequestOptions) (*restmodel.APIHost, error) {
	mockHost := &restmodel.APIHost{
		Id:      restmodel.ToStringPtr("mock_host_id"),
		HostURL: restmodel.ToStringPtr("mock_url"),
		Distro: restmodel.DistroInfo{
			Id:       restmodel.ToStringPtr(spawnRequest.DistroID),
			Provider: restmodel.ToStringPtr(evergreen.ProviderNameMock),
		},
		Provider:     restmodel.ToStringPtr(evergreen.ProviderNameMock),
		Status:       restmodel.ToStringPtr(evergreen.HostUninitialized),
		StartedBy:    restmodel.ToStringPtr("mock_user"),
		UserHost:     true,
		Provisioned:  false,
		InstanceTags: spawnRequest.InstanceTags,
		InstanceType: restmodel.ToStringPtr(spawnRequest.InstanceType),
	}
	return mockHost, nil
}

func (*LocalRunMock) GetSpawnHost(ctx context.Context, hostID string) (*restmodel.APIHost, error) {
	return nil, errors.New("(*LocalRunMock) GetSpawnHost is not implemented")
}

func (*LocalRunMock) ModifySpawnHost(ctx context.Context, hostID string, changes host.HostModifyOptions) error {
	return errors.New("(*LocalRunMock) ModifySpawnHost is not implemented")
}

func (*LocalRunMock) TerminateSpawnHost(ctx context.Context, hostID string) error {
	return errors.New("(*LocalRunMock) TerminateSpawnHost is not implemented")
}

func (*LocalRunMock) StopSpawnHost(context.Context, string, string, bool) error {
	return errors.New("(*LocalRunMock) StopSpawnHost is not implemented")
}

func (*LocalRunMock) StartSpawnHost(context.Context, string, string, bool) error {
	return errors.New("(*LocalRunMock) StartSpawnHost is not implemented")
}

func (*LocalRunMock) ChangeSpawnHostPassword(context.Context, string, string) error {
	return errors.New("(*LocalRunMock) ChangeSpawnHostPassword is not implemented")
}

func (*LocalRunMock) ExtendSpawnHostExpiration(context.Context, string, int) error {
	return errors.New("(*LocalRunMock) ExtendSpawnHostExpiration is not implemented")
}

func (*LocalRunMock) AttachVolume(context.Context, string, *host.VolumeAttachment) error {
	return errors.New("(*LocalRunMock) AttachVolume is not implemented")
}

func (*LocalRunMock) DetachVolume(context.Context, string, string) error {
	return errors.New("(*LocalRunMock) DetachVolume is not implemented")
}

func (*LocalRunMock) CreateVolume(context.Context, *host.Volume) (*restmodel.APIVolume, error) {
	return nil, errors.New("(*LocalRunMock) CreateVolume is not implemented")
}

func (*LocalRunMock) DeleteVolume(context.Context, string) error {
	return errors.New("(*LocalRunMock) DeleteVolume is not implemented")
}

func (*LocalRunMock) ModifyVolume(context.Context, string, *restmodel.VolumeModifyOptions) error {
	return errors.New("(*LocalRunMock) ModifyVolume is not implemented")
}

func (*LocalRunMock) GetVolumesByUser(context.Context) ([]restmodel.APIVolume, error) {
	return nil, errors.New("(*LocalRunMock) GetVolumesByUser is not implemented")
}

func (c *LocalRunMock) GetVolume(context.Context, string) (*restmodel.APIVolume, error) {
	return nil, errors.New("(*LocalRunMock) GetVolume is not implemented")
}

// GetHosts will return an array with a single mock host
func (c *LocalRunMock) GetHosts(ctx context.Context, data restmodel.APIHostParams) ([]*restmodel.APIHost, error) {
	spawnRequest := &restmodel.HostRequestOptions{
		DistroID:     "mock_distro",
		KeyName:      "mock_key",
		UserData:     "",
		InstanceTags: nil,
		InstanceType: "mock_type",
	}
	host, _ := c.CreateSpawnHost(ctx, spawnRequest)
	return []*restmodel.APIHost{host}, nil
}

// nolint
func (c *LocalRunMock) SetBannerMessage(ctx context.Context, m string, t evergreen.BannerTheme) error {
	return nil
}
func (c *LocalRunMock) GetBannerMessage(ctx context.Context) (string, error) { return "", nil }
func (c *LocalRunMock) SetServiceFlags(ctx context.Context, f *restmodel.APIServiceFlags) error {
	return nil
}
func (c *LocalRunMock) GetServiceFlags(ctx context.Context) (*restmodel.APIServiceFlags, error) {
	return nil, nil
}
func (c *LocalRunMock) RestartRecentTasks(ctx context.Context, starAt, endAt time.Time) error {
	return nil
}
func (c *LocalRunMock) GetSettings(ctx context.Context) (*evergreen.Settings, error) { return nil, nil }
func (c *LocalRunMock) UpdateSettings(ctx context.Context, update *restmodel.APIAdminSettings) (*restmodel.APIAdminSettings, error) {
	return nil, nil
}
func (c *LocalRunMock) GetEvents(ctx context.Context, ts time.Time, limit int) ([]interface{}, error) {
	return nil, nil
}
func (c *LocalRunMock) RevertSettings(ctx context.Context, guid string) error { return nil }
func (c *LocalRunMock) ExecuteOnDistro(context.Context, string, restmodel.APIDistroScriptOptions) ([]string, error) {
	return nil, nil
}

// SendResults posts a set of test results for the communicator's task.
// If results are empty or nil, this operation is a noop.
func (c *LocalRunMock) SendTestResults(ctx context.Context, td client.TaskData, results *task.LocalTestResults) error {
	c.LocalTestResults = results
	return nil
}

// SendFiles attaches task files.
func (c *LocalRunMock) AttachFiles(ctx context.Context, td client.TaskData, taskFiles []*artifact.File) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	grip.Info("attaching files")
	c.AttachedFiles[td.ID] = append(c.AttachedFiles[td.ID], taskFiles...)

	return nil
}

// SendTestLog posts a test log for a communicator's task. Is a
// noop if the test Log is nil.
func (c *LocalRunMock) SendTestLog(ctx context.Context, td client.TaskData, log *serviceModel.TestLog) (string, error) {
	c.TestLogs = append(c.TestLogs, log)
	c.TestLogs[len(c.TestLogs)-1].Id = c.LogID
	c.TestLogCount += 1
	c.LogID = fmt.Sprintf("%s-%d", c.LogID, c.TestLogCount)
	return c.LogID, nil
}

func (c *LocalRunMock) GetManifest(ctx context.Context, td client.TaskData) (*manifest.Manifest, error) {
	return &manifest.Manifest{}, nil
}

func (c *LocalRunMock) S3Copy(ctx context.Context, td client.TaskData, req *apimodels.S3CopyRequest) (string, error) {
	return "", nil
}

func (c *LocalRunMock) KeyValInc(ctx context.Context, td client.TaskData, kv *serviceModel.KeyVal) error {
	if cached, ok := c.keyVal[kv.Key]; ok {
		*kv = *cached
	} else {
		c.keyVal[kv.Key] = kv
	}
	kv.Value++
	return nil
}

func (c *LocalRunMock) PostJSONData(ctx context.Context, td client.TaskData, path string, data interface{}) error {
	return nil
}

func (c *LocalRunMock) GetJSONData(ctx context.Context, td client.TaskData, tn, dn, vn string) ([]byte, error) {
	return nil, nil
}

func (c *LocalRunMock) GetJSONHistory(ctx context.Context, td client.TaskData, tags bool, tn, dn string) ([]byte, error) {
	return nil, nil
}

func (c *LocalRunMock) GetDistrosList(ctx context.Context) ([]restmodel.APIDistro, error) {
	mockDistros := []restmodel.APIDistro{
		{
			Name:             restmodel.ToStringPtr("archlinux-build"),
			UserSpawnAllowed: true,
		},
		{
			Name:             restmodel.ToStringPtr("baas-linux"),
			UserSpawnAllowed: false,
		},
	}
	return mockDistros, nil
}

func (c *LocalRunMock) GetCurrentUsersKeys(ctx context.Context) ([]restmodel.APIPubKey, error) {
	return []restmodel.APIPubKey{
		{
			Name: restmodel.ToStringPtr("key0"),
			Key:  restmodel.ToStringPtr("ssh-fake 12345"),
		},
		{
			Name: restmodel.ToStringPtr("key1"),
			Key:  restmodel.ToStringPtr("ssh-fake 67890"),
		},
	}, nil
}

func (c *LocalRunMock) AddPublicKey(ctx context.Context, keyName, keyValue string) error {
	return errors.New("(c *LocalRunMock) AddPublicKey not implemented")
}

func (c *LocalRunMock) DeletePublicKey(ctx context.Context, keyName string) error {
	return errors.New("(c *LocalRunMock) DeletePublicKey not implemented")
}

func (c *LocalRunMock) ListAliases(ctx context.Context, keyName string) ([]serviceModel.ProjectAlias, error) {
	return nil, errors.New("(c *LocalRunMock) ListAliases not implemented")
}

func (c *LocalRunMock) GetClientConfig(ctx context.Context) (*evergreen.ClientConfig, error) {
	return &evergreen.ClientConfig{
		ClientBinaries: []evergreen.ClientBinary{
			evergreen.ClientBinary{
				Arch: "amd64",
				OS:   "darwin",
				URL:  "http://example.com/clients/darwin_amd64/evergreen",
			},
		},
		LatestRevision: evergreen.ClientVersion,
	}, nil
}

// GenerateTasks posts new tasks for the `generate.tasks` command.
func (c *LocalRunMock) GenerateTasks(ctx context.Context, td client.TaskData, jsonBytes []json.RawMessage) error {
	if td.ID != "mock_id" {
		return errors.New("mock failed, wrong id")
	}
	if td.Secret != "mock_secret" {
		return errors.New("mock failed, wrong secret")
	}
	return nil
}

func (c *LocalRunMock) GenerateTasksPoll(ctx context.Context, td client.TaskData) (*apimodels.GeneratePollResponse, error) {
	return &apimodels.GeneratePollResponse{
		Finished: true,
		Errors:   []string{},
	}, nil
}

func (c *LocalRunMock) CreateHost(ctx context.Context, td client.TaskData, options apimodels.CreateHost) ([]string, error) {
	if td.ID == "" {
		return []string{}, errors.New("no task ID sent to CreateHost")
	}
	if td.Secret == "" {
		return []string{}, errors.New("no task secret sent to CreateHost")
	}
	c.CreatedHost = options
	return []string{"id"}, options.Validate()
}

func (c *LocalRunMock) ListHosts(_ context.Context, _ client.TaskData) ([]restmodel.CreateHost, error) {
	return nil, nil
}

func (c *LocalRunMock) GetSubscriptions(_ context.Context) ([]event.Subscription, error) {
	if c.GetSubscriptionsFail {
		return nil, errors.New("failed to fetch subscriptions")
	}

	return []event.Subscription{
		{
			ResourceType: "type",
			Trigger:      "trigger",
			Owner:        "owner",
			Selectors: []event.Selector{
				{
					Type: "id",
					Data: "data",
				},
			},
			Subscriber: event.Subscriber{
				Type:   "email",
				Target: "a@domain.invalid",
			},
		},
	}, nil
}

func (c *LocalRunMock) CreateVersionFromConfig(ctx context.Context, project, message string, active bool, config []byte) (*serviceModel.Version, error) {
	return &serviceModel.Version{}, nil
}

func (c *LocalRunMock) GetCommitQueue(ctx context.Context, projectID string) (*restmodel.APICommitQueue, error) {
	return &restmodel.APICommitQueue{
		ProjectID: restmodel.ToStringPtr("mci"),
		Queue: []restmodel.APICommitQueueItem{
			restmodel.APICommitQueueItem{
				Issue: restmodel.ToStringPtr("123"),
				Modules: []restmodel.APIModule{
					restmodel.APIModule{
						Module: restmodel.ToStringPtr("test_module"),
						Issue:  restmodel.ToStringPtr("345"),
					},
				},
			},
			restmodel.APICommitQueueItem{
				Issue: restmodel.ToStringPtr("345"),
				Modules: []restmodel.APIModule{
					restmodel.APIModule{
						Module: restmodel.ToStringPtr("test_module2"),
						Issue:  restmodel.ToStringPtr("567"),
					},
				},
			},
		},
	}, nil
}

func (c *LocalRunMock) DeleteCommitQueueItem(ctx context.Context, projectID, item string) error {
	return nil
}

func (c *LocalRunMock) EnqueueItem(ctx context.Context, patchID string, force bool) (int, error) {
	return 0, nil
}

func (c *LocalRunMock) CreatePatchForMerge(ctx context.Context, patchID string) (*restmodel.APIPatch, error) {
	return nil, nil
}

func (c *LocalRunMock) SendNotification(_ context.Context, _ string, _ interface{}) error {
	return nil
}

func (c *LocalRunMock) GetDockerLogs(context.Context, string, time.Time, time.Time, bool) ([]byte, error) {
	return []byte("this is a log"), nil
}

func (c *LocalRunMock) GetDockerStatus(context.Context, string) (*cloud.ContainerStatus, error) {
	return &cloud.ContainerStatus{HasStarted: true}, nil
}

func (c *LocalRunMock) GetManifestByTask(context.Context, string) (*manifest.Manifest, error) {
	return &manifest.Manifest{Id: "manifest0"}, nil
}

func (c *LocalRunMock) StartHostProcesses(context.Context, []string, string, int) ([]restmodel.APIHostProcess, error) {
	return nil, nil
}

func (c *LocalRunMock) GetHostProcessOutput(context.Context, []restmodel.APIHostProcess, int) ([]restmodel.APIHostProcess, error) {
	return nil, nil
}

func (c *LocalRunMock) GetMatchingHosts(context.Context, time.Time, time.Time, string, bool) ([]string, error) {
	return nil, nil
}

func (c *LocalRunMock) GetTaskSyncReadCredentials(context.Context) (*evergreen.S3Credentials, error) {
	return &evergreen.S3Credentials{}, nil
}

func (c *LocalRunMock) GetTaskSyncPath(context.Context, string) (string, error) {
	return "", nil
}

func (c *LocalRunMock) GetDistroByName(context.Context, string) (*restmodel.APIDistro, error) {
	return nil, nil
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func PopulateExpansions(t *task.Task, h *host.Host, oauthToken string) (util.Expansions, error) {
	if t == nil {
		return nil, errors.New("task cannot be nil")
	}
	if h == nil {
		return nil, errors.New("host cannot be nil")
	}

	expansions := util.Expansions{}
	expansions.Put("execution", fmt.Sprintf("%v", t.Execution))
	expansions.Put("version_id", t.Version)
	expansions.Put("task_id", t.Id)
	expansions.Put("task_name", t.DisplayName)
	expansions.Put("build_id", t.BuildId)
	expansions.Put("build_variant", t.BuildVariant)
	expansions.Put("revision", t.Revision)
	expansions.Put(evergreen.GlobalGitHubTokenExpansion, oauthToken)
	expansions.Put("distro_id", h.Distro.Id)
	// expansions.Put("project",  .Identifier)
	// expansions.Put("project_tags", strings.Join( .Tags, ","))

	// if t.TriggerID != "" {
	// 	expansions.Put("trigger_event_identifier", t.TriggerID)
	// 	expansions.Put("trigger_event_type", t.TriggerType)
	// 	expansions.Put("trigger_id", t.TriggerEvent)
	// 	var upstreamProjectID string
	// 	if t.TriggerType == model.ProjectTriggerLevelTask {
	// 		var upstreamTask *task.Task
	// 		upstreamTask, err = task.FindOneId(t.TriggerID)
	// 		if err != nil {
	// 			return nil, errors.Wrap(err, "error finding task")
	// 		}
	// 		if upstreamTask == nil {
	// 			return nil, errors.New("upstream task not found")
	// 		}
	// 		expansions.Put("trigger_status", upstreamTask.Status)
	// 		expansions.Put("trigger_revision", upstreamTask.Revision)
	// 		upstreamProjectID = upstreamTask.Project
	// 	} else if t.TriggerType == ProjectTriggerLevelBuild {
	// 		var upstreamBuild *build.Build
	// 		upstreamBuild, err = build.FindOneId(t.TriggerID)
	// 		if err != nil {
	// 			return nil, errors.Wrap(err, "error finding build")
	// 		}
	// 		if upstreamBuild == nil {
	// 			return nil, errors.New("upstream build not found")
	// 		}
	// 		expansions.Put("trigger_status", upstreamBuild.Status)
	// 		expansions.Put("trigger_revision", upstreamBuild.Revision)
	// 		upstreamProjectID = upstreamBuild.Project
	// 	}

	// }

	// v, err := VersionFindOneId(t.Version)
	// if err != nil {
	// 	return nil, errors.Wrap(err, "error finding version")
	// }
	// if v == nil {
	// 	return nil, errors.Wrapf(err, "version '%s' doesn't exist", v.Id)
	// }

	// expansions.Put("branch_name", v.Branch)
	// expansions.Put("author", v.Author)
	// expansions.Put("created_at", v.CreateTime.Format(build.IdTimeLayout))

	// if evergreen.IsGitTagRequester(v.Requester) {
	// 	expansions.Put("triggered_by_git_tag", v.TriggeredByGitTag.Tag)
	// }
	// if evergreen.IsPatchRequester(v.Requester) {
	// 	var p *patch.Patch
	// 	p, err = patch.FindOne(patch.ByVersion(t.Version))
	// 	if err != nil {
	// 		return nil, errors.Wrapf(err, "error finding patch for version '%s'", t.Version)
	// 	}
	// 	if p == nil {
	// 		return nil, errors.Errorf("no patch found for version '%s'", t.Version)
	// 	}

	// 	expansions.Put("is_patch", "true")
	// 	expansions.Put("revision_order_id", fmt.Sprintf("%s_%d", v.Author, v.RevisionOrderNumber))
	// 	expansions.Put("alias", p.Alias)

	// 	if v.Requester == evergreen.MergeTestRequester {
	// 		expansions.Put("is_commit_queue", "true")
	// 		expansions.Put("commit_message", p.Description)
	// 	}

	// 	if v.Requester == evergreen.GithubPRRequester {
	// 		expansions.Put("github_pr_number", fmt.Sprintf("%d", p.GithubPatchData.PRNumber))
	// 		expansions.Put("github_org", p.GithubPatchData.BaseOwner)
	// 		expansions.Put("github_repo", p.GithubPatchData.BaseRepo)
	// 		expansions.Put("github_author", p.GithubPatchData.Author)
	// 	}
	// } else {
	// 	expansions.Put("revision_order_id", strconv.Itoa(v.RevisionOrderNumber))
	// }

	for _, e := range h.Distro.Expansions {
		expansions.Put(e.Key, e.Value)
	}
	// proj, _, err := LoadProjectForVersion(v, t.Project, false)
	// if err != nil {
	// 	return nil, errors.Wrap(err, "error unmarshalling project")
	// }
	// bv := proj.FindBuildVariant(t.BuildVariant)
	// expansions.Update(bv.Expansions)
	return expansions, nil
}

// LocalAgentRun - run a file
func LocalAgentRun(file string, display_task_id string, build_variant string, expansions string) {

	// var a *Agent
	// var mockCommunicator *client.LocalRunMock
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
		comm: NewLocalRunMock("url"),
	}

	var mockCommunicator = a.comm.(*LocalRunMock)
	a.jasper, err = jasper.NewSynchronizedManager(true)
	if err != nil {
		panic(err)
	}

	// 	projYml := `
	// pre:
	//   - command: shell.exec
	//     params:
	//       script: "echo hi"

	// tasks:
	// - name: test_task1
	//   depends_on: []
	//   commands:
	//   - command: shell.exec
	//     params:
	//       script: "echo Yeah!"

	// post:
	//   - command: shell.exec
	//     params:
	//       script: "echo hiiii"
	// `

	var tc = &taskContext{
		task: client.TaskData{
			ID:     "task_id456",
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

	dat, err := ioutil.ReadFile(file)
	check(err)

	//var pp ParserProject
	_, err = model.LoadProjectInto([]byte(dat), "", p)
	if err != nil {
		panic(err)
	}

	expands, err := ioutil.ReadFile(expansions)
	check(err)

	var expandsYaml map[string]string
	err = yaml.Unmarshal(expands, &expandsYaml)
	check(err)

	var tmpDirName string
	tmpDirName, err = ioutil.TempDir("/home/ubuntu/temp", "evg_local_run_")
	if err != nil {
		panic(err)
	}

	// err = os.MkdirAll(tmpDirName+"/src", 0755)
	// check(err)

	err = os.MkdirAll(tmpDirName+"/tmp", 0755)
	check(err)

	fmt.Printf("Temp DIR: %v\n", tmpDirName)

	tc.taskDirectory = tmpDirName

	tc.taskConfig = &model.TaskConfig{
		BuildVariant: &model.BuildVariant{
			Name: "buildvariant_id",
		},
		Task: &task.Task{
			Id:          "task_id456",
			Version:     versionId,
			DisplayName: display_task_id,
		},
		Expansions: &util.Expansions{
			"Foo": "Bar",

			"aws_key":    "FAKEFAKE",
			"aws_secret": "FAKEFAKE",

			"LocalRunHack": "true",

			// Server stuff
			"disable_shared_scons_cache": "true",

			// Hard coded evergreen constants
			"global_github_oauth_token": "FAKE",
		},
		Project: p,
		Timeout: &model.Timeout{IdleTimeoutSecs: 15, ExecTimeoutSecs: 15},
		WorkDir: tc.taskDirectory,
		Distro: &distro.Distro{
			CloneMethod: "",
		},
		ProjectRef: &model.ProjectRef{
			// TODO - make configurable
			Owner:  "markbenvenuto",
			Repo:   "mongo",
			Branch: "blackduck_vulnerability_evg",
		},
	}

	tc.taskConfig.WorkDir = tc.taskDirectory
	tc.taskConfig.Expansions.Put("workdir", tc.taskConfig.WorkDir)

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

	v := &model.Version{
		Id:                  "versionId",
		CreateTime:          time.Now(),
		Revision:            "foobar",
		RevisionOrderNumber: 500,
		Requester:           evergreen.RepotrackerVersionRequester,
		BuildVariants: []model.VersionBuildStatus{
			{
				BuildVariant: build_variant,
				Activated:    false,
			},
		},
		Config: string(dat),
	}
	table := model.NewTaskIdTable(p, v, "", "")

	args := model.BuildCreateArgs{
		Project:   *p,
		Version:   *v,
		TaskIDs:   table,
		BuildName: build_variant,
		Activated: true,
	}
	_, tasks, err := model.CreateBuildFromVersionNoInsert(args)

	t2 := tasks.Export()
	// for i := 0; i < tasks.Len(); i++ {
	// 	fmt.Printf("T: %v - %v\n", t2[i].DisplayName, t2[i].Id)
	// }

	var dt *task.Task

	for i := 0; i < tasks.Len(); i++ {
		if t2[i].DisplayName == display_task_id {
			dt = &t2[i]
		}
	}

	tc.taskModel = dt
	tc.taskGroup = dt.TaskGroup

	bv := p.FindBuildVariant(dt.BuildVariant)
	tc.taskConfig.Expansions.Update(bv.Expansions)

	tc.taskConfig.Expansions.Update(expandsYaml)

	h := host.Host{
		Id: "h",
		Distro: distro.Distro{
			Id:      "d1",
			WorkDir: "/home/evg",
			Expansions: []distro.Expansion{
				distro.Expansion{
					Key:   "note",
					Value: "huge success",
				},
				distro.Expansion{
					Key:   "cake",
					Value: "truth",
				},
				// distro.Expansion{
				// 	Key: "Foo", Value: "Bar"},
				// distro.Expansion{
				// 	Key: "aws_key", Value: "FAKEFAKE"},
				// distro.Expansion{
				// 	Key: "aws_secret", Value: "FAKEFAKE"},
				// distro.Expansion{
				// 	Key: "LocalRunHack", Value: "true"},
				// // Hard coded evergreen constants
				// distro.Expansion{
				// 	Key: "global_github_oauth_token", Value: "FAKE"},
			},
		},
	}

	e1, err := PopulateExpansions(dt, &h, "FAKEOAUTH")
	if err != nil {
		return
	}

	tc.taskConfig.Expansions.Update(e1)

	// tc.taskConfig.Expansions = &e

	fmt.Println("Run pre task commands")
	a.runPreTaskCommands(ctx, tc)

	fmt.Println("Run task commands")
	a.runTaskCommands(ctx, tc)

	fmt.Println("Run post task commands")
	a.runPostTaskCommands(ctx, tc)

	fmt.Println("Close logger")
	_ = tc.logger.Close()
	msgs := mockCommunicator.GetMockMessages()["task_id456"]

	// for i := 0; i < len(msgs); i++ {
	// 	fmt.Printf("msg: %v\n", msgs[i])
	// }
	fmt.Printf("msg: %v\n", len(msgs))

}
