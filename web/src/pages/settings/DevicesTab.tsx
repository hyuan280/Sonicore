import { useState, useEffect, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { translateApiError } from "../../i18n/errorCodes";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Card } from "../../components/ui/card";
import { api } from "../../api/client";
import { Plus, Loader2, Volume2, Trash2 } from "lucide-react";

interface DeviceConfig {
  id: string;
  name: string;
  device_type: string;
  driver: string;
  device_id: string;
  bound_jukebox?: string;
}

interface AudioDevice {
  id: string;
  name?: string;
  driver?: string;
}

// Derives the driver for a detected device id. The backend now reports the
// driver explicitly; this heuristic only remains as a fallback for older
// servers or manually entered ids.
function driverFor(id: string): string {
  return id.startsWith("hw:") ? "alsa" : "pulseaudio";
}

export default function DevicesTab() {
  const { t } = useTranslation();
  const [devices, setDevices] = useState<DeviceConfig[]>([]);
  const [detected, setDetected] = useState<AudioDevice[]>([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [addForm, setAddForm] = useState({
    name: "",
    device_type: "local",
    device_id: "",
    driver: "pulseaudio",
  });
  const [adding, setAdding] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [error, setError] = useState("");
  const formRef = useRef<HTMLDivElement>(null);
  const mountedRef = useRef(true);
  const requestSeqRef = useRef(0);

  const load = useCallback(
    (seq?: number) => {
      const requestSeq = seq ?? ++requestSeqRef.current;
      setLoading(true);
      Promise.all([api.jukebox.deviceConfigs(), api.jukebox.audioDevices()])
        .then(([configs, d]) => {
          // Ignore stale responses: both when unmounted and when a newer
          // request has been issued (e.g. StrictMode double effects or a
          // language switch recreating the callback).
          if (!mountedRef.current || requestSeq !== requestSeqRef.current) return;
          setError("");
          setDevices(configs.devices || []);
          setDetected(d.devices || []);
        })
        .catch((err: unknown) => {
          if (!mountedRef.current || requestSeq !== requestSeqRef.current) return;
          setError(translateApiError(t, err));
        })
        .finally(() => {
          if (mountedRef.current && requestSeq === requestSeqRef.current) setLoading(false);
        });
    },
    [t],
  );

  useEffect(() => {
    // Re-arm the guard: this effect re-runs when `load` changes (e.g. a
    // language switch recreates the callback), and the previous cleanup
    // has marked the component as unmounted.
    mountedRef.current = true;
    const seq = ++requestSeqRef.current;
    load(seq);
    return () => {
      mountedRef.current = false;
    };
  }, [load]);

  useEffect(() => {
    if (!showAdd) return;
    const el = formRef.current;
    if (!el) return;
    requestAnimationFrame(() => {
      const container = el.closest("main");
      if (container) {
        container.scrollTo({ top: container.scrollHeight, behavior: "smooth" });
      } else {
        el.scrollIntoView({ behavior: "smooth", block: "end" });
      }
    });
  }, [showAdd]);

  const closeForm = () => {
    setShowAdd(false);
    setError("");
    setAddForm({ name: "", device_type: "local", device_id: "", driver: "pulseaudio" });
  };

  const existingIds = new Set(devices.map((d: DeviceConfig) => d.device_id + ":" + d.driver));
  const existingNames = new Set(devices.map((d: DeviceConfig) => d.name));
  const filteredDetected = detected.filter(
    (d: AudioDevice) =>
      !existingIds.has(d.id + ":" + (d.driver || driverFor(d.id))) &&
      !existingNames.has(d.name || d.id),
  );

  const handleAdd = async () => {
    if (!addForm.name || !addForm.device_id) return;
    if (existingIds.has(addForm.device_id + ":" + addForm.driver)) {
      setError(t("settings.deviceAlreadyExists"));
      return;
    }
    setAdding(true);
    setError("");
    try {
      await api.jukebox.createDeviceConfig({
        name: addForm.name,
        device_type: addForm.device_type,
        device_id: addForm.device_id,
        driver: addForm.driver,
      });
      setShowAdd(false);
      setAddForm({ name: "", device_type: "local", device_id: "", driver: "pulseaudio" });
      load();
    } catch (e) {
      setError(translateApiError(t, e));
    } finally {
      setAdding(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm(t("settings.deleteDevice"))) return;
    setDeletingId(id);
    setError("");
    try {
      await api.jukebox.deleteDeviceConfig(id);
      load();
    } catch (e) {
      setError(translateApiError(t, e));
    } finally {
      setDeletingId(null);
    }
  };

  const selectDetected = (d: AudioDevice) => {
    setAddForm({
      name: d.name || d.id,
      device_type: "local",
      device_id: d.id,
      driver: d.driver || driverFor(d.id),
    });
  };

  const openAddCard = () => {
    setError("");
    setShowAdd(true);
  };

  return (
    <div className="space-y-4">
      {error && <p className="text-xs text-red-400">{error}</p>}

      {loading ? (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="w-5 h-5 animate-spin text-zinc-500" />
        </div>
      ) : (
        devices.map((d: DeviceConfig) => (
          <Card key={d.id} className="p-4">
            <div className="flex items-center gap-3">
              <Volume2 className="w-4 h-4 text-green-500 shrink-0" />
              <div className="flex-1 min-w-0">
                <div className="text-sm">{d.name}</div>
                <div className="text-xs text-zinc-500">
                  {d.device_type} · {d.driver} · {d.device_id}
                </div>
              </div>
              {d.bound_jukebox ? (
                <span className="text-xs px-2 py-0.5 rounded-full bg-green-600/20 text-green-400 border border-green-600/30 shrink-0">
                  {d.bound_jukebox}
                </span>
              ) : (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleDelete(d.id)}
                  disabled={deletingId !== null}
                >
                  {deletingId === d.id ? (
                    <Loader2 className="w-4 h-4 animate-spin" />
                  ) : (
                    <Trash2 className="w-4 h-4 text-red-400" />
                  )}
                </Button>
              )}
            </div>
          </Card>
        ))
      )}

      {!showAdd && (
        <Card
          role="button"
          tabIndex={0}
          aria-label={t("settings.add")}
          className="group relative p-4 overflow-hidden border-dashed cursor-pointer hover:border-zinc-500 focus:border-green-500 focus:outline-none transition-colors"
          onClick={openAddCard}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              openAddCard();
            }
          }}
        >
          <div
            aria-hidden="true"
            className="flex items-center justify-between gap-3 min-w-0 blur-[3px] select-none pointer-events-none"
          >
            <div className="flex items-center gap-3 min-w-0 flex-1">
              <Volume2 className="w-4 h-4 text-green-500 shrink-0" />
              <div className="min-w-0 flex-1">
                <span className="block h-5 w-40 max-w-full rounded bg-zinc-700/80" />
                <span className="block h-4 w-64 max-w-full rounded bg-zinc-700/50" />
              </div>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <Button variant="ghost" size="sm" tabIndex={-1}>
                <Trash2 className="w-4 h-4 text-red-400" />
              </Button>
            </div>
          </div>
          <div className="absolute inset-0 flex items-center justify-center">
            <span className="w-9 h-9 rounded-full border-2 border-dashed border-zinc-600 bg-zinc-800/90 flex items-center justify-center shadow-lg transition-colors group-hover:border-green-500 group-focus:border-green-500">
              <Plus className="w-4 h-4 text-zinc-300 transition-colors group-hover:text-green-400 group-focus:text-green-400" />
            </span>
          </div>
        </Card>
      )}

      {showAdd && (
        <div ref={formRef}>
          <Card className="space-y-3">
            <select
              value={addForm.device_type}
              onChange={(e) => {
                const selectedType = e.target.value;
                setAddForm({
                  name: "",
                  device_type: selectedType,
                  device_id: "",
                  driver: selectedType === "local" ? "pulseaudio" : selectedType,
                });
              }}
              className="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white"
            >
              <option value="local">local</option>
              <option value="mpd">mpd</option>
              <option value="airplay">airplay</option>
            </select>

            {addForm.device_type === "local" && (
              <div className="space-y-1 max-h-40 overflow-y-auto border border-zinc-800 rounded-lg p-1">
                {filteredDetected.length === 0 ? (
                  <p className="text-xs text-zinc-500 text-center py-4">
                    {t("settings.allDetectedConfigured")}
                  </p>
                ) : (
                  filteredDetected.map((d: AudioDevice) => (
                    <button
                      key={d.id}
                      onClick={() => selectDetected(d)}
                      className={`w-full text-left px-3 py-2 rounded-lg text-sm transition-colors ${addForm.device_id === d.id ? "bg-green-600/20" : "hover:bg-zinc-800"}`}
                    >
                      <div className="text-white">{d.name}</div>
                      <div className="text-xs text-zinc-500">{d.id}</div>
                    </button>
                  ))
                )}
              </div>
            )}

            <div className="space-y-2">
              <Input
                value={addForm.name}
                onChange={(e) => setAddForm({ ...addForm, name: e.target.value })}
                placeholder={
                  addForm.device_type === "local"
                    ? t("settings.nameAutoFilled")
                    : t("settings.deviceName")
                }
              />
              <Input
                value={addForm.device_id}
                onChange={(e) => setAddForm({ ...addForm, device_id: e.target.value })}
                placeholder={t("settings.deviceId")}
              />
            </div>

            <div className="flex items-center gap-2">
              <Button
                variant="primary"
                size="sm"
                onClick={handleAdd}
                disabled={adding || !addForm.name || !addForm.device_id}
              >
                {adding ? <Loader2 className="w-4 h-4 animate-spin" /> : t("settings.create")}
              </Button>
              <Button
                size="sm"
                className="bg-zinc-600 hover:bg-zinc-500 text-white"
                onClick={closeForm}
              >
                {t("settings.cancel")}
              </Button>
            </div>
          </Card>
        </div>
      )}
    </div>
  );
}
