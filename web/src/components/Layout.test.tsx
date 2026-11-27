import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { describe, expect, it, vi } from "vitest";
import { meKey } from "@/api/auth";
import type { MeResponse } from "@/api/types";
import { Layout } from "./Layout";
import { RequireAdmin } from "./RequireAuth";

function renderAs(me: MeResponse, path = "/sites") {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } }));
  const qc = new QueryClient();
  qc.setQueryData(meKey, me);
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route element={<Layout />}>
            <Route path="sites" element={<p>sites page</p>} />
            <Route element={<RequireAdmin />}>
              <Route path="security" element={<p>security page</p>} />
            </Route>
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const navLabels = () => within(screen.getByRole("navigation", { name: "Main" })).getAllByRole("link").map((a) => a.textContent);

describe("Layout", () => {
  it("shows every section to admins", () => {
    renderAs({ user: { id: 1, username: "admin", role: "admin" }, csrf_token: "t" });
    expect(navLabels()).toEqual(expect.arrayContaining(["Dashboard", "Sites", "Security", "Updates", "Users", "API keys"]));
    expect(screen.getByRole("link", { name: "Account settings" })).toHaveAttribute("href", "/account");
  });

  it("shows only Sites to a site account, and keeps it off admin pages", async () => {
    renderAs({ user: { id: 2, username: "client", role: "site", site_id: 5, max_subdomains: 3 }, csrf_token: "t" }, "/security");
    expect(navLabels()).toEqual(["Sites"]);
    expect(await screen.findByText("sites page")).toBeInTheDocument();
    expect(screen.queryByText("security page")).not.toBeInTheDocument();
  });
});
