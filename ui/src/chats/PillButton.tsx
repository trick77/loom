import type { ReactNode } from "react";

export function PillButton({
  children,
  variant,
  enabled = true,
  onClick,
  title,
}: {
  children: ReactNode;
  variant: "solid" | "white" | "muted";
  enabled?: boolean;
  onClick?(): void;
  title?: string;
}) {
  let className = "ui-control-text rounded-lg px-3 py-1.5 font-medium transition-colors ";
  if (variant === "solid") {
    className += "bg-[#343433] text-[#f5f3ee] hover:bg-[#3d3d3b]";
  } else if (variant === "white") {
    className += "bg-white text-[#1d1d1c] hover:bg-[#ece9e2]";
  } else {
    className += `bg-[#282827] ${enabled ? "text-[#faf9f5]" : "text-[#8c8a82]"}`;
  }
  return (
    <button
      type="button"
      className={className}
      onClick={onClick}
      disabled={!enabled}
      aria-disabled={!enabled}
      title={title}
    >
      {children}
    </button>
  );
}
