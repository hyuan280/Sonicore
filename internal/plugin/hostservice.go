package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginsdk "github.com/hyuan280/Sonicore-PluginSDK/go"
	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"

	"github.com/sonicore/server/internal/infrastructure/logger"
)

// pluginRepo is the storage surface the host service needs. It exists so
// the service can be tested with a fake repository.
type pluginRepo interface {
	GetConfig(ctx context.Context, id string) (string, error)
	GetData(ctx context.Context, id, key string) (string, error)
	SetData(ctx context.Context, id, key, valueJSON string) error
	DeleteData(ctx context.Context, id, key string) error
	UpdateStatus(ctx context.Context, id, status, msg string) error
}

// hostService implements gen.HostServiceServer. One instance per plugin
// (scoped by the plugin id stored in it), served to the plugin over the
// go-plugin broker from the host's GRPCClient callback.
//
// The connection identity is authoritative: a hostService instance is only
// ever served to the plugin process it was created for, so every method
// resolves the plugin from h.pluginID and ignores any id the request
// carries. Trusting req.PluginId would let a plugin read/overwrite/delete
// other plugins' config and data or spoof their status reports.
type hostService struct {
	gen.UnimplementedHostServiceServer
	pluginID string
	repo     pluginRepo
	// slog is the plugin's own log sink ({data_dir}/log/plugins/{name}.log);
	// nil falls back to the global logger (tests).
	slog *slog.Logger
	// registerSchedule installs a plugin-declared schedule into the
	// scheduler (wired by the manager, nil when unavailable).
	registerSchedule func(spec *gen.ScheduleSpec) error
}

func newHostService(pluginID string, repo pluginRepo, registerSchedule func(spec *gen.ScheduleSpec) error, sl *slog.Logger) *hostService {
	return &hostService{pluginID: pluginID, repo: repo, registerSchedule: registerSchedule, slog: sl}
}

func (h *hostService) GetConfig(ctx context.Context, req *gen.GetConfigRequest) (*gen.GetConfigResponse, error) {
	raw, err := h.repo.GetConfig(ctx, h.pluginID)
	if err != nil {
		// The plugin is an untrusted boundary: log the detailed error
		// host-side and hand back a generic message only.
		logger.Warn("[plugin] host get config for %s: %v", h.pluginID, err)
		return nil, status.Error(codes.Internal, "get config failed")
	}
	return &gen.GetConfigResponse{ConfigJson: raw}, nil
}

func (h *hostService) GetData(ctx context.Context, req *gen.GetDataRequest) (*gen.GetDataResponse, error) {
	raw, err := h.repo.GetData(ctx, h.pluginID, req.Key)
	if err != nil {
		logger.Warn("[plugin] host get data for %s key %s: %v", h.pluginID, req.Key, err)
		return nil, status.Error(codes.Internal, "get data failed")
	}
	return &gen.GetDataResponse{ValueJson: raw}, nil
}

func (h *hostService) SetData(ctx context.Context, req *gen.SetDataRequest) (*gen.SetDataResponse, error) {
	if err := h.repo.SetData(ctx, h.pluginID, req.Key, req.ValueJson); err != nil {
		// A JSON validation failure is the caller's fault; anything else
		// (DB/storage failure) is a host problem.
		if errors.Is(err, ErrInvalidDataJSON) {
			return nil, status.Error(codes.InvalidArgument, "set data failed: invalid data JSON")
		}
		logger.Warn("[plugin] host set data for %s key %s: %v", h.pluginID, req.Key, err)
		return nil, status.Error(codes.Internal, "set data failed")
	}
	return &gen.SetDataResponse{}, nil
}

func (h *hostService) DeleteData(ctx context.Context, req *gen.DeleteDataRequest) (*gen.DeleteDataResponse, error) {
	if err := h.repo.DeleteData(ctx, h.pluginID, req.Key); err != nil {
		logger.Warn("[plugin] host delete data for %s key %s: %v", h.pluginID, req.Key, err)
		return nil, status.Error(codes.Internal, "delete data failed")
	}
	return &gen.DeleteDataResponse{}, nil
}

