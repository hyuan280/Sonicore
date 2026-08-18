import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { useJukebox } from "../stores/jukebox";
import { api } from "../api/client";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Plus, Loader2, Turntable, ChevronRight, Trash2, Shield, X } from "lucide-react";

export default function JukeboxPage() {
  const { t } = useTranslation();
  const { list, loading, loadList, create, delete: delJbx } = useJukebox();
  const [showCreate, setShowCreate] = useState(false);
  const role = localStorage.getItem("role");
  const isAdmin = role === "admin" || role === "super_admin";
  const [delId, setDelId] = useState<string | null>(null);
  const [searchQ, setSearchQ] = useState("");

  const handleDelete = async (id: string) => {
    await delJbx(id);
    setDelId(null);
  };

  useEffect(() => {
    loadList();
  }, []);

  const filtered = searchQ.trim()
    ? list.filter((j: any) => j.name.toLowerCase().includes(searchQ.trim().toLowerCase()))
    : list;

  return (
    <div className="p-6 space-y-6 pb-24">
      <div className="relative flex items-center">
        <h1 className="text-2xl font-bold shrink-0">{t("nav.jukeboxes")}</h1>
        <div className="absolute left-1/2 -translate-x-1/2 w-full max-w-[60%] min-w-[200px] px-4">
          <input
            type="text"
            placeholder={t("search.searchJukeboxes")}
            value={searchQ}
            onChange={(e) => setSearchQ(e.target.value)}
            className="w-full px-3 py-1.5 pr-8 text-sm bg-zinc-800 text-zinc-300 border-none outline-none placeholder-zinc-500"
          />
          {searchQ && (
            <button
              onClick={() => setSearchQ("")}
              className="absolute right-5 top-1/2 -translate-y-1/2 p-1 text-zinc-500 hover:text-white cursor-pointer"
            >
              <X className="w-4 h-4" />
            </button>
          )}
        </div>
        <div className="flex-1" />
        {isAdmin && (
          <Button variant="primary" size="sm" onClick={() => setShowCreate(true)}>
            <Plus className="w-4 h-4 mr-1" /> {t("jukebox.newJukebox")}
          </Button>
        )}
      </div>

      {loading && filtered.length === 0 && (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="w-6 h-6 animate-spin text-zinc-500" />
        </div>
      )}

      {!loading && filtered.length === 0 && (
        <div className="text-center py-20 text-zinc-500">
          <Turntable className="w-12 h-12 mx-auto mb-3 opacity-30" />
          <p>{t("jukebox.noJukeboxes")}</p>
          {isAdmin ? (
            <p className="text-sm mt-1">{t("jukebox.getStarted")}</p>
          ) : (
            <p className="text-sm mt-1 flex items-center justify-center gap-1">
              <Shield className="w-3 h-3" /> {t("jukebox.contactAdmin")}
            </p>
          )}
        </div>
      )}

      <div className="space-y-2">
        {filtered.map((j) => (
          <div
            key={j.id}
            className="flex items-center gap-3 p-4 border border-zinc-800 rounded-xl bg-zinc-900/50 hover:bg-zinc-800/30 transition-colors group"
          >
            <Link to={`/jukebox/${j.id}`} className="flex-1 min-w-0 flex items-center gap-3">
              <Turntable
                className={`w-10 h-10 shrink-0 ${j.is_playing ? "text-green-500" : "text-zinc-600"}`}
              />
              <div className="flex-1 min-w-0">
                <div className="font-medium">{j.name}</div>
                <div className="text-xs text-zinc-500">{j.device_name || j.device_id}</div>
              </div>
            </Link>
            <div className="flex items-center gap-2 flex-shrink-0">
              {isAdmin &&
                (delId === j.id ? (
                  <>
                    <button
                      onClick={() => handleDelete(j.id)}
                      className="text-xs px-2 py-1 rounded bg-red-600/20 text-red-400 hover:bg-red-600/30 cursor-pointer"
                    >
                      {t("jukebox.delete")}
                    </button>
                    <button
                      onClick={() => setDelId(null)}
                      className="text-xs px-2 py-1 rounded text-zinc-500 hover:text-white cursor-pointer"
                    >
                      {t("jukebox.no")}
                    </button>
                  </>
                ) : (
                  <button
                    onClick={() => setDelId(j.id)}
                    className="opacity-0 group-hover:opacity-100 p-1.5 rounded text-zinc-500 hover:text-red-400 hover:bg-zinc-800 transition-all cursor-pointer"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                ))}
              <ChevronRight className="w-4 h-4 text-zinc-600" />
            </div>
          </div>
        ))}
      </div>

      {showCreate && (
        <CreateModal
          onClose={() => setShowCreate(false)}
          onCreate={async (name, configId) => {
            await create(name, configId);
            setShowCreate(false);
          }}
        />
      )}
    </div>
  );
}

function CreateModal({
  onClose,
  onCreate,
}: {
  onClose: () => void;
  onCreate: (name: string, configId: string) => Promise<void>;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [configId, setConfigId] = useState("");
  const [devices, setDevices] = useState<any[]>([]);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    api.jukebox
      .availableDeviceConfigs()
      .then((d) => {
        setDevices(d.devices || []);
        if (d.devices?.length > 0) setConfigId(d.devices[0].id);
      })
      .catch(() => {});
  }, []);

  const handleCreate = async () => {
    if (!name.trim() || !configId) return;
    setSubmitting(true);
    try {
      await onCreate(name.trim(), configId);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="bg-zinc-900 border border-zinc-700 rounded-xl p-6 w-full max-w-md shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-bold mb-4">{t("jukebox.createJukebox")}</h2>
        <div className="space-y-4">
          <div>
            <label className="text-sm text-zinc-400 block mb-1">{t("jukebox.name")}</label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Living Room"
              onKeyDown={(e) => e.key === "Enter" && handleCreate()}
            />
          </div>
          <div>
            <label className="text-sm text-zinc-400 block mb-1">{t("jukebox.audioDevice")}</label>
            {devices.length === 0 ? (
              <div className="text-sm text-zinc-500 py-2 border border-zinc-800 rounded-lg px-3">
                {t("jukebox.noDevices")}
              </div>
            ) : (
              <div className="space-y-1 max-h-48 overflow-y-auto">
                {devices.map((d: any) => (
                  <label
                    key={d.id}
                    className={`flex items-center gap-2 px-3 py-2 rounded-lg cursor-pointer transition-colors ${
                      configId === d.id
                        ? "bg-green-600/20 border border-green-600/30"
                        : "hover:bg-zinc-800 border border-transparent"
                    }`}
                  >
                    <input
                      type="radio"
                      name="device"
                      value={d.id}
                      checked={configId === d.id}
                      onChange={() => setConfigId(d.id)}
                      className="accent-green-500"
                    />
                    <div>
                      <div className="text-sm">{d.name}</div>
                      <div className="text-xs text-zinc-500">
                        {d.device_type} · {d.driver} · {d.device_id}
                      </div>
                    </div>
                  </label>
                ))}
              </div>
            )}
          </div>
        </div>
        <div className="flex justify-end gap-2 mt-6">
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button
            variant="primary"
            onClick={handleCreate}
            disabled={submitting || !name.trim() || !configId}
          >
            {submitting ? <Loader2 className="w-4 h-4 animate-spin" /> : t("common.create")}
          </Button>
        </div>
      </div>
    </div>
  );
}
