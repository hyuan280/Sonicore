import { useState, useEffect } from "react"
import { useNavigate } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { translateApiError } from "../i18n/errorCodes"
import { useAuth } from "../stores/auth"
import { Button } from "../components/ui/button"
import { Input } from "../components/ui/input"
import Logo from "../components/Logo"
import LanguageSwitcher from "../components/LanguageSwitcher"

export default function LoginPage() {
  const [tab, setTab] = useState<"login" | "register">("login")
  const [username, setUsername] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState("")
  const { login, register, allowRegistration, loadRegistrationStatus } = useAuth()
  const navigate = useNavigate()
  const { t } = useTranslation()

  useEffect(() => { loadRegistrationStatus() }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault(); setError("")
    try {
      if (tab === "login") await login(username, password)
      else await register(username, email, password)
      navigate("/libraries")
    } catch (err: unknown) { setError(translateApiError(t, err)) }
  }

  const tabs = allowRegistration ? (["login", "register"] as const) : (["login"] as const)

  return (
    <div className="min-h-screen flex items-center justify-center bg-black">
      <div className="fixed top-4 right-4 z-50">
        <LanguageSwitcher />
      </div>
      <div className="w-full max-w-sm space-y-6 px-4">
        <div className="text-center space-y-3">
          <Logo className="w-12 h-12 mx-auto" />
          <h1 className="text-3xl font-bold">Sonicore</h1>
          <p className="text-zinc-400 text-sm">{t("auth.tagline")}</p>
        </div>

        {tabs.length > 1 && (
          <div className="flex rounded-lg bg-zinc-900 p-1">
            {tabs.map(tabOption => (
              <button key={tabOption} onClick={() => setTab(tabOption)}
                className={`flex-1 py-2 text-sm rounded-md transition-colors cursor-pointer ${tab === tabOption ? "bg-green-600 text-white" : "text-zinc-400 hover:text-white"}`}>
                {t(tabOption === "login" ? "auth.signIn" : "auth.register")}
              </button>
            ))}
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-4">
          <Input placeholder={t("auth.username")} value={username} onChange={e => setUsername(e.target.value)} required />
          {tab === "register" && <Input type="email" placeholder={t("auth.email")} value={email} onChange={e => setEmail(e.target.value)} required />}
          <Input type="password" placeholder={t("auth.password")} value={password} onChange={e => setPassword(e.target.value)} required />
          {error && <p className="text-red-400 text-sm">{error}</p>}
          <Button type="submit" className="w-full">{t(tab === "login" ? "auth.signIn" : "auth.createAccount")}</Button>
        </form>
      </div>
    </div>
  )
}
