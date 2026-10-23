import { useState, type FormEvent } from "react"
import { useNavigate } from "react-router-dom"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useAuth } from "@/hooks/useAuth"
import { useTunnelCapabilities } from "@/hooks/useTunnelCapabilities"
import { t } from "@/i18n"

export function LoginPage() {
  const [value, setValue] = useState("")
  const { setToken } = useAuth()
  const navigate = useNavigate()
  // The capabilities answer this machine's operator without a token, repository included.
  const repo = useTunnelCapabilities().data?.repo?.slug

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault()
    const trimmed = value.trim()
    if (!trimmed) return
    setToken(trimmed)
    navigate("/", { replace: true })
  }

  return (
    <div className="flex min-h-screen flex-col items-center bg-background px-4 pb-10 pt-[120px] text-[13px]">
      <form onSubmit={handleSubmit} className="flex w-full max-w-[360px] flex-col gap-5">
        <div className="flex flex-col items-center gap-3.5 text-center">
          <img src="/favicon.svg" alt="" className="h-10 w-10" />
          <h1 className="text-xl font-semibold tracking-[-0.01em]">{t("login.title")}</h1>
          <p className="leading-5 text-muted-foreground">{t("login.paste")}</p>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="token" className="text-xs text-foreground/80">
            {t("login.token")}
          </Label>
          <Input
            id="token"
            type="password"
            autoComplete="off"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder={t("login.placeholder")}
            className="h-10 rounded-lg font-mono text-[13px]"
            autoFocus
          />
        </div>
        <Button type="submit" className="h-11 w-full rounded-lg bg-brand text-white hover:bg-brand/90" disabled={!value.trim()}>
          {t("login.enter")}
        </Button>
        <p className="text-center text-xs leading-[18px] text-muted-foreground">{t("login.link_hint")}</p>
      </form>
      <p className="mt-auto pt-10 text-xs text-muted-foreground">{repo ? `${repo} · ${window.location.origin}` : window.location.origin}</p>
    </div>
  )
}
