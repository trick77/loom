import { SidebarOpenButton } from "./SidebarOpenButton";
import { UserMemoryPanel } from "./UserMemoryPanel";

/**
 * MemoryPage hosts the user's global memory — the durable personal facts the
 * assistant reuses across every chat. It mirrors the other full-page views
 * (Artifacts, Projects) and renders the read-only UserMemoryPanel.
 */
export function MemoryPage({ onOpenSidebar }: { onOpenSidebar(): void }) {
  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-[640px] px-4 pb-16 pt-10 md:px-6">
        <header className="flex min-w-0 items-center gap-2">
          <SidebarOpenButton onClick={onOpenSidebar} />
          <h1 className="font-serif text-[28px] font-medium leading-8 text-[#f4f0e8]">Memory</h1>
        </header>
        <div className="mt-6">
          <UserMemoryPanel />
        </div>
      </div>
    </div>
  );
}
