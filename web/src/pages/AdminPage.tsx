import { useState, useEffect } from "react"
import { useNavigate } from "react-router-dom"
import { api } from "../api/client"
import { getRole } from "../stores/auth"
import { Card } from "../components/ui/card"
import { Shield } from "lucide-react"

const roleLabel = (r: string) =>
  r === "super_admin" ? "Super Admin" : r === "admin" ? "Admin" : "User"

export default function AdminPage() {
  const navigate = useNavigate()
  const [users, setUsers] = useState<any[]>([])
  const [allowRegistration, setAllowRegistration] = useState(true)
  const [error, setError] = useState("")

  useEffect(() => {
    if (getRole() !== "super_admin" && getRole() !== "admin") { navigate("/settings"); return }
    loadData()
  }, [])

  async function loadData() {
    try {
      const [u, s] = await Promise.all([
        api.admin.users(),
        api.admin.getSettings(),
      ])
      setUsers(u.users)
      setAllowRegistration(s.allow_registration)
    } catch (err: any) { setError(err.error || "failed to load") }
  }

  async function updateRole(id: string, role: string) {
    try {
      await api.admin.updateRole(id, role)
      loadData()
    } catch (err: any) { setError(err.error || "failed") }
  }

  async function toggleRegistration() {
    try {
      await api.admin.updateSettings({ allow_registration: !allowRegistration })
      setAllowRegistration(!allowRegistration)
    } catch (err: any) { setError(err.error || "failed") }
  }

  const currentRole = getRole()

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">Administration</h1>

      {error && <p className="text-sm text-red-400">{error}</p>}

      <Card className="space-y-3">
        <h3 className="font-medium flex items-center gap-2"><Shield className="w-4 h-4" /> Server Settings</h3>
        <div className="flex items-center justify-between p-3 rounded-lg bg-zinc-800/50">
          <div>
            <p className="text-sm font-medium">Allow Registration</p>
            <p className="text-xs text-zinc-400">Allow new users to create accounts</p>
          </div>
          <button
            onClick={toggleRegistration}
            className={`relative w-12 h-6 rounded-full transition-colors cursor-pointer ${allowRegistration ? "bg-green-600" : "bg-zinc-700"}`}
          >
            <span className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${allowRegistration ? "translate-x-6" : ""}`} />
          </button>
        </div>
      </Card>

      <Card className="space-y-3">
        <h3 className="font-medium flex items-center gap-2"><Shield className="w-4 h-4" /> Users</h3>
        <div className="space-y-1">
          {users.map(u => (
            <div key={u.id} className="flex items-center justify-between p-3 rounded-lg bg-zinc-800/50 text-sm">
              <div className="flex-1 min-w-0">
                <span className="truncate block">{u.username}</span>
                <span className="text-zinc-500 text-xs">{u.email}</span>
              </div>
              <span className="text-zinc-400 w-20 text-center">{roleLabel(u.role)}</span>
              <div className="flex gap-2 w-36 justify-end">
                {currentRole === "super_admin" && u.role !== "super_admin" && (
                  <>
                    {u.role !== "admin" && (
                      <button onClick={() => updateRole(u.id, "admin")}
                        className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer">
                        Promote
                      </button>
                    )}
                    {u.role !== "user" && (
                      <button onClick={() => updateRole(u.id, "user")}
                        className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer">
                        Demote
                      </button>
                    )}
                  </>
                )}
                {currentRole === "admin" && u.role === "user" && (
                  <button onClick={() => updateRole(u.id, "admin")}
                    className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer">
                    Promote
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      </Card>
    </div>
  )
}
