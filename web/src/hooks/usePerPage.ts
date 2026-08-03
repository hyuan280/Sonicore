import { useState, useCallback } from "react"

export function usePerPage(key: string, defaultVal: number = 20): [number, (val: number) => void] {
  const [perPage, setPerPage] = useState<number>(() => {
    try {
      const stored = localStorage.getItem(`sonicore_perpage_${key}`)
      if (stored) {
        const n = parseInt(stored, 10)
        if ([10, 20, 50].includes(n)) return n
      }
    } catch {}
    return defaultVal
  })

  const set = useCallback((val: number) => {
    setPerPage(val)
    try { localStorage.setItem(`sonicore_perpage_${key}`, String(val)) } catch {}
  }, [key])

  return [perPage, set]
}
