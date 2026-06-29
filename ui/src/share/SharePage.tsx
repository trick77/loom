import { useEffect, useState } from "react";

import {
  getPublicShare,
  ShareNotFoundError,
  type Message,
  type PublicShare,
  type PublicShareMessage,
} from "../api";
import { MessageBubble } from "../chat/messages";
import loomLogo from "../assets/loom-logo.svg";

type Status = "loading" | "not-found" | "error" | "ready";

// SharePage is the standalone, read-only public viewer. It is mounted directly by
// main.tsx (outside <App/>) so a logged-out visitor never hits the auth gate. It
// renders only the frozen snapshot: title, "Shared by", a notice, and the
// transcript — no sidebar, composer, actions, metrics, or citations.
export function SharePage({ shareId }: { shareId: string }) {
  const [status, setStatus] = useState<Status>("loading");
  const [share, setShare] = useState<PublicShare | null>(null);

  useEffect(() => {
    // Defense in depth alongside the X-Robots-Tag header: keep crawlers out.
    const meta = document.createElement("meta");
    meta.name = "robots";
    meta.content = "noindex, nofollow";
    document.head.appendChild(meta);
    return () => {
      document.head.removeChild(meta);
    };
  }, []);

  useEffect(() => {
    let active = true;
    setStatus("loading");
    getPublicShare(shareId)
      .then((loaded) => {
        if (!active) return;
        setShare(loaded);
        setStatus("ready");
        if (loaded.title !== "") document.title = `${loaded.title} · Loom`;
      })
      .catch((err) => {
        if (!active) return;
        setStatus(err instanceof ShareNotFoundError ? "not-found" : "error");
      });
    return () => {
      active = false;
    };
  }, [shareId]);

  if (status === "loading") {
    return <CenteredNotice>Loading…</CenteredNotice>;
  }
  if (status === "not-found") {
    return (
      <CenteredNotice>
        This conversation isn’t available. The link may have been disabled or removed.
      </CenteredNotice>
    );
  }
  if (status === "error" || share === null) {
    return <CenteredNotice>Something went wrong loading this conversation.</CenteredNotice>;
  }

  return (
    <main className="flex min-h-svh flex-col bg-bg font-sans text-ink">
      <header className="flex shrink-0 items-center justify-between gap-3 border-b border-[#252523] px-4 py-3 sm:px-6">
        <div className="flex min-w-0 items-center gap-2">
          <img src={loomLogo} alt="" aria-hidden className="h-5 w-5 shrink-0" />
          <h1 className="min-w-0 truncate font-sans text-sm font-normal text-[#d5d2c9]">
            {share.title || "Shared conversation"}
          </h1>
        </div>
        {share.author !== "" && (
          <span className="shrink-0 rounded-md bg-[#46453f] px-2.5 py-0.5 text-sm text-[#d6d3ca]">
            Shared by {share.author}
          </span>
        )}
      </header>

      <div className="flex-1 overflow-y-auto px-6 pt-8 [scrollbar-gutter:stable_both-edges] md:px-8">
        <div className="ui-thread-rail mx-auto w-full max-w-[720px] space-y-6 pb-16">
          <ShareNotice />
          {share.messages.map((message) => (
            <div key={message.id} className="space-y-6">
              <MessageBubble message={toRenderMessage(message)} retryContent={null} publicView />
            </div>
          ))}
        </div>
      </div>
    </main>
  );
}

// ShareNotice is the thin info banner above the transcript. It mirrors Claude's
// notice but drops the "Report" affordance and the vendor framing.
function ShareNotice() {
  return (
    <div className="flex items-start gap-2 rounded-lg border border-[#34342f] bg-[#262622] px-4 py-3 text-[13px] leading-relaxed text-[#9a958b]">
      <span aria-hidden className="mt-px text-[#7f7b72]">
        ⓘ
      </span>
      <p>This is a shared copy of a conversation. Some content, such as uploaded files, may not be shown here.</p>
    </div>
  );
}

function CenteredNotice({ children }: { children: React.ReactNode }) {
  return (
    <main className="flex min-h-svh items-center justify-center bg-bg px-6 text-center font-sans text-muted">
      <div className="flex max-w-md flex-col items-center gap-4">
        <img src={loomLogo} alt="" aria-hidden className="h-10 w-10" />
        <p>{children}</p>
      </div>
    </main>
  );
}

// toRenderMessage adapts a sanitized snapshot message to the shape MessageBubble
// renders. Fields the public snapshot omits (attachments, citations, tokens) are
// simply absent; publicView mode never reads them.
function toRenderMessage(message: PublicShareMessage): Message & { hadAttachment?: boolean } {
  return {
    id: message.id,
    threadId: "",
    role: message.role,
    content: message.content,
    artifacts: message.artifacts,
    contentBlocks: message.contentBlocks,
    createdAt: message.createdAt,
    hadAttachment: message.hadAttachment,
  };
}
