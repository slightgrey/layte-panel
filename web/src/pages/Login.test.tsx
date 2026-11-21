import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router";
import { describe, expect, it, vi } from "vitest";
import { LoginPage } from "./Login";

function renderLogin() {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/login"]}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/login/2fa" element={<p>2fa step</p>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("LoginPage", () => {
  it("goes to the 2FA step after a correct password", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(json(200, { totp_required: true, csrf_token: "t" }));
    renderLogin();

    await userEvent.type(screen.getByLabelText("Username"), "admin");
    await userEvent.type(screen.getByLabelText("Password"), "correct horse battery staple");
    await userEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(await screen.findByText("2fa step")).toBeInTheDocument();
    const [url, init] = fetchSpy.mock.calls[0]!;
    expect(url).toBe("/api/v1/auth/login");
    expect(JSON.parse(init!.body as string)).toEqual({ username: "admin", password: "correct horse battery staple" });
  });

  it("shows the server's error message", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      json(401, { error: { code: "invalid_credentials", message: "invalid username or password" } }),
    );
    renderLogin();

    await userEvent.type(screen.getByLabelText("Username"), "admin");
    await userEvent.type(screen.getByLabelText("Password"), "wrong");
    await userEvent.click(screen.getByRole("button", { name: "Continue" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("invalid username or password");
  });
});
