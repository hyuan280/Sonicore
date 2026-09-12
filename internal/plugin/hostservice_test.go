package plugin

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginsdk "github.com/hyuan280/Sonicore-PluginSDK/go"
	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"
)

// fakeRepo records every call so tests can assert which plugin id the host
// service resolved operations against.
type fakeRepo struct {
	configRaw string
	dataRaw   string
	setErr    error

	configIDs []string
	dataIDs   []string
	dataKeys  []string
	setIDs    []string
	setKeys   []string
	delIDs    []string
	delKeys   []string
	statuses  []statusCall
}

type statusCall struct {
	id, st, msg string
}

func (f *fakeRepo) GetConfig(ctx context.Context, id string) (string, error) {
	f.configIDs = append(f.configIDs, id)
	if f.setErr != nil {
		return "", f.setErr
	}
	return f.configRaw, nil
}

func (f *fakeRepo) GetData(ctx context.Context, id, key string) (string, error) {
	f.dataIDs = append(f.dataIDs, id)
	f.dataKeys = append(f.dataKeys, key)
	if f.setErr != nil {
		return "", f.setErr
	}
	return f.dataRaw, nil
}

func (f *fakeRepo) SetData(ctx context.Context, id, key, valueJSON string) error {
	f.setIDs = append(f.setIDs, id)
	f.setKeys = append(f.setKeys, key)
	return f.setErr
}

func (f *fakeRepo) DeleteData(ctx context.Context, id, key string) error {
	f.delIDs = append(f.delIDs, id)
	f.delKeys = append(f.delKeys, key)
	return f.setErr
}

func (f *fakeRepo) UpdateStatus(ctx context.Context, id, st, msg string) error {
	f.statuses = append(f.statuses, statusCall{id: id, st: st, msg: msg})
	return f.setErr
}

// TestHostServiceScopesAllAccessToItsPluginID is the regression test for
// the cross-plugin access bug: a malicious plugin can put any string into
// req.PluginId, but every operation must resolve against the plugin id the
// hostService instance was created for (the broker connection identity).
func TestHostServiceScopesAllAccessToItsPluginID(t *testing.T) {
	repo := &fakeRepo{configRaw: `{"a":1}`, dataRaw: `"v"`}
	hs := newHostService("self", repo, nil, nil)
	ctx := context.Background()

	_, err := hs.GetConfig(ctx, &gen.GetConfigRequest{PluginId: "victim"})
	require.NoError(t, err)
	_, err = hs.GetData(ctx, &gen.GetDataRequest{PluginId: "victim", Key: "k1"})
	require.NoError(t, err)
	_, err = hs.SetData(ctx, &gen.SetDataRequest{PluginId: "victim", Key: "k2", ValueJson: `"x"`})
	require.NoError(t, err)
	_, err = hs.DeleteData(ctx, &gen.DeleteDataRequest{PluginId: "victim", Key: "k1"})
	require.NoError(t, err)
	_, err = hs.ReportStatus(ctx, &gen.ReportStatusRequest{PluginId: "victim", Status: pluginsdk.StatusOK})
	require.NoError(t, err)

	require.Equal(t, []string{"self"}, repo.configIDs)
	require.Equal(t, []string{"self"}, repo.dataIDs)
	require.Equal(t, []string{"self"}, repo.setIDs)
	require.Equal(t, []string{"self"}, repo.delIDs)
	require.Equal(t, []statusCall{{id: "self", st: StatusOK, msg: ""}}, repo.statuses)
}

// TestHostServiceEmptyPluginIDStillWorks pins the legacy contract: callers
// that send no plugin id (or older SDKs) must land on the connection's own
// plugin, not on an empty id.
func TestHostServiceEmptyPluginIDStillWorks(t *testing.T) {
	repo := &fakeRepo{configRaw: `{}`}
	hs := newHostService("self", repo, nil, nil)

	resp, err := hs.GetConfig(context.Background(), &gen.GetConfigRequest{})
	require.NoError(t, err)
	require.Equal(t, `{}`, resp.ConfigJson)
	require.Equal(t, []string{"self"}, repo.configIDs)
}

