import { describe, expect, it, beforeEach, vi } from "vitest"

vi.mock("./locales/en/translation.json", () => ({ default: { app: { name: "Sonicore" } } }))
vi.mock("./locales/zh/translation.json", () => ({ default: { app: { name: "声核" } } }))

import i18n, { i18nReady, switchLanguage } from "./index"

describe("i18n", () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18nReady
    await i18n.changeLanguage("en")
  })

  it("loads initial bundle and resolves ready", async () => {
    expect(i18n.t("app.name")).toBe("Sonicore")
  })

  it("switchLanguage switches to zh", async () => {
    await switchLanguage("zh")
    expect(i18n.language).toBe("zh")
    expect(i18n.t("app.name")).toBe("声核")
  })

  it("switchLanguage normalizes zh-CN to zh", async () => {
    await switchLanguage("zh-CN")
    expect(i18n.language).toBe("zh")
  })

  it("switchLanguage falls back to en for unknown language", async () => {
    await switchLanguage("xx")
    expect(i18n.language).toBe("en")
  })

  it("persists language choice to localStorage", async () => {
    await switchLanguage("zh")
    expect(localStorage.getItem("i18nextLng")).toBe("zh")
  })

  it("switchLanguage is idempotent for same language", async () => {
    await switchLanguage("en")
    expect(i18n.language).toBe("en")
  })
})
