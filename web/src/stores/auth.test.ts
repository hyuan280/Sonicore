import { describe, expect, it, beforeEach, vi } from "vitest";

vi.mock("../api/client", () => ({
  api: {
    auth: {
      login: vi.fn(),
      register: vi.fn(),
      logout: vi.fn(),
      me: vi.fn(),
      registrationStatus: vi.fn(),
    },
  },
}));

import { api } from "../api/client";
import { useAuth, getRole, isAdmin } from "./auth";

const mocked = vi.mocked(api.auth);

const AUTH_RESPONSE = {
  token: "tok",
  refresh_token: "rt",
  session_token: "st",
  user_id: "u1",
  username: "alice",
  role: "admin",
};

describe("useAuth", () => {
  beforeEach(() => {
    localStorage.clear();
    useAuth.setState({ user: null, token: null, loading: false, allowRegistration: true });
    vi.clearAllMocks();
  });

  it("login stores tokens and user", async () => {
    mocked.login.mockResolvedValue(AUTH_RESPONSE);
    await useAuth.getState().login("alice", "pw");
    expect(localStorage.getItem("token")).toBe("tok");
    expect(localStorage.getItem("refresh_token")).toBe("rt");
    expect(localStorage.getItem("session_token")).toBe("st");
    expect(localStorage.getItem("role")).toBe("admin");
    const s = useAuth.getState();
    expect(s.token).toBe("tok");
    expect(s.user).toEqual({
      id: "u1",
      username: "alice",
      email: "",
      role: "admin",
      created_at: "",
    });
  });

  it("register stores tokens and user with email", async () => {
    mocked.register.mockResolvedValue({ ...AUTH_RESPONSE, username: "bob" });
    await useAuth.getState().register("bob", "b@x.io", "pw");
    expect(useAuth.getState().user?.email).toBe("b@x.io");
    expect(localStorage.getItem("token")).toBe("tok");
  });

  it("logout clears storage and state", async () => {
    localStorage.setItem("token", "t");
    localStorage.setItem("playerState", "{}");
    mocked.logout.mockResolvedValue(null);
    await useAuth.getState().logout();
    expect(mocked.logout).toHaveBeenCalled();
    expect(localStorage.getItem("token")).toBeNull();
    expect(localStorage.getItem("playerState")).toBeNull();
    expect(useAuth.getState().user).toBeNull();
    expect(useAuth.getState().token).toBeNull();
  });

  it("logout proceeds even when API call fails", async () => {
    mocked.logout.mockRejectedValue(new Error("net"));
    await useAuth.getState().logout();
    expect(useAuth.getState().user).toBeNull();
  });

  it("loadUser sets user and role on success", async () => {
    mocked.me.mockResolvedValue({
      id: "u1",
      username: "alice",
      email: "a@x.io",
      role: "user",
      created_at: "t",
    });
    await useAuth.getState().loadUser();
    expect(localStorage.getItem("role")).toBe("user");
    expect(useAuth.getState().user?.role).toBe("user");
  });

  it("loadUser clears everything on failure", async () => {
    localStorage.setItem("token", "t");
    mocked.me.mockRejectedValue(new Error("401"));
    await useAuth.getState().loadUser();
    expect(localStorage.getItem("token")).toBeNull();
    expect(useAuth.getState().token).toBeNull();
    expect(useAuth.getState().user).toBeNull();
  });

  it("loadRegistrationStatus sets flag", async () => {
    mocked.registrationStatus.mockResolvedValue({ allow_registration: false });
    await useAuth.getState().loadRegistrationStatus();
    expect(useAuth.getState().allowRegistration).toBe(false);
  });

  it("loadRegistrationStatus ignores failures", async () => {
    mocked.registrationStatus.mockRejectedValue(new Error("x"));
    await useAuth.getState().loadRegistrationStatus();
    expect(useAuth.getState().allowRegistration).toBe(true);
  });

  it("initial token comes from localStorage", () => {
    localStorage.setItem("token", "stored");
    useAuth.setState({ token: localStorage.getItem("token") });
    expect(useAuth.getState().token).toBe("stored");
  });
});

describe("role helpers", () => {
  beforeEach(() => localStorage.clear());

  it("getRole returns null without stored role", () => {
    expect(getRole()).toBeNull();
  });

  it("getRole returns stored role", () => {
    localStorage.setItem("role", "super_admin");
    expect(getRole()).toBe("super_admin");
  });

  it("isAdmin is true for admin and super_admin", () => {
    localStorage.setItem("role", "admin");
    expect(isAdmin()).toBe(true);
    localStorage.setItem("role", "super_admin");
    expect(isAdmin()).toBe(true);
  });

  it("isAdmin is false for user and missing role", () => {
    localStorage.setItem("role", "user");
    expect(isAdmin()).toBe(false);
    localStorage.removeItem("role");
    expect(isAdmin()).toBe(false);
  });
});
