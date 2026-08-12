import { describe, expect, it } from "vitest"
import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import {
  ERROR_CODES,
  getErrorI18nKey,
  getErrorName,
  translateApiError,
} from "./errorCodes"

const GO_SOURCE = readFileSync(
  resolve(process.cwd(), "../internal/core/domain/error_codes.go"),
  "utf8",
)

function parseGoErrorCodes(): Map<number, string> {
  // const 定义区：Err<Name> ErrorCode = <N>  → suffix → code
  const suffixToCode = new Map<string, number>()
  const constRe = /Err(\w+)\s+ErrorCode\s*=\s*(\d+)/g
  let m: RegExpExecArray | null
  while ((m = constRe.exec(GO_SOURCE)) !== null) {
    suffixToCode.set(m[1], parseInt(m[2], 10))
  }

  // 限定在 errorCodeKeys map 区域内提取条目：Err<Name>: "<KEY>" → suffix → key
  const mapStart = GO_SOURCE.indexOf("var errorCodeKeys")
  const mapEnd = GO_SOURCE.indexOf("}", mapStart)
  const keysSection = GO_SOURCE.slice(mapStart, mapEnd)
  const result = new Map<number, string>()
  const entryRe = /Err(\w+)\s*:\s*"([A-Z0-9_]+)"/g
  while ((m = entryRe.exec(keysSection)) !== null) {
    const code = suffixToCode.get(m[1])
    if (code !== undefined) result.set(code, m[2])
  }
  return result
}

describe("errorCodes 前后端一致性", () => {
  const goCodes = parseGoErrorCodes()

  it("Go 源文件能解析出错误码和 key", () => {
    expect(goCodes.size).toBeGreaterThan(50)
    expect(goCodes.get(1)).toBe("UNAUTHORIZED")
    expect(goCodes.get(703)).toBe("PLATFORM_UPSTREAM_ERROR")
  })

  it("双向一致：Go 每个 code 在 TS 中存在且 key 相同", () => {
    for (const [code, goKey] of goCodes) {
      const tsKey = Object.entries(ERROR_CODES).find(([, c]) => c === code)?.[0]
      expect(tsKey, `code ${code}: TS 缺少或 key 不匹配`).toBe(goKey)
    }
  })

  it("双向一致：TS 每个 key 在 Go 中存在且 code 相同", () => {
    for (const [tsKey, tsCode] of Object.entries(ERROR_CODES)) {
      expect(goCodes.get(tsCode), `TS key ${tsKey}: Go 缺少或 code 不匹配`).toBe(tsKey)
    }
  })

  it("两边条目数量一致", () => {
    expect(Object.keys(ERROR_CODES).length).toBe(goCodes.size)
  })

  it("每个 code 落在正确的 category 区间", () => {
    const categories: [number, number, string][] = [
      [1, 99, "common"],
      [100, 199, "auth"],
      [200, 299, "library"],
      [300, 399, "user"],
      [400, 499, "metadata"],
      [500, 599, "jukebox"],
      [600, 699, "stream"],
      [700, 799, "platform"],
    ]
    for (const [code] of goCodes) {
      const cat = categories.find(([lo, hi]) => code >= lo && code <= hi)
      expect(cat, `code ${code} 不在任何已声明区间`).toBeDefined()
    }
  })
})

describe("getErrorI18nKey / getErrorName", () => {
  it("返回已知 code 的 i18n key 和名称", () => {
    expect(getErrorI18nKey(1)).toBe("errors.common.UNAUTHORIZED")
    expect(getErrorName(101)).toBe("INVALID_CREDENTIALS")
  })

  it("未知 code 返回 undefined", () => {
    expect(getErrorI18nKey(9999)).toBeUndefined()
    expect(getErrorName(9999)).toBeUndefined()
  })
})

describe("translateApiError", () => {
  // 模拟真实 i18next：已知 key 返回 key 本身，未知 key 回退到 defaultValue
  const KNOWN_KEYS = new Set(["errors.auth.INVALID_CREDENTIALS", "errors.common.UNAUTHORIZED"])
  const t = ((key: string, opts?: { defaultValue?: string }): string => {
    return KNOWN_KEYS.has(key) ? key : (opts?.defaultValue ?? key)
  }) as unknown as Parameters<typeof translateApiError>[0]

  it("code 已知时走 i18n 翻译", () => {
    expect(translateApiError(t, { code: 101, error: "Invalid username or password" }))
      .toBe("errors.auth.INVALID_CREDENTIALS")
  })

  it("code 未知时回退到 error 字段", () => {
    expect(translateApiError(t, { code: 9999, error: "custom message" })).toBe("custom message")
  })

  it("无 code 时回退到 error 字段", () => {
    expect(translateApiError(t, { error: "plain error" })).toBe("plain error")
  })

  it("无 error 时回退到 message 字段", () => {
    expect(translateApiError(t, { message: "fallback message" })).toBe("fallback message")
  })

  it("全部缺失时回退到 common.unknown", () => {
    expect(translateApiError(t, {})).toBe("common.unknown")
  })

  it("非对象入参回退到 common.unknown", () => {
    expect(translateApiError(t, null)).toBe("common.unknown")
    expect(translateApiError(t, "boom")).toBe("common.unknown")
  })
})
