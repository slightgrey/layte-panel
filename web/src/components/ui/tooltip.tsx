import { type ReactNode, useId } from "react";
import { cn } from "@/lib/utils";

/**
 * A small tooltip: shown while the trigger is hovered or has keyboard focus.
 * The trigger is focusable and points to the text with aria-describedby.
 */
export function Tooltip({ content, children, className }: { content: ReactNode; children: ReactNode; className?: string }) {
  const id = useId();
  return (
    <span className={cn("group relative inline-flex", className)}>
      <span tabIndex={0} aria-describedby={id} className="inline-flex cursor-help rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring">
        {children}
      </span>
      <span
        id={id}
        role="tooltip"
        className="pointer-events-none invisible absolute top-full left-0 z-50 mt-2 w-max max-w-[min(22rem,80vw)] rounded-[10px] bg-card px-3 py-2 text-xs leading-relaxed font-normal text-foreground opacity-0 shadow-raised transition-opacity group-focus-within:visible group-focus-within:opacity-100 group-hover:visible group-hover:opacity-100"
      >
        {content}
      </span>
    </span>
  );
}
