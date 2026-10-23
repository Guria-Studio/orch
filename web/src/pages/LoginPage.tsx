import { useState, type FormEvent } from "react"
import { useNavigate } from "react-router-dom"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useAuth } from "@/hooks/useAuth"
import { t } from "@/i18n"

export function LoginPage() {
  const [value, setValue] = useState("")
  const { setToken } = useAuth()
  const navigate = useNavigate()

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault()
    const trimmed = value.trim()
    if (!trimmed) return
    setToken(trimmed)
    navigate("/", { replace: true })
  }

  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-6 bg-background px-4 text-[13px]">
      <form onSubmit={handleSubmit} className="w-full max-w-sm space-y-4 rounded-xl border bg-card p-6 shadow-sm">
        <div className="space-y-2">
          <img src="/favicon.svg" alt="" className="h-7 w-7" />
          <h1 className="text-lg font-semibold tracking-[-0.01em]">{t("login.title")}</h1>
          <p className="text-muted-foreground">{t("login.paste")}</p>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="token" className="text-xs text-muted-foreground">
            {t("login.token")}
          </Label>
          <Input
            id="token"
            type="password"
            autoComplete="off"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder={t("login.placeholder")}
            autoFocus
          />
        </div>
        <Button type="submit" className="w-full" disabled={!value.trim()}>
          {t("login.enter")}
        </Button>
        <p className="text-xs text-muted-foreground">{t("login.link_hint")}</p>
      </form>
      <p className="font-mono text-xs text-muted-foreground">{window.location.origin}</p>
    </div>
  )
}
