import { beforeEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, getCsrfToken, setCsrfToken } from "./client";

function mockFetch(status: number, body: unknown) {
  return vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
}

describe("api client", () => {
  beforeEach(() => setCsrfToken(null));

  it("stores the CSRF token from responses and sends it on POST", async () => {
    mockFetch(200, { totp_required: true, csrf_token: "tok123" });
    await api("POST", "/auth/login", { username: "a", password: "b" });
    expect(getCsrfToken()).toBe("tok123");

    const spy = mockFetch(204, undefined);
    await api("POST", "/auth/logout");
    const headers = spy.mock.lastCall![1]!.headers as Record<string, string>;
    expect(headers["X-CSRF-Token"]).toBe("tok123");
  });

  it("does not send the CSRF token on GET", async () => {
    setCsrfToken("tok");
    const spy = mockFetch(200, {});
    await api("GET", "/dashboard");
    const headers = spy.mock.calls[0]![1]!.headers as Record<string, string>;
    expect(headers["X-CSRF-Token"]).toBeUndefined();
  });

  it("turns error bodies into ApiError", async () => {
    mockFetch(401, { error: { code: "totp_required", message: "two-factor code required" } });
    const err = await api("GET", "/auth/me").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 401, code: "totp_required", message: "two-factor code required" });
  });

  it("reports network failures", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("Failed to fetch"));
    await expect(api("GET", "/auth/me")).rejects.toMatchObject({ code: "network" });
  });
});
