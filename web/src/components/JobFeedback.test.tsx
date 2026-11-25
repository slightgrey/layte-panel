import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { JobButtonLabel, JobFeedbackProvider, useJobFeedback } from "./JobFeedback";

function StartButton() {
  const job = useJobFeedback("object-cache-1", "Turning on the object cache");
  return (
    <button type="button" onClick={() => job.start({ job_id: "j1" })}>
      <JobButtonLabel state={job.state}>Turn on</JobButtonLabel>
    </button>
  );
}

function setup() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <JobFeedbackProvider>
          <StartButton />
        </JobFeedbackProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const jobResponse = (status: string, error?: string) =>
  new Response(JSON.stringify({ id: "j1", kind: "site.object_cache", status, error, events: [], has_secret: false, created_at: "" }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });

describe("JobFeedback", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("shows Working… on the button and a flash message, then Done", async () => {
    vi.useFakeTimers();
    const fetch = vi.spyOn(globalThis, "fetch").mockResolvedValue(jobResponse("running"));
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
    expect(screen.getByRole("button", { name: /Working/ })).toBeTruthy();
    expect(screen.getByText("Turning on the object cache…")).toBeTruthy();

    fetch.mockResolvedValue(jobResponse("succeeded"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });
    expect(screen.getByRole("button", { name: /Done/ })).toBeTruthy();
    expect(screen.getByText("Turning on the object cache: done")).toBeTruthy();

    // The button goes back to normal, and the message disappears.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(8000);
    });
    expect(screen.getByRole("button", { name: "Turn on" })).toBeTruthy();
    expect(screen.queryByText(/object cache: done/)).toBeNull();
  });

  it("keeps a failure message with the reason until it's closed", async () => {
    vi.useFakeTimers();
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jobResponse("failed", "WordPress didn't store anything in Redis"));
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });
    expect(screen.getByRole("button", { name: /Failed/ })).toBeTruthy();
    expect(screen.getByText("WordPress didn't store anything in Redis")).toBeTruthy();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(20000);
    });
    expect(screen.getByText("Turning on the object cache: failed")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByText(/object cache: failed/)).toBeNull();
  });
});
