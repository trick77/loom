import { useCallback, useEffect, useRef, useState } from "react";

export type CopyStatus = "idle" | "copied" | "failed";

// useCopyFeedback writes text to the clipboard and holds the "Copied" feedback
// for resetMs. A new copy restarts the delay, and unmounting clears it.
//
// `key` tells apart several copy targets sharing one hook (a list of links): it
// comes back as copiedKey while that target's feedback is showing.
//
// A failed write returns false and by default changes nothing, so the caller
// reports it its own way; with showFailure the status reads "failed" for the
// same delay.
export function useCopyFeedback<Key = never>(
  resetMs: number,
  { showFailure = false }: { showFailure?: boolean } = {},
): {
  status: CopyStatus;
  copiedKey: Key | null;
  copy(text: string, key?: Key): Promise<boolean>;
} {
  const [feedback, setFeedback] = useState<{
    status: CopyStatus;
    key: Key | null;
  }>(IDLE);
  const resetRef = useRef<number | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (resetRef.current !== null) window.clearTimeout(resetRef.current);
      resetRef.current = null;
    };
  }, []);

  const copy = useCallback(
    async (text: string, key?: Key) => {
      const ok = await writeClipboard(text);
      if (!mountedRef.current || (!ok && !showFailure)) return ok;
      setFeedback({ status: ok ? "copied" : "failed", key: key ?? null });
      if (resetRef.current !== null) window.clearTimeout(resetRef.current);
      resetRef.current = window.setTimeout(() => {
        resetRef.current = null;
        setFeedback(IDLE);
      }, resetMs);
      return ok;
    },
    [resetMs, showFailure],
  );

  return {
    status: feedback.status,
    copiedKey: feedback.status === "copied" ? feedback.key : null,
    copy,
  };
}

const IDLE = { status: "idle", key: null } as const;

// writeClipboard writes to the clipboard and reports whether it worked: the
// clipboard is absent on insecure origins and the write is rejected when the
// document is not focused or permission is denied. A rejected write used to
// escape as an unhandled rejection while the button still said "Copied".
async function writeClipboard(content: string): Promise<boolean> {
  try {
    if (navigator.clipboard === undefined) return false;
    await navigator.clipboard.writeText(content);
    return true;
  } catch {
    return false;
  }
}
