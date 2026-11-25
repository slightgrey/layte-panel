import { X } from "lucide-react";
import { type ReactNode, useEffect, useRef } from "react";
import { Button } from "./button";

/** A small modal dialog (Escape or the X closes it unless `locked`). */
export function Dialog({
  open,
  onClose,
  title,
  children,
  locked = false,
  wide = false,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  locked?: boolean;
  wide?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  // Callers pass a new onClose on every render; read the latest from a ref,
  // so the effects below don't re-run (and steal focus) while typing.
  const close = useRef({ onClose, locked });
  close.current = { onClose, locked };
  useEffect(() => {
    if (!open) return;
    // Skip keys a child already handled (e.g. Escape closing the editor's search box).
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && !e.defaultPrevented && !close.current.locked && close.current.onClose();
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);
  // Focus the first field once, when the dialog opens.
  useEffect(() => {
    if (open) ref.current?.querySelector<HTMLElement>("input, button")?.focus();
  }, [open]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/30 p-4 backdrop-blur-[2px]" onMouseDown={() => !locked && onClose()}>
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        // Never taller than the window: the title stays put and the content scrolls.
        className={`flex max-h-[calc(100dvh-2rem)] w-full flex-col ${wide ? "max-w-5xl" : "max-w-lg"} rounded-[1.25rem] bg-card shadow-raised`}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-4 px-6 pt-6 pb-4">
          <h2 className="text-base font-semibold">{title}</h2>
          {!locked && (
            <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close" className="-mt-1 -mr-2">
              <X />
            </Button>
          )}
        </div>
        <div className="min-h-0 overflow-y-auto px-6 pb-6">{children}</div>
      </div>
    </div>
  );
}