func TestHostServiceReportStatusBranches(t *testing.T) {
	repo := &fakeRepo{}
	hs := newHostService("self", repo, nil, nil)
	ctx := context.Background()

	_, err := hs.ReportStatus(ctx, &gen.ReportStatusRequest{PluginId: "attacker", Status: pluginsdk.StatusConfigInvalid, Error: "bad"})
	require.NoError(t, err)
	_, err = hs.ReportStatus(ctx, &gen.ReportStatusRequest{PluginId: "attacker", Status: "custom", Error: "oops"})
	require.NoError(t, err)
	_, err = hs.ReportStatus(ctx, &gen.ReportStatusRequest{PluginId: "attacker"})
	require.NoError(t, err)

	require.Equal(t, []statusCall{
		{id: "self", st: StatusError, msg: "config invalid: bad"},
		// Unknown status strings are normalized to the host-controlled
		// StatusError; only ok/config_invalid are whitelisted.
		{id: "self", st: StatusError, msg: "oops"},
		{id: "self", st: StatusError, msg: ""},
	}, repo.statuses)
}

func TestHostServiceGetConfigPropagatesRepoErrors(t *testing.T) {
	repo := &fakeRepo{setErr: context.DeadlineExceeded}
	hs := newHostService("self", repo, nil, nil)

	_, err := hs.GetConfig(context.Background(), &gen.GetConfigRequest{PluginId: "victim"})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

// TestHostServiceSetDataErrorMapping pins the caller/server error split:
// invalid data JSON is the plugin's fault (InvalidArgument), storage
// failures are host problems (Internal).
func TestHostServiceSetDataErrorMapping(t *testing.T) {
	repo := &fakeRepo{setErr: fmt.Errorf("wrap: %w", ErrInvalidDataJSON)}
	hs := newHostService("self", repo, nil, nil)
	_, err := hs.SetData(context.Background(), &gen.SetDataRequest{PluginId: "attacker", Key: "k", ValueJson: "not-json"})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	repo = &fakeRepo{setErr: context.DeadlineExceeded}
	hs = newHostService("self", repo, nil, nil)
	_, err = hs.SetData(context.Background(), &gen.SetDataRequest{PluginId: "attacker", Key: "k", ValueJson: `"x"`})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestHostServiceRegisterScheduleValidation(t *testing.T) {
	hs := newHostService("self", &fakeRepo{}, nil, nil)
	ctx := context.Background()

	_, err := hs.RegisterSchedule(ctx, &gen.RegisterScheduleRequest{})
	require.Error(t, err)

	_, err = hs.RegisterSchedule(ctx, &gen.RegisterScheduleRequest{Schedule: &gen.ScheduleSpec{Id: "s1"}})
	require.Error(t, err)

	_, err = hs.RegisterSchedule(ctx, &gen.RegisterScheduleRequest{
		Schedule: &gen.ScheduleSpec{Id: "s1", Cron: "x", Interval: "y"},
	})
	require.Error(t, err)

	var got *gen.ScheduleSpec
	hs.registerSchedule = func(spec *gen.ScheduleSpec) error {
		got = spec
		return nil
	}
	_, err = hs.RegisterSchedule(ctx, &gen.RegisterScheduleRequest{
		Schedule: &gen.ScheduleSpec{Id: "s1", Cron: "* * * * *"},
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "s1", got.Id)

	// Host-side unavailability maps to Unavailable (generic message), not
	// InvalidArgument, and never leaks the internal detail to the plugin.
	hs.registerSchedule = func(spec *gen.ScheduleSpec) error {
		return fmt.Errorf("%w: task service not ready", ErrSchedulerUnavailable)
	}
	_, err = hs.RegisterSchedule(ctx, &gen.RegisterScheduleRequest{
		Schedule: &gen.ScheduleSpec{Id: "s2", Interval: "30s"},
	})
	require.Error(t, err)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.NotContains(t, status.Convert(err).Message(), "task service")

	// Invalid interval stays InvalidArgument with a generic message.
	hs.registerSchedule = func(spec *gen.ScheduleSpec) error {
		return fmt.Errorf("invalid interval")
	}
	_, err = hs.RegisterSchedule(ctx, &gen.RegisterScheduleRequest{
		Schedule: &gen.ScheduleSpec{Id: "s3", Interval: "30s"},
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.NotContains(t, status.Convert(err).Message(), "invalid interval")
}
