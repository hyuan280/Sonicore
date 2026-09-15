package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/logger"
	pluginmgr "github.com/sonicore/server/internal/plugin"
)

// PluginHandler serves the admin plugin-management API: installed plugin
// lifecycle plus the marketplace (repos, catalog, install/update).
type PluginHandler struct {
	mgr    *pluginmgr.Manager
	market *pluginmgr.Market
}

func NewPluginHandler(mgr *pluginmgr.Manager, market *pluginmgr.Market) *PluginHandler {
	return &PluginHandler{mgr: mgr, market: market}
}

func (h *PluginHandler) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.mgr.List(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	if err := h.market.EnrichDownloads(r.Context(), list); err != nil {
		logger.Warn("[plugin] enrich download counts: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"plugins": list})
}

// UninstalledList returns the plugins discovered on disk but not installed
// (the admin installs them explicitly).
func (h *PluginHandler) UninstalledList(w http.ResponseWriter, r *http.Request) {
	list, err := h.mgr.ListUninstalled(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	if err := h.market.EnrichDownloads(r.Context(), list); err != nil {
		logger.Warn("[plugin] enrich download counts: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"plugins": list})
}

// InstallPlugin marks a discovered plugin as installed. Installs default to
// disabled: the admin enables the plugin afterwards.
func (h *PluginHandler) InstallPlugin(w http.ResponseWriter, r *http.Request) {
	if err := h.mgr.Install(r.Context(), mux.Vars(r)["id"]); err != nil {
		writePluginActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Delete permanently removes the plugin: directory on disk plus the DB
// record (and with it the stored config/data). The soft variant is
// POST /{id}/uninstall.
func (h *PluginHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.mgr.Delete(r.Context(), mux.Vars(r)["id"]); err != nil {
		writePluginActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// LogsStream serves a plugin's log file over SSE (text/event-stream): the
// last 50 lines are sent first, then new lines as the plugin writes them.
// The client authenticates with the regular Authorization header (fetch
// stream), filters levels and searches locally.
func (h *PluginHandler) LogsStream(w http.ResponseWriter, r *http.Request) {
	id, ok := h.requireInstalled(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// http.ResponseController unwraps writer middlewares and works even when
	// the ResponseWriter type assertion would fail.
	rc := http.NewResponseController(w)
	// SSE streams live indefinitely — clear the server's global
	// WriteTimeout deadline, otherwise the connection is cut after 60s.
	_ = rc.SetWriteDeadline(time.Time{})
	ctx, cancel := context.WithCancel(r.Context())
	// The stream joins the long-connection registry: server shutdown
	// cancels it immediately instead of waiting for the client.
	unregister := RegisterLongConn(cancel)
	defer unregister()

	var writeMu sync.Mutex
	write := func(s string) bool {
		writeMu.Lock()
		defer writeMu.Unlock()
		if _, err := fmt.Fprint(w, s); err != nil {
			return false
		}
		return rc.Flush() == nil
	}

	// Send the first bytes immediately: even if the log file is missing
	// below, the client gets a valid SSE response instead of an empty one
	// (which surfaces as ERR_EMPTY_RESPONSE in browsers).
	if !write(": connected\n\n") {
		return
	}

	// Keepalive comments so proxies never kill an idle stream. The handler
	// waits for this goroutine after cancel(): ctx.Done and ticker.C can
	// both be ready and select picks randomly, so without the wait a ping
	// could race a write after the handler has returned.
	keepaliveDone := make(chan struct{})
	go func() {
		defer close(keepaliveDone)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !write(": ping\n\n") {
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		<-keepaliveDone
	}()

	// cursor = timestamp of the last record the client already saw: the
	// stream resumes right after it (replays lines written while the
	// client was disconnected/paused).
	cursor := r.URL.Query().Get("cursor")

	if err := h.mgr.StreamLogs(ctx, id, pluginmgr.StreamOpts{Cursor: cursor}, func(line string) {
		// One SSE event per physical line; the client reassembles
		// multi-line records. Defense in depth: never let a stray \r
		// reach a frame boundary.
		line = strings.ReplaceAll(line, "\r", "")
		write("data: " + line + "\n\n")
	}, func() {
		// The cursor line rotated away / the file was recreated: tell the
		// client some logs were skipped, then follow from the end.
		write("event: gap\n\n")
	}); err != nil {
		// Fixed copy only — the real error may contain internal paths and
		// must not reach the client.
		logger.Warn("[plugin] log stream for %s: %v", id, err)
		write("event: error\ndata: log stream failed\n\n")
	}
}

// requireInstalled gates plugin-scoped endpoints: uninstalled plugins expose
// no config/form/page and cannot be enabled, so their admin endpoints
// answer 404 like any unknown plugin.
func (h *PluginHandler) requireInstalled(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := mux.Vars(r)["id"]
	installed, err := h.mgr.IsInstalled(r.Context(), id)
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return "", false
	}
	if !installed {
		writeCodedError(w, http.StatusNotFound, domain.ErrNotFound)
		return "", false
	}
	return id, true
}

type setEnabledRequest struct {
	// Pointer so a missing field (empty body) is distinguishable from an
	// explicit false — an empty request must not disable a running plugin.
	Enabled *bool `json:"enabled"`
}

func (h *PluginHandler) SetEnabled(w http.ResponseWriter, r *http.Request) {
	id, ok := h.requireInstalled(w, r)
	if !ok {
		return
	}
	var req setEnabledRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if req.Enabled == nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if err := h.mgr.SetEnabled(r.Context(), id, *req.Enabled); err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type setConfigRequest struct {
	Config json.RawMessage `json:"config"`
}

func (h *PluginHandler) SetConfig(w http.ResponseWriter, r *http.Request) {
	id, ok := h.requireInstalled(w, r)
	if !ok {
		return
	}
	var req setConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	// The config must be a JSON object (not null/missing/array); otherwise
	// a {"config":null} payload would pass json.Valid and get persisted as
	// a SQL null instead of a config object.
	var obj map[string]any
	if len(req.Config) == 0 || json.Unmarshal(req.Config, &obj) != nil || obj == nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if err := h.mgr.SetConfig(r.Context(), id, string(req.Config)); err != nil {
		if errors.Is(err, pluginmgr.ErrInvalidConfigJSON) {
			writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
			return
		}
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetConfig returns the plugin's currently stored configuration values.
// The config form itself is fetched via GetForm (get_form assembly, with
// a fallback built from the manifest [plugin.config] declaration), so no
// schema is needed here.
func (h *PluginHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	id, ok := h.requireInstalled(w, r)
	if !ok {
		return
	}
	values, err := h.mgr.GetConfig(r.Context(), id)
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": values})
}

// GetForm returns the plugin's config form UI assembly (get_form
// equivalent). Falls back to a form built from the manifest [plugin.config]
// schema when the plugin provides none.
func (h *PluginHandler) GetForm(w http.ResponseWriter, r *http.Request) {
	id, ok := h.requireInstalled(w, r)
	if !ok {
		return
	}
	schema, err := h.mgr.GetForm(r.Context(), id)
	if err != nil {
		// A fetch failure (RPC timeout, dead process) is not the same as
		// "the plugin provides no form" — surface it instead of silently
		// falling back.
		logger.Warn("[plugin] get form for %s: %v", id, err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	if schema == "" {
		schema = fallbackForm(h.mgr, id)
	}
	if schema == "" {
		writeJSON(w, http.StatusOK, map[string]any{"schema": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema": json.RawMessage(schema)})
}

// GetPage returns the plugin's data page UI assembly (get_page
// equivalent); nil when the plugin provides no page (the frontend then
// shows only the config form).
func (h *PluginHandler) GetPage(w http.ResponseWriter, r *http.Request) {
	id, ok := h.requireInstalled(w, r)
	if !ok {
		return
	}
	schema, err := h.mgr.GetPage(r.Context(), id)
	if err != nil {
		logger.Warn("[plugin] get page for %s: %v", id, err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	if schema == "" {
		writeJSON(w, http.StatusOK, map[string]any{"schema": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema": json.RawMessage(schema)})
}

// fallbackForm builds a Vuetify component-tree form from the manifest
// config fields so plugins without an OnForm builder still get a usable
// config form.
func fallbackForm(mgr *pluginmgr.Manager, id string) string {
	fields, ok := mgr.ConfigSchema(id)
	if !ok || len(fields) == 0 {
		return ""
	}
	content := make([]any, 0, len(fields))
	for _, f := range fields {
		component := "VTextField"
		props := map[string]any{"model": f.Key, "label": f.Label}
		switch f.Type {
		case "boolean":
			component = "VSwitch"
		case "number":
			props["type"] = "number"
		}
		content = append(content, map[string]any{
			"component": component,
			"props":     props,
		})
	}
	raw, err := json.Marshal(map[string]any{
		"component": "VForm",
		"content":   content,
	})
	if err != nil {
		return ""
	}
	return string(raw)
}

func (h *PluginHandler) Uninstall(w http.ResponseWriter, r *http.Request) {
	if err := h.mgr.Uninstall(r.Context(), mux.Vars(r)["id"]); err != nil {
		writePluginActionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writePluginActionError maps manager errors for the install/uninstall/
// delete actions: a missing plugin row is a 404, everything else 500.
func writePluginActionError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeCodedError(w, http.StatusNotFound, domain.ErrNotFound)
		return
	}
	writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
}

// Update runs the plugin update flow: download the newest version from its
// source repo, verify, swap the directory and restart when it was enabled.
func (h *PluginHandler) Update(w http.ResponseWriter, r *http.Request) {
	if err := h.market.UpdatePlugin(r.Context(), mux.Vars(r)["id"]); err != nil {
		if errors.Is(err, pluginmgr.ErrAlreadyUpToDate) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "detail": "already up to date"})
			return
		}
		writeMarketError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Market returns the merged catalog of every enabled repository (served
// from the repo cache, never live-fetched per request).
func (h *PluginHandler) Market(w http.ResponseWriter, r *http.Request) {
	entries, err := h.market.Catalog(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"plugins": entries})
}

// MarketDetail returns one plugin across all repos (official repos win
// ties; nil when unknown).
func (h *PluginHandler) MarketDetail(w http.ResponseWriter, r *http.Request) {
	entry, err := h.market.Detail(r.Context(), mux.Vars(r)["name"])
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"plugin": entry})
}

type marketInstallRequest struct {
	Name string `json:"name"`
	Repo string `json:"repo"`
}

// Install downloads and installs a plugin from the marketplace.
func (h *PluginHandler) Install(w http.ResponseWriter, r *http.Request) {
	var req marketInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Repo = strings.TrimSpace(req.Repo)
	if req.Name == "" || req.Repo == "" {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if err := h.market.Install(r.Context(), req.Name, req.Repo); err != nil {
		writeMarketError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Repos lists configured plugin repositories.
func (h *PluginHandler) Repos(w http.ResponseWriter, r *http.Request) {
	repos, err := h.market.Repos(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"repos": repos})
}

type addRepoRequest struct {
	URL string `json:"url"`
}

// AddRepo fetches and validates the repo.json at url, then stores the repo.
func (h *PluginHandler) AddRepo(w http.ResponseWriter, r *http.Request) {
	var req addRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if !validRepoURL(req.URL) {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if err := h.market.AddRepo(r.Context(), req.URL); err != nil {
		if errors.Is(err, pluginmgr.ErrInvalidRepoJSON) {
			writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
			return
		}
		writeMarketError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RemoveRepo deletes a repository (official repos refuse removal).
func (h *PluginHandler) RemoveRepo(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if err := h.market.RemoveRepo(r.Context(), url); err != nil {
		writeMarketError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// validRepoURL gates the admin-entered repository URL. It only rejects missing
// hosts and userinfo; the first hop is deliberately NOT restricted to public
// addresses, because the admin may point at a self-hosted repository on a
// private network. Redirects from that URL are vetted by the market client's
// CheckRedirect, and the download_url inside repo.json is re-vetted on the
// market's download path.
func validRepoURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// writeMarketError maps market sentinel errors to HTTP codes: unknown
// repos/plugins are 404, invalid client input 400, state conflicts 409,
// everything else 500 (including upstream data problems like a checksum
// mismatch).
func writeMarketError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows),
		errors.Is(err, pluginmgr.ErrRepoNotFound),
		errors.Is(err, pluginmgr.ErrPluginNotFoundInRepo),
		errors.Is(err, pluginmgr.ErrNotInstalled):
		writeCodedError(w, http.StatusNotFound, domain.ErrNotFound)
	case errors.Is(err, pluginmgr.ErrInvalidPluginName):
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
	case errors.Is(err, pluginmgr.ErrPluginDirConflict),
		errors.Is(err, pluginmgr.ErrOfficialRepoUndeletable),
		errors.Is(err, pluginmgr.ErrNoRepoSource),
		errors.Is(err, pluginmgr.ErrRepoDisabled),
		errors.Is(err, pluginmgr.ErrRepoNameConflict),
		errors.Is(err, pluginmgr.ErrAlreadyInstalled),
		errors.Is(err, pluginmgr.ErrRepoExists):
		writeCodedError(w, http.StatusConflict, domain.ErrConflict)
	default:
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
	}
}
