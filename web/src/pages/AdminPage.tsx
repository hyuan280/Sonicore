import { useState, useEffect } from "react"
import { useTranslation } from "react-i18next"
import { translateApiError } from "../i18n/errorCodes"
import { useNavigate } from "react-router-dom"
import { api } from "../api/client"
import { getRole } from "../stores/auth"
import { Card } from "../components/ui/card"
import { Input } from "../components/ui/input"
import { Shield, Users } from "lucide-react"

export default function AdminPage() {
  const { t } = useTranslation()
  const roleLabels: Record<string, string> = { super_admin: t("admin.roleSuperAdmin"), admin: t("admin.roleAdmin"), user: t("admin.roleUser") }
  const navigate = useNavigate()
  const [users, setUsers] = useState<any[]>([])
  const [allowRegistration, setAllowRegistration] = useState(true)
  const [mbEnabled, setMbEnabled] = useState(false)
  const [mbApiUrl, setMbApiUrl] = useState("")
  const [mbRateLimit, setMbRateLimit] = useState("1")
  const [mbSaving, setMbSaving] = useState(false)
  const [mbModified, setMbModified] = useState(false)
  const [mbInit, setMbInit] = useState({ enabled: false, apiUrl: "", rateLimit: "1" })
  const [mbError, setMbError] = useState("")
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
      setMbEnabled(s.metadata_musicbrainz_enabled)
      setMbApiUrl(s.metadata_musicbrainz_api_url || "")
      setMbRateLimit(s.metadata_musicbrainz_rate_limit || "1")
      setMbInit({
        enabled: s.metadata_musicbrainz_enabled,
        apiUrl: s.metadata_musicbrainz_api_url || "",
        rateLimit: s.metadata_musicbrainz_rate_limit || "1",
      })
    } catch (err: any) { setError(translateApiError(t, err)) }
  }

  async function updateRole(id: string, role: string) {
    try {
      await api.admin.updateRole(id, role)
      loadData()
    } catch (err: any) { setError(translateApiError(t, err)) }
  }

  async function toggleRegistration() {
    try {
      await api.admin.updateSettings({ allow_registration: !allowRegistration })
      setAllowRegistration(!allowRegistration)
    } catch (err: any) { setError(translateApiError(t, err)) }
  }

  const currentRole = getRole()

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">{t("admin.administration")}</h1>

      {error && <p className="text-sm text-red-400">{error}</p>}

      <Card className="space-y-3">
        <h3 className="font-medium flex items-center gap-2"><Shield className="w-4 h-4" /> {t("admin.serverSettings")}</h3>
        <div className="flex items-center justify-between p-3 rounded-lg bg-zinc-800/50">
          <div>
            <p className="text-sm font-medium">{t("admin.allowRegistration")}</p>
            <p className="text-xs text-zinc-400">{t("admin.allowRegistrationDesc")}</p>
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
        <h3 className="font-medium flex items-center gap-2"><img src="/musicbrainz.svg" className="w-4 h-4" /> {t("admin.musicbrainz")}</h3>
        <div className="space-y-3 p-3 rounded-lg bg-zinc-800/50">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium">{t("admin.enableMusicBrainz")}</p>
              <p className="text-xs text-zinc-400">{t("admin.enableMusicBrainzDesc")}</p>
            </div>
            <button onClick={() => { setMbEnabled(!mbEnabled); setMbModified(true) }}
              className={`relative w-12 h-6 rounded-full transition-colors cursor-pointer ${mbEnabled ? "bg-green-600" : "bg-zinc-700"}`}>
              <span className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${mbEnabled ? "translate-x-6" : ""}`} />
            </button>
          </div>
          <div>
            <p className="text-xs text-zinc-400 mb-1">{t("admin.apiUrl")}</p>
            <Input value={mbApiUrl} onChange={e => { setMbApiUrl(e.target.value); setMbModified(true) }}
              placeholder="https://musicbrainz.org/ws/2" />
          </div>
          <div>
            <p className="text-xs text-zinc-400 mb-1">{t("admin.rateLimit")}</p>
            <Input value={mbRateLimit} onChange={e => { setMbRateLimit(e.target.value); setMbModified(true) }}
              placeholder="1" />
          </div>
          {mbModified && (
          <div className="flex items-center gap-3">
          <button onClick={async () => {
            setMbSaving(true); setMbError("")
            try {
              await api.admin.updateSettings({
                metadata_musicbrainz_enabled: mbEnabled,
                metadata_musicbrainz_api_url: mbApiUrl || undefined,
                metadata_musicbrainz_rate_limit: mbRateLimit || undefined,
              })
              setMbModified(false)
            } catch (err: any) { setMbError(translateApiError(t, err)) }
            setMbSaving(false)
          }} disabled={mbSaving}
            className="px-3 py-1.5 rounded-lg text-sm bg-green-600 text-white hover:bg-green-500 disabled:opacity-50 cursor-pointer">
            {mbSaving ? t("admin.saving") : t("admin.save")}
          </button>
          {mbError && <span className="text-xs text-red-400">{mbError}</span>}
          </div>
          )}
        </div>
      </Card>

      <Card className="space-y-3">
        <h3 className="font-medium flex items-center gap-2"><Users className="w-4 h-4" /> {t("admin.users")}</h3>
        <div className="space-y-1">
          {users.map(u => (
            <div key={u.id} className="flex items-center justify-between p-3 rounded-lg bg-zinc-800/50 text-sm">
              <div className="flex-1 min-w-0">
                <span className="truncate block">{u.username}</span>
                <span className="text-zinc-500 text-xs">{u.email}</span>
              </div>
              <span className="text-zinc-400 w-20 text-center">{roleLabels[u.role] ?? t("admin.roleUser")}</span>
              <div className="flex gap-2 w-36 justify-end">
                {currentRole === "super_admin" && u.role !== "super_admin" && (
                  <>
                    {u.role !== "admin" && (
                      <button onClick={() => updateRole(u.id, "admin")}
                        className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer">
                        {t("admin.promote")}
                      </button>
                    )}
                    {u.role !== "user" && (
                      <button onClick={() => updateRole(u.id, "user")}
                        className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer">
                        {t("admin.demote")}
                      </button>
                    )}
                  </>
                )}
                {currentRole === "admin" && u.role === "user" && (
                  <button onClick={() => updateRole(u.id, "admin")}
                    className="text-xs px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700 cursor-pointer">
                    {t("admin.promote")}
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
