import { useState, useRef, useCallback, useEffect } from "react"
import { useAuth } from "../stores/auth"
import { useLibrary } from "../stores/library"
import { Button } from "../components/ui/button"
import { Input } from "../components/ui/input"
import { Card } from "../components/ui/card"
import { api } from "../api/client"
import { Music, Scan, Trash2, Plus, FolderOpen, Loader2, UserRound, SquareLibrary, Speaker, Volume2 } from "lucide-react"
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

  const startScan = useCallback(async (id: string) => {
    pollingRef.current[id] = true
    setScanning(prev => ({ ...prev, [id]: { scanned: 0, total: 0 } }))
    try {
      await api.libraries.scan(id)
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
                    <Button variant="ghost" size="sm" onClick={() => startScan(lib.id)} disabled={!!prog}>
                      {prog ? <Loader2 className="w-4 h-4 animate-spin" /> : <Scan className="w-4 h-4" />}
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
