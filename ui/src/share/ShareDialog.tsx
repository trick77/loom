import { useEffect, useState } from "react";

import { createShare, disableShare, updateShare, type ShareInfo } from "../api";
import { Icon, type IconName } from "../chat/Icon";

// ShareDialog is the owner-facing share modal. It copies Claude's flow 1:1, minus
// "Share with your team" and minus "Report": Keep private ⇄ Create public link,
// with an Update affordance when the thread has new messages since the snapshot.
export function ShareDialog({
  threadId,
  share,
  hasNewMessages,
  onShareChange,
  onClose,
}: {
  threadId: string;
  share: ShareInfo | null;
  /** True when the thread has messages newer than the current snapshot. */
  hasNewMessages: boolean;
  onShareChange(next: ShareInfo | null): void;
  onClose(): void;
}) {
  const isShared = share?.shared === true;
  const [choice, setChoice] = useState<"private" | "public">(isShared ? "public" : "private");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    setChoice(share?.shared === true ? "public" : "private");
  }, [share]);

  // Close on Escape, matching the app's other dismissible surfaces.
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  async function run<T>(fn: () => Promise<T>, after?: (result: T) => void) {
    setBusy(true);
    setError(null);
    try {
      const result = await fn();
      after?.(result);
    } catch {
      setError("Something went wrong. Please try again.");
    } finally {
      setBusy(false);
    }
  }

  function selectPrivate() {
    if (busy) return;
    if (isShared) {
      void run(() => disableShare(threadId), () => onShareChange(share ? { ...share, shared: false } : null));
    }
    setChoice("private");
  }

  function selectPublic() {
    if (busy) return;
    setChoice("public");
    if (!isShared) {
      void run(() => createShare(threadId), (info) => onShareChange(info));
    }
  }

  const absoluteUrl =
    share && !share.shareUrl.startsWith("http")
      ? window.location.origin + share.shareUrl
      : share?.shareUrl ?? "";

  async function copyLink() {
    if (!absoluteUrl) return;
    try {
      await navigator.clipboard.writeText(absoluteUrl);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      setError("Couldn’t copy the link.");
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4"
      role="dialog"
      aria-modal="true"
      aria-label={isShared ? "Chat shared" : "Share chat"}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-[528px] rounded-[14px] border border-[#454540] bg-[#363632] p-5 text-[#f4f0e8] shadow-[0_24px_48px_rgba(0,0,0,0.45)]">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h2 className="font-sans text-[22px] font-semibold text-[#f4f0e8]">
              {isShared ? "Chat shared" : "Share chat"}
            </h2>
            <p className="mt-0.5 text-sm text-[#a8a399]">
              {isShared ? (
                hasNewMessages ? (
                  <>
                    New messages since last shared{" "}
                    <button
                      type="button"
                      className="font-medium text-[#5599e7] underline underline-offset-2 transition-colors hover:text-[#6da7ec] disabled:opacity-50"
                      disabled={busy}
                      onClick={() => void run(() => updateShare(threadId), (info) => onShareChange(info))}
                    >
                      Update
                    </button>
                  </>
                ) : (
                  "Future messages aren’t included."
                )
              ) : (
                "Only messages up to this point will be shared."
              )}
            </p>
          </div>
          <button
            type="button"
            aria-label="Close"
            className="shrink-0 leading-none text-[#d5d2c9] transition-colors hover:text-white"
            onClick={onClose}
          >
            <Icon name="close" size="1.25rem" />
          </button>
        </div>

        {/* Options sit inside one rounded frame with dividers, matching the reference. */}
        <div className="mt-4 overflow-hidden rounded-xl border border-[#454540] divide-y divide-[#454540]">
          <ShareOption
            icon="lock"
            title="Keep private"
            subtitle="Only you have access"
            selected={choice === "private"}
            disabled={busy}
            onClick={selectPrivate}
          />
          <ShareOption
            icon="globe"
            title="Create public link"
            subtitle="Anyone with the link can view"
            selected={choice === "public"}
            disabled={busy}
            onClick={selectPublic}
          />
        </div>

        {isShared && choice === "public" && (
          <div className="mt-4 flex items-center gap-1 rounded-lg border border-[#454540] bg-[#2a2a28] py-1 pl-3 pr-1">
            {/* The URL fades out on the right (mask) so it never runs under the button. */}
            <span
              title={absoluteUrl}
              className="min-w-0 flex-1 overflow-hidden whitespace-nowrap text-sm text-[#cfcbc1]"
              style={{
                WebkitMaskImage: "linear-gradient(to right, #000 calc(100% - 28px), transparent)",
                maskImage: "linear-gradient(to right, #000 calc(100% - 28px), transparent)",
              }}
            >
              {absoluteUrl}
            </span>
            <button
              type="button"
              className="shrink-0 rounded-md bg-[#f4f0e8] px-3 py-1.5 text-sm font-medium text-[#1c1c19] transition-colors hover:bg-white disabled:opacity-50"
              disabled={busy}
              onClick={() => void copyLink()}
            >
              {copied ? "Copied" : "Copy link"}
            </button>
          </div>
        )}

        {/* Disclaimer — Claude's wording minus the Usage Policy link (loom has none). */}
        <p className="mt-4 text-xs leading-relaxed text-[#8a857b]">
          Don’t share personal information or third-party content without permission.
        </p>

        {!isShared && (
          <div className="mt-3 flex justify-end">
            <button
              type="button"
              className="rounded-lg bg-[#f4f0e8] px-4 py-2 text-sm font-medium text-[#1c1c19] transition-colors hover:bg-white disabled:opacity-50"
              disabled={busy || choice !== "public"}
              onClick={selectPublic}
            >
              Create share link
            </button>
          </div>
        )}

        {error !== null && <p className="mt-3 text-xs text-[#d98278]">{error}</p>}
      </div>
    </div>
  );
}

function ShareOption({
  icon,
  title,
  subtitle,
  selected,
  disabled,
  onClick,
}: {
  icon: IconName;
  title: string;
  subtitle: string;
  selected: boolean;
  disabled: boolean;
  onClick(): void;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={`flex w-full items-center gap-3 px-3.5 py-3 text-left transition-colors disabled:cursor-default ${
        selected ? "bg-[#3f3f3a]" : "hover:bg-[#3a3a36]"
      }`}
    >
      <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-[#2a2a28] text-[#cfcbc1]">
        <Icon name={icon} size="18px" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium text-[#f4f0e8]">{title}</span>
        <span className="block text-sm text-[#a8a399]">{subtitle}</span>
      </span>
      {selected && <Icon name="check" size="18px" className="shrink-0 text-[#5599e7]" />}
    </button>
  );
}
