import { useEffect, useState } from "react";

import { getProjectMemory, refreshProjectMemory } from "../api";
import { Icon } from "../chat/Icon";

/**
 * ProjectMemoryPanel shows the project's auto-generated shared memory — the
 * compact digest injected into every chat in the project so sibling chats stay
 * aware of each other. It is read-only with a manual refresh (full rebuild).
 */
export function ProjectMemoryPanel({ projectId }: { projectId: string }) {
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  useEffect(() => {
    let active = true;
    setLoading(true);
    getProjectMemory(projectId)
      .then((memory) => {
        if (active) setContent(memory.content);
      })
      .catch(() => {
        if (active) setContent("");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [projectId]);

  function handleRefresh() {
    setRefreshing(true);
    refreshProjectMemory(projectId)
      .then((memory) => setContent(memory.content))
      .catch(() => undefined)
      .finally(() => setRefreshing(false));
  }

  const hasContent = content.trim() !== "";

  return (
    <section
      aria-label="Project memory"
      className="rounded-2xl border border-[#343432] bg-[#1f1f1d] p-5"
    >
      <div className="flex items-start justify-between gap-3">
        <Icon name="wave" size="22px" className="text-[#d5d2c9]" label="Project memory" />
        <button
          type="button"
          className="ui-meta-text text-[#8f8b82] hover:text-[#c7c5bd] disabled:opacity-50"
          onClick={handleRefresh}
          disabled={refreshing || loading}
        >
          {refreshing ? "Refreshing…" : "Refresh"}
        </button>
      </div>
      <h2 className="mt-3 text-sm font-medium text-[#ecece6]">Project memory</h2>
      {loading ? (
        <p className="mt-2 text-sm text-[#807d74]">Loading…</p>
      ) : hasContent ? (
        <p className="mt-2 whitespace-pre-wrap text-sm leading-5 text-[#c7c5bd]" data-project-memory-content>
          {content}
        </p>
      ) : (
        <p className="mt-2 text-sm leading-5 text-[#807d74]">
          Project memory will show here after a few chats.
        </p>
      )}
    </section>
  );
}
