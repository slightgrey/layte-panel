import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { JobProgress } from "./JobProgress";

// A controllable stand-in for the browser's EventSource.
class FakeEventSource {
  static last: FakeEventSource;
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {};
  onerror: (() => void) | null = null;
  closed = false;
  constructor(public url: string) {
    FakeEventSource.last = this;
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    (this.listeners[type] ??= []).push(fn);
  }
  close() {
    this.closed = true;
  }
  emit(type: string, data: object) {
    for (const fn of this.listeners[type] ?? []) fn(new MessageEvent(type, { data: JSON.stringify(data) }));
  }
}

describe("JobProgress", () => {
  it("shows steps from the SSE stream and reports completion", async () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ error: { code: "secret_gone", message: "gone" } }), { status: 404 }));
    const onDone = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <JobProgress jobId="j1" onDone={onDone} />
      </QueryClientProvider>,
    );
    const es = FakeEventSource.last;
    expect(es.url).toBe("/api/v1/jobs/j1/events");

    act(() => {
      es.emit("step", { seq: 1, type: "step", step: "Create Linux user acme", status: "running" });
      es.emit("step", { seq: 2, type: "step", step: "Create Linux user acme", status: "done" });
      es.emit("step", { seq: 3, type: "step", step: "Configure nginx", status: "running" });
      es.emit("log", { seq: 4, type: "log", message: "nginx -t ok" });
    });
    expect(screen.getByText("Create Linux user acme")).toBeInTheDocument();
    expect(screen.getByText("nginx -t ok")).toBeInTheDocument();
    expect(onDone).not.toHaveBeenCalled();

    act(() => {
      es.emit("step", { seq: 5, type: "step", step: "Configure nginx", status: "failed" });
      es.emit("done", { seq: 6, type: "done", status: "failed", message: "nginx -t failed (change rolled back)" });
    });
    expect(screen.getByRole("alert")).toHaveTextContent("rolled back");
    expect(onDone).toHaveBeenCalledWith(false);
    expect(es.closed).toBe(true);
    vi.unstubAllGlobals();
  });
});
