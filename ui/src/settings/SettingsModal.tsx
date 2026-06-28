import { useEffect, useState } from "react";

import { Icon, type IconName } from "../chat/Icon";
import { SharedChatsPanel } from "./SharedChatsPanel";
import { UsagePanel } from "./UsagePanel";

type SettingsSection = "usage" | "shares";

const SECTIONS: { id: SettingsSection; label: string; icon: IconName }[] = [
  { id: "usage", label: "Usage", icon: "sliders" },
  { id: "shares", label: "Shared chats", icon: "upload" },
];

/**
 * SettingsModal — centered overlay modal with a left nav (Usage, Shared chats).
 * There is deliberately no search box (per design).
 */
export function SettingsModal({ onClose }: { onClose(): void }) {
  const [section, setSection] = useState<SettingsSection>("usage");

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="Settings"
      onClick={onClose}
    >
      <div
        className="flex h-[560px] w-full max-w-[960px] overflow-hidden rounded-2xl border border-[#343432] bg-[#262624] shadow-[0_24px_60px_rgba(0,0,0,0.5)]"
        onClick={(event) => event.stopPropagation()}
      >
        <nav className="w-[220px] shrink-0 border-r border-[#343432] bg-[#21211f] p-3">
          <div className="px-2 pb-2 pt-1 text-xs font-medium uppercase tracking-wide text-[#807d74]">
            Settings
          </div>
          {SECTIONS.map((entry) => (
            <button
              key={entry.id}
              className={`flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-sm transition-colors ${
                section === entry.id
                  ? "bg-[#343433] text-[#f4f0e8]"
                  : "text-[#cfcbc1] hover:bg-[#2a2a28]"
              }`}
              type="button"
              aria-current={section === entry.id ? "page" : undefined}
              onClick={() => setSection(entry.id)}
            >
              <Icon name={entry.icon} size="18px" className="shrink-0" />
              {entry.label}
            </button>
          ))}
        </nav>
        <div className="relative flex-1 overflow-y-auto p-6">
          <button
            className="absolute right-4 top-4 grid h-8 w-8 place-items-center rounded-md text-[#aaa79e] hover:bg-[#2a2a28]"
            type="button"
            aria-label="Close settings"
            onClick={onClose}
          >
            <Icon name="close" size="18px" />
          </button>
          {section === "usage" ? <UsagePanel /> : <SharedChatsPanel />}
        </div>
      </div>
    </div>
  );
}
