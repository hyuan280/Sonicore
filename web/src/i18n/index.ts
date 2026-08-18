import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";

const LOCALE_LOADERS: Record<string, () => Promise<{ default: Record<string, unknown> }>> = {
  en: () => import("./locales/en/translation.json"),
  zh: () => import("./locales/zh/translation.json"),
};

const initPromise = i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    fallbackLng: "en",
    supportedLngs: ["en", "zh"],
    interpolation: { escapeValue: false },
    detection: {
      order: ["localStorage", "navigator"],
      caches: [],
    },
  });

i18n.on("languageChanged", (lng: string) => {
  try {
    localStorage.setItem("i18nextLng", lng);
  } catch {
    /* ignore storage errors (private mode, quota) */
  }
});

const bundlePromises: Record<string, Promise<boolean>> = {};

async function loadBundle(lng: string): Promise<boolean> {
  const loader = LOCALE_LOADERS[lng];
  if (!loader) return false;
  if (!bundlePromises[lng]) {
    bundlePromises[lng] = loader()
      .then((mod) => {
        i18n.addResourceBundle(lng, "translation", mod.default);
        return true;
      })
      .catch((err) => {
        console.error("Failed to load locale bundle", lng, err);
        delete bundlePromises[lng];
        return false;
      });
  }
  return bundlePromises[lng];
}

function resolveLang(raw: string): string {
  const base = raw.split("-")[0];
  return LOCALE_LOADERS[base] ? base : "en";
}

async function loadLocales(): Promise<void> {
  await initPromise;
  const detected = i18n.language || "en";
  const primaryLang = resolveLang(detected);

  if (primaryLang === "en") {
    await loadBundle("en");
  } else {
    const [primaryOk] = await Promise.all([loadBundle(primaryLang), loadBundle("en")]);
    if (!primaryOk) {
      console.warn("Primary locale failed, falling back to en");
      await i18n.changeLanguage("en");
    }
  }
}

let lastRequested: string | null = null;

export async function switchLanguage(lng: string): Promise<void> {
  lastRequested = lng;
  const resolved = resolveLang(lng);
  if (!i18n.hasResourceBundle(resolved, "translation")) {
    const ok = await loadBundle(resolved);
    if (!ok) return;
  }
  if (lastRequested !== lng) return;
  if (i18n.language !== resolved) {
    await i18n.changeLanguage(resolved);
  }
}

export const i18nReady = loadLocales();

export default i18n;
