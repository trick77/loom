import { useEffect, useRef, useState } from "react";

import { attachAcceptedFiles, isFileDrag } from "./attachmentFiles";

export function WindowFileDrop({
  enabled,
  onAttachFiles,
  onAttachError,
}: {
  enabled: boolean;
  onAttachFiles(files: File[]): void;
  onAttachError(message: string): void;
}) {
  const [isDragging, setIsDragging] = useState(false);
  const dragDepth = useRef(0);

  useEffect(() => {
    if (!enabled) return undefined;

    function handleDragEnter(event: DragEvent) {
      if (!isFileDrag(event)) return;
      event.preventDefault();
      dragDepth.current += 1;
      setIsDragging(true);
    }

    function handleDragOver(event: DragEvent) {
      if (!isFileDrag(event)) return;
      event.preventDefault();
      if (event.dataTransfer !== null) event.dataTransfer.dropEffect = "copy";
    }

    function handleDragLeave(event: DragEvent) {
      if (!isFileDrag(event)) return;
      event.preventDefault();
      dragDepth.current = Math.max(0, dragDepth.current - 1);
      if (dragDepth.current === 0) setIsDragging(false);
    }

    function handleDrop(event: DragEvent) {
      if (!isFileDrag(event)) return;
      event.preventDefault();
      dragDepth.current = 0;
      setIsDragging(false);
      const files = Array.from(event.dataTransfer?.files ?? []);
      if (files.length === 0) return;
      attachAcceptedFiles({ files, onAttachFiles, onAttachError });
    }

    window.addEventListener("dragenter", handleDragEnter);
    window.addEventListener("dragover", handleDragOver);
    window.addEventListener("dragleave", handleDragLeave);
    window.addEventListener("drop", handleDrop);
    return () => {
      window.removeEventListener("dragenter", handleDragEnter);
      window.removeEventListener("dragover", handleDragOver);
      window.removeEventListener("dragleave", handleDragLeave);
      window.removeEventListener("drop", handleDrop);
    };
  }, [enabled, onAttachError, onAttachFiles]);

  if (!enabled || !isDragging) return null;

  return (
    <div className="pointer-events-none fixed inset-0 z-50 grid place-items-center bg-bg/82 text-[#f3f0e8] backdrop-blur-[2px]">
      <div className="flex flex-col items-center gap-3 text-center">
        <div
          className="text-[52px] leading-none text-[#c7c5bd]"
          style={{ fontFamily: '"Anthropic Icons"' }}
          aria-hidden="true"
        >
          {"\ue06d"}
        </div>
        <div className="ui-composer-text text-[#aaa79e]">
          Drop files here to add it to the conversation
        </div>
      </div>
    </div>
  );
}
