import { useEffect, useState } from "react";

import { disableShare, getMyShares, type ShareListItem } from "../api";
import { Icon } from "../chat/Icon";

// SharedChatsPanel lists the user's active and disabled shares with copy-link and
// revoke controls. It is the management surface referenced from Settings.
export function SharedChatsPanel() {
  const [shares, setShares] = useState<ShareListItem[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    getMyShares()
      .then((items) => active && setShares(items))
      .catch(() => active && setError("Failed to load shared chats."));
    return () => {
      active = false;
    };
  }, []);

  async function revoke(item: ShareListItem) {
    try {
      await disableShare(item.threadId);
      setShares((current) =>
        (current ?? []).map((s) => (s.threadId === item.threadId ? { ...s, shared: false } : s)),
      );
    } catch {
      setError("Failed to disable the link.");
    }
  }

  async function copy(item: ShareListItem) {
    const url = item.shareUrl.startsWith("http")
      ? item.shareUrl
      : window.location.origin + item.shareUrl;
    try {
      await navigator.clipboard.writeText(url);
      setCopiedId(item.shareId);
      window.setTimeout(() => setCopiedId((id) => (id === item.shareId ? null : id)), 1500);
    } catch {
      setError("Couldn’t copy the link.");
    }
  }

  return (
    <section>
      <h1 className="font-serif text-2xl font-light tracking-tight text-[#f4f0e8]">Shared chats</h1>
      <p className="mt-1 text-sm text-[#a8a399]">
        Public, read-only snapshots of your conversations. Disable a link to revoke access.
      </p>

      {error !== null && <p className="mt-4 text-sm text-[#d98278]">{error}</p>}

      {shares !== null && shares.length === 0 && (
        <p className="mt-6 text-sm text-[#807d74]">You haven’t shared any chats yet.</p>
      )}

      <div className="mt-4 divide-y divide-[#343432] border-y border-[#343432]">
        {(shares ?? []).map((item) => (
          <div key={item.shareId} className="flex items-center gap-3 py-3">
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm text-[#f3f0e8]">{item.title || "Untitled"}</div>
              <div className="mt-0.5 flex items-center gap-2 text-xs text-[#807d74]">
                <span>Snapshot {formatDate(item.snapshotAt)}</span>
                {!item.shared && <span className="text-[#9a8a6a]">· disabled</span>}
              </div>
            </div>
            {item.shared && (
              <button
                type="button"
                className="shrink-0 rounded-md border border-[#3a3a36] px-2.5 py-1 text-xs text-[#d5d2c9] transition-colors hover:bg-[#2a2a28]"
                onClick={() => void copy(item)}
              >
                {copiedId === item.shareId ? "Copied" : "Copy link"}
              </button>
            )}
            {item.shared && (
              <button
                type="button"
                aria-label="Disable link"
                className="shrink-0 rounded-md border border-[#3a3a36] px-2.5 py-1 text-xs text-[#d98278] transition-colors hover:bg-[#d03b3c] hover:text-white"
                onClick={() => void revoke(item)}
              >
                <span className="flex items-center gap-1.5">
                  <Icon name="eyeOff" size="14px" />
                  Disable
                </span>
              </button>
            )}
          </div>
        ))}
      </div>
    </section>
  );
}

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}
