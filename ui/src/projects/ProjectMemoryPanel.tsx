import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { editProjectMemory, getProjectMemory } from "../api";
import { Icon } from "../chat/Icon";
import { MemoryMarkdown } from "../MemoryMarkdown";
import { MemoryComposer, useDismissOnOutside } from "../MemoryEditComposer";

/**
 * ProjectMemoryPanel shows the project's auto-generated shared memory — the
 * compact digest injected into every thread in the project so sibling threads stay
 * aware of each other. It is read-only; the memory refreshes automatically in
 * the background after threads.
 */
export function ProjectMemoryPanel({ projectId }: { projectId: string }) {
  const { t } = useTranslation();
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(true);
  const [composerOpen, setComposerOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  // Tracks the panel's current project so an edit that resolves after the panel
  // has switched projects does not write the stale result into the new project.
  const projectIdRef = useRef(projectId);
  useEffect(() => {
    projectIdRef.current = projectId;
  }, [projectId]);

  async function handleEdit(instruction: string) {
    setPending(true);
    setError(undefined);
    try {
      const updated = await editProjectMemory(projectId, instruction);
      if (projectIdRef.current !== projectId) return;
      setContent(updated.content);
      setComposerOpen(false);
    } catch {
      if (projectIdRef.current === projectId) {
        setError(t("projects.memory.editError"));
      }
    } finally {
      setPending(false);
    }
  }

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

  const hasContent = content.trim() !== "";

  const containerRef = useRef<HTMLDivElement>(null);
  useDismissOnOutside(composerOpen, containerRef, () => setComposerOpen(false));

  return (
    <div className="relative" ref={containerRef}>
      <section
        aria-label={t("projects.memory.label")}
        className="relative overflow-hidden rounded-2xl border border-[#343432] bg-[#1f1f1d]"
      >
        <h2 className="flex items-center gap-1.5 px-5 pt-5 text-[15px] font-medium text-[#ecece6]">
          <Icon name="memory" size="21px" className="text-[#d5d2c9]" />
          <span>{t("projects.memory.label")}</span>
        </h2>

        <p className="mt-1.5 px-5 text-[13px] leading-5 text-[#8a887f]">
          {t("projects.memory.description")}
        </p>

        {loading ? (
          <p className="mt-2 h-[490px] px-5 pb-5 text-sm text-[#8f8b82]">
            {t("projects.memory.loading")}
          </p>
        ) : hasContent ? (
          <div
            className="relative mt-2 h-[490px] text-base text-[#f3f0e8]"
            data-project-memory-content
            data-testid="project-memory-content"
          >
            <div
              className="ui-sidebar-scroll h-full overflow-y-auto"
              data-testid="project-memory-scroll"
            >
              <MemoryMarkdown content={content} className="px-5 pb-8" />
            </div>
            <div
              aria-hidden="true"
              className="pointer-events-none absolute inset-x-0 bottom-0 h-8 bg-gradient-to-t from-[#1f1f1d] to-transparent"
              data-testid="project-memory-bottom-fade"
            />
          </div>
        ) : (
          <p className="mt-2 h-[490px] px-5 pb-5 text-sm leading-5 text-[#8f8b82]">
            {t("projects.memory.empty")}
          </p>
        )}
      </section>
      <MemoryComposer
        open={composerOpen}
        onOpen={() => {
          setError(undefined);
          setComposerOpen(true);
        }}
        onClose={() => setComposerOpen(false)}
        pending={pending}
        error={error}
        onSubmit={handleEdit}
      />
    </div>
  );
}
