import { forwardRef, type KeyboardEvent, type ReactNode } from "react";

function classNames(...values: Array<string | false | undefined>): string {
  return values.filter(Boolean).join(" ");
}

export const BrowsingListRowFrame = forwardRef<
  HTMLLIElement,
  {
    active: boolean;
    after?: ReactNode;
    children: ReactNode;
    hideDivider?: boolean;
    rowClassName?: string;
    surfaceClassName: string;
    surfaceActiveClassName?: string;
    surfaceAriaLabel?: string;
    surfaceRole?: string;
    surfaceTabIndex?: number;
    onPointerEnter?(): void;
    onPointerLeave?(): void;
    onSurfaceClick?(): void;
    onSurfaceKeyDown?(event: KeyboardEvent<HTMLDivElement>): void;
  }
>(function BrowsingListRowFrame(
  {
    active,
    after,
    children,
    hideDivider = false,
    rowClassName,
    surfaceClassName,
    surfaceActiveClassName = "bg-[#2a2a28]",
    surfaceAriaLabel,
    surfaceRole,
    surfaceTabIndex,
    onPointerEnter,
    onPointerLeave,
    onSurfaceClick,
    onSurfaceKeyDown,
  },
  ref,
) {
  return (
    <li
      ref={ref}
      className={classNames(
        "relative border-b",
        active || hideDivider ? "border-transparent" : "border-[#343432]",
        rowClassName,
      )}
      onPointerEnter={onPointerEnter}
      onPointerLeave={onPointerLeave}
    >
      <div
        aria-label={surfaceAriaLabel}
        className={classNames(surfaceClassName, active && surfaceActiveClassName)}
        onClick={onSurfaceClick}
        onKeyDown={onSurfaceKeyDown}
        role={surfaceRole}
        tabIndex={surfaceTabIndex}
      >
        {children}
      </div>
      {after}
    </li>
  );
});
