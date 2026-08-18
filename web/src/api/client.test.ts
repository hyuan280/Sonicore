import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import { api } from "./client";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const fetchMock = vi.fn();

describe("api client", () => {
  beforeEach(() => {
    localStorage.clear();
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("sends JSON content-type without auth header when logged out", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ ok: true }));
    await api.auth.login({ username: "u", password: "p" });
    const [, opts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(opts.headers).toEqual({ "Content-Type": "application/json" });
  });

  it("adds bearer token when present", async () => {
    localStorage.setItem("token", "tok123");
    fetchMock.mockResolvedValue(jsonResponse({ ok: true }));
    await api.auth.me();
    const [url, opts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/user/me");
    expect((opts.headers as Record<string, string>).Authorization).toBe("Bearer tok123");
  });

  it("sends content-type for requests without a body", async () => {
    localStorage.setItem("token", "t");
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    await api.auth.logout();
    const [, opts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(opts.headers).toEqual({ Authorization: "Bearer t", "Content-Type": "application/json" });
  });

  it("returns parsed JSON on success", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ items: [1, 2] }));
    await expect(api.user.playlists()).resolves.toEqual({ items: [1, 2] });
  });

  it("returns null on 204", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    await expect(api.auth.logout()).resolves.toBeNull();
  });

  it("throws error with status and code from body", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ error: "bad thing", code: 4042 }, 404));
    const err: any = await api.auth.me().catch((e) => e);
    expect(err.message).toBe("bad thing");
    expect(err.status).toBe(404);
    expect(err.code).toBe(4042);
  });

  it("throws statusText when body has no error", async () => {
    fetchMock.mockResolvedValue(
      new Response(null, { status: 500, statusText: "Internal Server Error" }),
    );
    const err: any = await api.auth.me().catch((e) => e);
    expect(err.message).toBe("Internal Server Error");
  });

  it("retries once after a successful token refresh on 401", async () => {
    localStorage.setItem("token", "old");
    localStorage.setItem("refresh_token", "rt");
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ error: "expired" }, 401))
      .mockResolvedValueOnce(jsonResponse({ token: "new", refresh_token: "newrt" }))
      .mockResolvedValueOnce(jsonResponse({ id: "u1" }));

    const res = await api.auth.me();
    expect(res).toEqual({ id: "u1" });
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(localStorage.getItem("token")).toBe("new");
    expect(localStorage.getItem("refresh_token")).toBe("newrt");
  });

  it("clears storage and redirects to login when refresh fails", async () => {
    localStorage.setItem("token", "old");
    localStorage.setItem("refresh_token", "rt");
    fetchMock.mockResolvedValue(jsonResponse({ error: "expired" }, 401));

    const hrefSetter = vi.fn();
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { href: "" },
      writable: true,
    });
    vi.spyOn(window.location, "href", "set").mockImplementation(hrefSetter);

    const err: any = await api.auth.me().catch((e) => e);
    expect(err.message).toBe("session expired");
    expect(localStorage.getItem("token")).toBeNull();
    expect(hrefSetter).toHaveBeenCalledWith("/login");
  });

  it("does not refresh on 401 from login endpoint", async () => {
    localStorage.setItem("refresh_token", "rt");
    fetchMock.mockResolvedValue(jsonResponse({ error: "bad creds" }, 401));
    const err: any = await api.auth.login({ username: "u", password: "w" }).catch((e) => e);
    expect(err.status).toBe(401);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