func (h *hostService) RegisterSchedule(ctx context.Context, req *gen.RegisterScheduleRequest) (*gen.RegisterScheduleResponse, error) {
	spec := req.Schedule
	if spec == nil || spec.Id == "" ||
		(spec.Cron == "" && spec.Interval == "") ||
		(spec.Cron != "" && spec.Interval != "") {
		return nil, status.Error(codes.InvalidArgument, "invalid schedule spec: exactly one of cron/interval with an id is required")
	}
	if h.registerSchedule == nil {
		return nil, status.Error(codes.Unavailable, "scheduler unavailable")
	}
	if err := h.registerSchedule(spec); err != nil {
		// Only spec problems are the plugin's fault; scheduler/backend
		// readiness issues are host-side and must not be reported as
		// invalid arguments. Either way the plugin only gets a generic
		// message — the detail stays in the host log.
		logger.Warn("[plugin] register schedule for %s: %v", h.pluginID, err)
		if errors.Is(err, ErrSchedulerUnavailable) {
			return nil, status.Error(codes.Unavailable, "register schedule failed")
		}
		return nil, status.Error(codes.InvalidArgument, "register schedule failed")
	}
	return &gen.RegisterScheduleResponse{}, nil
}

// sanitizeLogMessage normalizes line endings in plugin messages (\r\n and
// lone \r become \n) and strips terminal/ANSI control characters. Multi-line
// messages keep their real newlines: the log file stores the record across
// several physical lines and the SSE stream delivers the whole record in one
// event — the frontend renders it in a table cell with line breaks. Raw
// control characters could otherwise spoof the console mirror or fake
// additional records in the timestamp-based log viewer.
func sanitizeLogMessage(msg string) string {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")
	var b strings.Builder
	b.Grow(len(msg))
	for _, r := range msg {
		if r == '\n' || r == '\t' || r >= 0x20 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (h *hostService) Log(ctx context.Context, req *gen.LogRequest) (*gen.LogResponse, error) {
	msg := fmt.Sprintf("[plugin:%s] %s", h.pluginID, sanitizeLogMessage(req.Message))
	if h.slog != nil {
		// The plugin's own sink; the file is already per-plugin so the
		// prefix above only matters for the shared stderr mirror.
		switch strings.ToLower(req.Level) {
		case "debug":
			h.slog.Debug(msg)
		case "warn", "warning":
			h.slog.Warn(msg)
		case "error":
			h.slog.Error(msg)
		default:
			h.slog.Info(msg)
		}
		return &gen.LogResponse{}, nil
	}
	switch strings.ToLower(req.Level) {
	case "debug":
		logger.Debug("%s", msg)
	case "warn", "warning":
		logger.Warn("%s", msg)
	case "error":
		logger.Error("%s", msg)
	default:
		logger.Info("%s", msg)
	}
	return &gen.LogResponse{}, nil
}

func (h *hostService) ReportStatus(ctx context.Context, req *gen.ReportStatusRequest) (*gen.ReportStatusResponse, error) {
	// The plugin may only report the two states the host cannot observe
	// itself: healthy and config-invalid. Everything else (ok/error/
	// disabled/stopped) is owned by the host lifecycle, so unknown status
	// strings are normalized to StatusError instead of being persisted
	// verbatim — otherwise a plugin could pollute the status column with
	// arbitrary values.
	switch req.Status {
	case pluginsdk.StatusOK:
		if err := h.repo.UpdateStatus(ctx, h.pluginID, StatusOK, ""); err != nil {
			logger.Warn("[plugin] persist status for %s: %v", h.pluginID, err)
		}
	case pluginsdk.StatusConfigInvalid:
		logDBError(ctx, h.repo, h.pluginID, StatusError, "config invalid: "+req.Error)
	default:
		logDBError(ctx, h.repo, h.pluginID, StatusError, req.Error)
	}
	return &gen.ReportStatusResponse{}, nil
}

func (h *hostService) Ping(ctx context.Context, req *gen.PingRequest) (*gen.PingResponse, error) {
	return &gen.PingResponse{}, nil
}

var _ gen.HostServiceServer = (*hostService)(nil)
