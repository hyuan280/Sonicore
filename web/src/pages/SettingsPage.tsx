import { useState, useRef, useCallback, useEffect } from "react"
import { useAuth } from "../stores/auth"
import { useLibrary } from "../stores/library"
import { usePlayer } from "../stores/player"
import { Button } from "../components/ui/button"
import { Input } from "../components/ui/input"
import { Card } from "../components/ui/card"
import { api } from "../api/client"
import { Music, ScanSearch, ColumnsSettings, Trash2, Plus, FolderOpen, Loader2, UserRound, SquareLibrary, Speaker, Volume2, Search, X, Upload, FileEdit } from "lucide-react"
import { formatDuration } from "../lib/utils"
import DirectoryPicker from "../components/DirectoryPicker"

const roleLabel = (r: string) =>
  r === "super_admin" ? "Super Admin" : r === "admin" ? "Admin" : "User"

export default function SettingsPage() {
  const { user, logout } = useAuth()
  const { libraries, load: reloadLibs } = useLibrary()
  const [showForm, setShowForm] = useState(false)
  const [name, setName] = useState("")
  const [path, setPath] = useState("")
  const [scanning, setScanning] = useState<Record<string, { scanned: number; total: number }>>({})
  const pollingRef = useRef<Record<string, boolean>>({})

  const [dirPickerOpen, setDirPickerOpen] = useState(false)


  const [scanDialogLib, setScanDialogLib] = useState<string | null>(null)
  const [scanOverwrite, setScanOverwrite] = useState(false)
  const [manageLib, setManageLib] = useState<any>(null)
  const [manageTracks, setManageTracks] = useState<any[]>([])
  const [searching, setSearching] = useState("")
  const [searchModal, setSearchModal] = useState<any>(null)

  useEffect(() => {
    if (!manageLib) { setManageTracks([]); return }
    api.data.tracks(manageLib.id, 1, 9999).then(d => setManageTracks(d.items || [])).catch(() => {})
  }, [manageLib?.id])

  const [showPwModal, setShowPwModal] = useState(false)
  const [pwForm, setPwForm] = useState({ oldPw: "", newPw: "", confirmPw: "" })
  const [pwError, setPwError] = useState("")
  const [pwSaving, setPwSaving] = useState(false)

  const isAdmin = user?.role === "admin" || user?.role === "super_admin"

  // On unmount, stop all polling
  useEffect(() => {
    return () => { pollingRef.current = {} }
  }, [])

  // On mount / libraries loaded, check for active scans
  useEffect(() => {
    if (!isAdmin || libraries.length === 0) return
    libraries.forEach(lib => {
      api.libraries.scanStatus(lib.id).then(status => {
        if (status.status === "running") {
          pollingRef.current[lib.id] = true
          setScanning(prev => ({ ...prev, [lib.id]: { scanned: status.scanned, total: status.total_files } }))
          setTimeout(() => pollScan(lib.id), 1000)
        }
      }).catch(() => {})
    })
  }, [libraries.length])

  const pollScan = useCallback(async (libId: string) => {
    if (!pollingRef.current[libId]) return
    try {
      const status = await api.libraries.scanStatus(libId)
      if (!pollingRef.current[libId]) return
      if (status.status === "running") {
        setScanning(prev => ({ ...prev, [libId]: { scanned: status.scanned, total: status.total_files } }))
        setTimeout(() => pollScan(libId), 1000)
      } else {
        setScanning(prev => { const n = { ...prev }; delete n[libId]; return n })
        pollingRef.current[libId] = false
        reloadLibs()
      }
    } catch {
      if (pollingRef.current[libId] === false) return
      setTimeout(() => pollScan(libId), 1000)
    }
  }, [reloadLibs])

  const startScan = useCallback(async (id: string, mode?: string) => {
    pollingRef.current[id] = true
    setScanning(prev => ({ ...prev, [id]: { scanned: 0, total: 0 } }))
    try {
      await api.libraries.scan(id, mode)
      pollScan(id)
    } catch {
      setScanning(prev => { const n = { ...prev }; delete n[id]; return n })
      pollingRef.current[id] = false
    }
  }, [pollScan])

  const create = async () => {
    await api.libraries.create({ name, path })
    setName(""); setPath(""); setShowForm(false)
    reloadLibs()
  }

  const del = async (id: string) => {
    if (confirm("Delete this library?")) {
      await api.libraries.delete(id)
      reloadLibs()
      api.user.getQueue().then((data: any) => {
        if (data?.tracks) {
          usePlayer.setState({
            queue: data.tracks.map((t: any) => ({
              id: t.id, title: t.title, artist: t.artist, album: t.album,
              album_id: t.album_id, duration: t.duration, suffix: t.suffix,
              cover_image_id: t.cover_image_id,
            })),
            queueIdx: data.queue_idx ?? 0,
            shuffleOrder: data.shuffle_order ?? [],
            shuffleIdx: data.shuffle_idx ?? 0,
            mode: data.mode ?? "normal",
          })
        }
        if (!data?.tracks?.length) {
          usePlayer.setState({ track: null, playing: false })
        }
      }).catch(() => {})
    }
  }

  const changePassword = async () => {
    setPwSaving(true); setPwError("")
    if (pwForm.newPw !== pwForm.confirmPw) {
      setPwError("New passwords do not match"); setPwSaving(false); return
    }
    try {
      await api.auth.changePassword(pwForm.oldPw, pwForm.newPw)
      setShowPwModal(false)
      setPwForm({ oldPw: "", newPw: "", confirmPw: "" })
    } catch (err: any) { setPwError(err.error || "Failed") }
    setPwSaving(false)
  }

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">Settings</h1>

      <Card className="space-y-3">
        <h3 className="font-medium flex items-center gap-2"><UserRound className="w-4 h-4" /> Account</h3>
        <div className="space-y-1 p-3 rounded-lg bg-zinc-800/50">
          <p className="text-sm text-zinc-400">Username: {user?.username}</p>
          <p className="text-sm text-zinc-400">Email: {user?.email}</p>
          <p className="text-sm text-zinc-400">Role: <span className="text-green-500">{roleLabel(user?.role || "")}</span></p>
        </div>
        <Button variant="primary" size="sm" onClick={() => setShowPwModal(true)}>Change Password</Button>
      </Card>

      {isAdmin && (
        <Card className="space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="font-medium flex items-center gap-2"><SquareLibrary className="w-4 h-4" /> Libraries</h3>
            <Button size="sm" onClick={() => setShowForm(!showForm)}><Plus className="w-4 h-4 mr-1" />Add</Button>
          </div>

          {showForm && (
            <div className="space-y-3 p-3 rounded-lg bg-zinc-800">
              <Input placeholder="Library name" value={name} onChange={e => setName(e.target.value)} />
              <div>
                <button onClick={() => setDirPickerOpen(true)}
                  className="w-full flex items-center gap-2 bg-zinc-800 text-sm text-zinc-300 rounded-lg px-3 py-2.5 border border-zinc-700 hover:border-zinc-500 cursor-pointer text-left">
                  <FolderOpen className="w-4 h-4 text-yellow-500 shrink-0" />
                  <span className="truncate flex-1">{path || "Select music directory..."}</span>
                </button>
              </div>
              <DirectoryPicker open={dirPickerOpen} initialPath={path} onClose={() => setDirPickerOpen(false)} onSelect={setPath} />
              <Button size="sm" onClick={create}>Create</Button>
            </div>
          )}

          <div className="space-y-2">
            {libraries.map(lib => {
              const prog = scanning[lib.id]
              return (
                <div key={lib.id} className="flex items-center justify-between p-3 rounded-lg bg-zinc-800/50">
                  <div className="flex items-center gap-3 min-w-0">
                    <Music className="w-4 h-4 text-green-500 shrink-0" />
                    <div className="min-w-0">
                      <p className="text-sm font-medium truncate">{lib.name}</p>
                      <p className="text-xs text-zinc-500 truncate">{lib.track_count} tracks · {lib.path}</p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2 shrink-0">
                    {prog ? (
                      <span className="text-sm font-semibold text-zinc-300 tabular-nums">
                        {prog.scanned}/{prog.total || "?"}
                      </span>
                    ) : null}
                    <Button variant="ghost" size="sm" onClick={() => setManageLib(lib)} disabled={!!prog}>
                      <ColumnsSettings className="w-4 h-4" />
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => { setScanDialogLib(lib.id); setScanOverwrite(false) }} disabled={!!prog}>
                      {prog ? <Loader2 className="w-4 h-4 animate-spin" /> : <ScanSearch className="w-4 h-4" />}
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => del(lib.id)}>
                      <Trash2 className="w-4 h-4 text-red-400" />
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>
        </Card>
      )}

      {isAdmin && <DeviceManager />}

      {showPwModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm" onClick={() => setShowPwModal(false)}>
          <div className="bg-zinc-900 border border-zinc-700 rounded-xl p-6 w-full max-w-md shadow-xl space-y-4" onClick={e => e.stopPropagation()}>
            <h2 className="text-lg font-bold">Change Password</h2>

            {pwError && <p className="text-sm text-red-400">{pwError}</p>}

            <Input type="password" placeholder="Current password" value={pwForm.oldPw}
              onChange={e => setPwForm({...pwForm, oldPw: e.target.value})} />
            <Input type="password" placeholder="New password" value={pwForm.newPw}
              onChange={e => setPwForm({...pwForm, newPw: e.target.value})} />
            <Input type="password" placeholder="Confirm new password" value={pwForm.confirmPw}
              onChange={e => setPwForm({...pwForm, confirmPw: e.target.value})}
              onKeyDown={e => e.key === "Enter" && changePassword()} />

            <div className="flex justify-end gap-2 pt-2">
              <Button variant="ghost" onClick={() => { setShowPwModal(false); setPwError(""); setPwForm({oldPw:"",newPw:"",confirmPw:""}) }}>Cancel</Button>
              <Button variant="primary" onClick={changePassword} disabled={pwSaving || !pwForm.oldPw || !pwForm.newPw || !pwForm.confirmPw}>
                {pwSaving ? <Loader2 className="w-4 h-4 animate-spin" /> : "Update"}
              </Button>
            </div>
          </div>
        </div>
      )}

      <Button variant="danger" onClick={logout}>Sign Out</Button>

      {/* Scan dialog */}
      {scanDialogLib && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm" onClick={() => setScanDialogLib(null)}>
          <div className="bg-zinc-900 border border-zinc-700 rounded-xl p-6 w-full max-w-md shadow-xl space-y-4" onClick={e => e.stopPropagation()}>
            <h2 className="text-lg font-bold">Scan Library</h2>
            <label className="flex items-center gap-3 p-3 rounded-lg bg-zinc-800 cursor-pointer" onClick={() => setScanOverwrite(false)}>
              <input type="radio" checked={!scanOverwrite} onChange={() => setScanOverwrite(false)} className="accent-green-500" />
              <div>
                <p className="text-sm font-medium">Search missing metadata</p>
                <p className="text-xs text-zinc-400">Only fill empty fields from MusicBrainz</p>
              </div>
            </label>
            <label className="flex items-center gap-3 p-3 rounded-lg bg-zinc-800 cursor-pointer" onClick={() => setScanOverwrite(true)}>
              <input type="radio" checked={scanOverwrite} onChange={() => setScanOverwrite(true)} className="accent-green-500" />
              <div>
                <p className="text-sm font-medium">Overwrite all metadata</p>
                <p className="text-xs text-zinc-400">Replace existing data with MusicBrainz results</p>
              </div>
            </label>
            <div className="flex justify-end gap-2 pt-2">
              <Button variant="ghost" onClick={() => setScanDialogLib(null)}>Cancel</Button>
              <Button variant="primary" onClick={() => { const id = scanDialogLib; const mode = scanOverwrite ? "overwrite" : "missing"; setScanDialogLib(null); startScan(id, mode) }}>Start Scan</Button>
            </div>
          </div>
        </div>
      )}

      {/* Library manage modal (full-screen) */}
      {manageLib && (
        <div className="fixed inset-0 z-50 bg-zinc-950 flex flex-col">
          {/* Header */}
          <div className="flex items-center justify-between px-6 py-4 border-b border-zinc-800 shrink-0">
            <div>
              <h2 className="text-lg font-bold">{manageLib.name}</h2>
              <p className="text-sm text-zinc-500">{manageLib.track_count} tracks</p>
            </div>
            <button onClick={() => { setManageLib(null); setManageTracks([]) }}
              className="p-2 rounded-lg hover:bg-zinc-800 cursor-pointer">
              <X className="w-5 h-5" />
            </button>
          </div>
          {/* Track list */}
          <div className="flex-1 overflow-y-auto p-6">
            {manageTracks.length === 0 ? (
              <div className="flex items-center justify-center h-full text-zinc-500">
                <Loader2 className="w-5 h-5 animate-spin mr-2" /> Loading tracks...
              </div>
            ) : (
              <div className="space-y-1">
                <div className="flex items-center gap-2 text-xs text-zinc-500 px-4 py-2 border-b border-zinc-800">
                  <span className="flex-1 min-w-0">Title</span>
                  <span className="w-32 shrink-0 text-center hidden sm:block">Artist</span>
                  <span className="w-32 shrink-0 text-center hidden sm:block">Album</span>
                  <span className="w-16 shrink-0 text-center">Format</span>
                  <span className="w-16 shrink-0 text-center">Duration</span>
                  <span className="w-36 shrink-0 text-center">Actions</span>
                </div>
                {manageTracks.map((t: any) => (
                  <div key={t.id} className="flex items-center gap-2 px-4 py-2 rounded-lg hover:bg-zinc-800/50 text-sm group">
                    <span className="flex-1 min-w-0 truncate">{t.title}</span>
                    <span className="w-32 shrink-0 truncate text-center text-zinc-400 hidden sm:block">{t.artist || ""}</span>
                    <span className="w-32 shrink-0 truncate text-center text-zinc-500 hidden sm:block">{t.album || ""}</span>
                    <span className="w-16 shrink-0 text-center text-zinc-500">{t.suffix || t.file_format || ""}</span>
                    <span className="w-16 shrink-0 text-center text-zinc-400">{formatDuration(t.duration)}</span>
                    <span className="w-36 shrink-0 flex items-center justify-end gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                      <button onClick={async () => {
                        setSearching(t.id); setSearchModal({ track: t, edit: {} })
                        try {
                          const res = await fetch("/api/metadata/search", {
                            method: "POST",
                            headers: {"Content-Type":"application/json", "Authorization": "Bearer " + localStorage.getItem("token")},
                            body: JSON.stringify({ track_id: t.id, title: t.title, artist: t.artist || "", album: t.album || "" }),
                          })
                          setSearchModal({ track: t, result: await res.json(), edit: {} })
                        } catch { setSearchModal({ track: t, error: "Search failed" }) }
                        setSearching("")
                      }}
                        className="p-1 rounded text-zinc-500 hover:text-green-400 cursor-pointer" title="Identify with MusicBrainz">
                        {searching === t.id ? <Loader2 className="w-4 h-4 animate-spin" /> : <ScanSearch className="w-4 h-4" />}
                      </button>
                      <button onClick={() => {
                        const mbid = prompt("Enter MusicBrainz recording ID:")
                        if (mbid) fetch("/api/metadata/identify", { method: "POST", headers: {"Content-Type":"application/json", "Authorization": "Bearer " + localStorage.getItem("token")}, body: JSON.stringify({ track_id: t.id, mbid }) }).then(r => r.ok ? alert("Updated!") : alert("Failed")).catch(() => {})
                      }} className="p-1 rounded text-zinc-500 hover:text-blue-400 cursor-pointer" title="Manual identify (enter MBID)">
                        <FileEdit className="w-4 h-4" />
                      </button>
                      <button onClick={() => {/* TODO: edit metadata (cover, lyrics) */}}
                        className="p-1 rounded text-zinc-500 hover:text-yellow-400 cursor-pointer" title="Edit metadata (cover / lyrics)">
                        <Upload className="w-4 h-4" />
                      </button>
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
      {searchModal &&       <SearchResultModal data={searchModal} onClose={() => setSearchModal(null)} onUpdate={(edit) => setSearchModal((prev: any) => ({ ...prev, edit }))} />}
    </div>
  )
}

function SearchResultModal({ data, onClose, onUpdate }: { data: any; onClose: () => void; onUpdate: (e: any) => void }) {
  const [saving, setSaving] = useState(false)
  return (
    <div className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center" onClick={onClose}>
      <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-6 w-full max-w-md" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4">
          <h3 className="font-bold">MusicBrainz Match</h3>
          <button onClick={onClose} className="p-1 rounded hover:bg-zinc-800 cursor-pointer"><X className="w-4 h-4" /></button>
        </div>
        <p className="text-xs text-zinc-500 mb-4 truncate">{data.track?.title}</p>
        {data.result == null ? (
          <div className="space-y-3">
            <div className="w-full h-1 bg-zinc-700 rounded-full overflow-hidden">
              <div className="h-full bg-green-500 animate-pulse rounded-full w-full" />
            </div>
            <p className="text-sm text-zinc-400 text-center">Searching MusicBrainz...</p>
          </div>
        ) : data.error ? (
          <p className="text-sm text-red-400">{data.error}</p>
        ) : data.result.matched ? (
          <div className="space-y-2 text-sm">
            {(["title","artist","album","year","genre"] as const).map(f => (
              <div key={f} className="flex items-center gap-2">
                <span className="text-zinc-400 w-14 shrink-0 text-right">{f.charAt(0).toUpperCase()+f.slice(1)}</span>
                <input
                  value={data.edit[f] ?? (data.result[f] || "")}
                  onChange={e => onUpdate({ ...data.edit, [f]: e.target.value })}
                  className="bg-zinc-800 rounded px-2 py-0.5 text-sm text-right flex-1 min-w-0 focus:outline-none focus:ring-1 focus:ring-green-500"
                />
              </div>
            ))}
            <div className="flex justify-between">
              <span className="text-zinc-400">MBID</span>
              <span className="text-xs font-mono text-zinc-500">{data.result.track_mbid}</span>
            </div>
            {data.result.file_hash && (
              <div className="flex justify-between">
                <span className="text-zinc-400">Hash</span>
                <span className="text-xs font-mono text-zinc-600 truncate ml-2 max-w-[200px]">{data.result.file_hash}</span>
              </div>
            )}
          </div>
        ) : (
          <p className="text-sm text-zinc-400">No match found on MusicBrainz</p>
        )}
        {data.result?.matched && (
          <button onClick={async () => {
            setSaving(true)
            try {
              await fetch("/api/metadata/save", {
                method: "POST",
                headers: {"Content-Type":"application/json", "Authorization": "Bearer " + localStorage.getItem("token")},
                body: JSON.stringify({
                  track_id: data.track?.id || "",
                  file_hash: data.result.file_hash || data.track?.file_hash || "",
                  track_mbid: data.result.track_mbid || "",
                  title: data.edit.title ?? data.result.title ?? "",
                  artist: data.edit.artist ?? data.result.artist ?? "",
                  album: data.edit.album ?? data.result.album ?? "",
                  year: parseInt(data.edit.year) || data.result.year || 0,
                  genre: data.edit.genre ?? data.result.genre ?? "",
                }),
              })
              onClose()
            } catch {}
            setSaving(false)
          }} disabled={saving}
            className="mt-4 w-full py-2 rounded-lg text-sm bg-green-600 text-white hover:bg-green-500 disabled:opacity-50 cursor-pointer">
            {saving ? "Saving..." : "Save & Use for This Track"}
          </button>
        )}
      </div>
    </div>
  )
}

function DeviceManager() {
  const [devices, setDevices] = useState<any[]>([])
  const [detected, setDetected] = useState<any[]>([])
  const [loading, setLoading] = useState(true)
  const [showAdd, setShowAdd] = useState(false)
  const [addForm, setAddForm] = useState({ name: "", device_type: "local", device_id: "", driver: "pulseaudio" })
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState("")

  const load = () => {
    setLoading(true)
    Promise.all([api.jukebox.deviceConfigs(), api.jukebox.audioDevices()])
      .then(([configs, d]) => {
        setDevices(configs.devices || [])
        setDetected(d.devices || [])
      }).finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const handleAdd = async () => {
    if (!addForm.name || !addForm.device_id) return
    setAdding(true)
    setError("")
    try {
      await api.jukebox.createDeviceConfig({
        name: addForm.name, device_type: addForm.device_type,
        device_id: addForm.device_id, driver: addForm.driver,
      })
      setShowAdd(false)
      setAddForm({ name: "", device_type: "local", device_id: "", driver: "pulseaudio" })
      load()
    } catch (e: any) {
      setError(e.error || e.message || "Failed to create")
    } finally { setAdding(false) }
  }

  const handleDelete = async (id: string) => {
    try { await api.jukebox.deleteDeviceConfig(id); load() } catch {}
  }

  const selectDetected = (d: any) => {
    setAddForm({
      name: d.name || d.id,
      device_type: "local",
      device_id: d.id,
      driver: d.id.startsWith("hw:") ? "alsa" : "pulseaudio",
    })
  }

  const existingIds = new Set(devices.map((d: any) => d.device_id + ":" + d.driver))
  const existingNames = new Set(devices.map((d: any) => d.name))
  const filteredDetected = detected.filter((d: any) =>
    !existingIds.has(d.id + ":" + (d.id.startsWith("hw:") ? "alsa" : "pulseaudio")) &&
    !existingNames.has(d.name || d.id)
  )

  return (
    <Card className="p-4 space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold text-zinc-400 flex items-center gap-2">
          <Speaker className="w-4 h-4" /> Audio Devices
        </h2>
        <Button variant="ghost" size="sm" onClick={() => setShowAdd(!showAdd)}>
          <Plus className="w-4 h-4 mr-1" /> Add
        </Button>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-8"><Loader2 className="w-5 h-5 animate-spin text-zinc-500" /></div>
      ) : (
        <>
          {showAdd && (
            <div className="p-3 border border-zinc-700 rounded-lg space-y-3">
              {error && <p className="text-xs text-red-400">{error}</p>}

              <select value={addForm.device_type} onChange={e => {
                const t = e.target.value
                setAddForm({ name: "", device_type: t, device_id: "", driver: t === "local" ? "pulseaudio" : t })
              }}
                className="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white">
                <option value="local">local</option>
                <option value="mpd">mpd</option>
                <option value="airplay">airplay</option>
              </select>

              {addForm.device_type === "local" && (
                <div className="space-y-1 max-h-40 overflow-y-auto border border-zinc-800 rounded-lg p-1">
                  {filteredDetected.length === 0 ? (
                    <p className="text-xs text-zinc-500 text-center py-4">All detected devices are already configured</p>
                  ) : filteredDetected.map((d: any) => (
                    <button key={d.id} onClick={() => selectDetected(d)}
                      className={`w-full text-left px-3 py-2 rounded-lg text-sm transition-colors ${addForm.device_id === d.id ? "bg-green-600/20" : "hover:bg-zinc-800"}`}>
                      <div className="text-white">{d.name}</div>
                      <div className="text-xs text-zinc-500">{d.id}</div>
                    </button>
                  ))}
                </div>
              )}

              <div className="space-y-2">
                <Input value={addForm.name} onChange={e => setAddForm({...addForm, name: e.target.value})}
                  placeholder={addForm.device_type === "local" ? "Name (auto-filled)" : "Device Name"} />
                <Input value={addForm.device_id} onChange={e => setAddForm({...addForm, device_id: e.target.value})}
                  placeholder="Device ID" />
              </div>

              <div className="flex justify-end gap-2">
                <Button variant="ghost" size="sm" onClick={() => { setShowAdd(false); setError("") }}>Cancel</Button>
                <Button variant="primary" size="sm" onClick={handleAdd}
                  disabled={adding || !addForm.name || !addForm.device_id}>
                  {adding ? <Loader2 className="w-4 h-4 animate-spin" /> : "Create"}
                </Button>
              </div>
            </div>
          )}
          {devices.length === 0 ? (
            <p className="text-zinc-500 text-center py-4">No devices configured</p>
          ) : (
            <div className="space-y-2">
              {devices.map((d: any) => (
                <div key={d.id} className="flex items-center gap-3 p-3 border border-zinc-800 rounded-lg">
                  <Volume2 className="w-4 h-4 text-green-500 shrink-0" />
                  <div className="flex-1 min-w-0">
                    <div className="text-sm">{d.name}</div>
                    <div className="text-xs text-zinc-500">{d.device_type} · {d.driver} · {d.device_id}</div>
                  </div>
                  {d.bound_jukebox ? (
                    <span className="text-xs px-2 py-0.5 rounded-full bg-green-600/20 text-green-400 border border-green-600/30">
                      {d.bound_jukebox}
                    </span>
                  ) : (
                    <Button variant="ghost" size="sm" onClick={() => handleDelete(d.id)}>
                      <Trash2 className="w-4 h-4 text-red-400" />
                    </Button>
                  )}
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </Card>
  )
}
