import { useCallback, useState } from "react";

import { indexDocument, listDocuments, uploadDocument } from "../api";

// Shared "+" composer attachment flow: upload a picked file, add it to knowledge,
// and surface ingestion progress via attachNote. Scope decides where the document
// lands for retrieval: projectId scopes it to a project; omitting it makes the
// document user-global. threadId is provenance only and does not affect retrieval.
export function useDocumentAttachments(scope: { threadId?: string; projectId?: string }) {
  const { threadId, projectId } = scope;
  const [attachNote, setAttachNote] = useState("");
  const handleAttachFiles = useCallback(
    (files: File[]) => {
      // Poll the document list until ingestion (which runs server-side in the
      // background) reaches a terminal state, so the note reflects real status
      // rather than just "request accepted".
      const waitForIngestion = async (documentId: string, filename: string) => {
        for (let attempt = 0; attempt < 40; attempt += 1) {
          await new Promise((resolve) => setTimeout(resolve, 1500));
          let docs;
          try {
            docs = await listDocuments(projectId);
          } catch {
            continue;
          }
          const current = docs.find((d) => d.id === documentId);
          if (current === undefined) continue;
          if (current.status === "embedded") {
            setAttachNote(`Added ${filename} to knowledge.`);
            return;
          }
          if (current.status === "error" || current.status === "stale") {
            setAttachNote(`Could not index ${filename}${current.error ? `: ${current.error}` : "."}`);
            return;
          }
        }
        setAttachNote(`${filename} is still processing…`);
      };

      void (async () => {
        for (const file of files) {
          setAttachNote(`Uploading ${file.name}…`);
          try {
            const doc = await uploadDocument(file, { threadId, projectId });
            // Composer uploads are added to knowledge automatically.
            setAttachNote(`Processing ${file.name}…`);
            await indexDocument(doc.id);
            await waitForIngestion(doc.id, file.name);
          } catch (error) {
            setAttachNote(error instanceof Error ? error.message : `Failed to upload ${file.name}.`);
            return;
          }
        }
      })();
    },
    [threadId, projectId],
  );

  return { attachNote, handleAttachFiles };
}
