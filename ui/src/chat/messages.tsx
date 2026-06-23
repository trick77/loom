import {
  type ComponentPropsWithoutRef,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type { ExtraProps } from "react-markdown";
import Markdown from "react-markdown";
import rehypeHighlight from "rehype-highlight";
import remarkGfm from "remark-gfm";

import { type ContentBlock, type Message } from "../api";
import { MessageMetrics } from "../MessageMetrics";
import { ActivityTracePanel } from "./ActivityTracePanel";
import {
  downloadableResponse,
  formatReceivedKB,
  markdownToPlainText,
  pendingFencedArtifact,
  type DownloadableResponse,
} from "./artifacts";
import { messageBlocks } from "./contentBlocks";
import { AttachmentPreview, isRevocablePreview } from "../components/AttachmentPreview";
import { formatAttachmentSize } from "./attachmentFiles";
import { MessageCitations } from "./Citations";
import { GeneratedArtifactCard } from "./GeneratedArtifactCard";
import { CheckIcon, DownloadIcon, FileIcon } from "./icons";
import { Icon } from "./Icon";
import { ImageLightbox } from "./ImageLightbox";
import { rehypeStreamFade } from "./streamFade";
import { isImageAttachment, type ComposerAttachment } from "./useDocumentAttachments";

export function MessageBubble({
  message,
  retryContent,
  onRetry,
  category,
}: {
  message: Message & { attachments?: ComposerAttachment[] };
  retryContent: string | null;
  onRetry(content: string): void;
  /** Thread-level prompt-classifier category, shown as a pill in the assistant metrics row. */
  category?: string;
}) {
  if (message.role === "user") {
    return (
      <div className="ui-user-message group ml-auto w-fit max-w-full md:max-w-[38.25rem]">
        {message.attachments !== undefined && message.attachments.length > 0 && (
          <SentAttachments attachments={message.attachments} />
        )}
        {message.content !== "" && (
          <div className="ui-message-text ui-user-message-text mt-2 rounded-xl bg-[#111110] px-4 py-3 text-[#f3f0e8]">
            {message.content}
          </div>
        )}
        <MessageActions
          copyLabel="Copy message"
          copyText={message.content}
          retryLabel="Retry message"
          onRetry={() => onRetry(message.content)}
          alignRight
        />
      </div>
    );
  }
  // Render the assistant message as a single ordered list of content blocks
  // (text / trace / artifact) so prose, tool-activity panels and images appear in
  // the exact chronological order they arrived. The copy/retry/TTS + metrics
  // footer renders ONCE at the bottom; past trace panels render collapsed and
  // inactive.
  const blocks = messageBlocks(message);
  const proseText = blocks
    .filter((block): block is Extract<ContentBlock, { type: "text" }> => block.type === "text")
    .map((block) => block.content)
    .join("\n\n");
  return (
    <div className="max-w-[46rem] space-y-3">
      {blocks.map((block, index) => (
        <AssistantBlock key={`${message.id}-block-${index}`} block={block} />
      ))}
      <MessageActions
        copyLabel="Copy response"
        copyText={markdownToPlainText(proseText)}
        retryLabel="Retry response"
        onRetry={retryContent === null ? undefined : () => onRetry(retryContent)}
        metricsMessage={message}
        category={category}
        speakable
      />
      <MessageCitations citations={message.citations} />
    </div>
  );
}

// AssistantBlock renders one committed content block. Trace blocks become a
// collapsed, inactive activity panel; text blocks render prose (with
// downloadable/pending-fenced-artifact detection); artifact blocks render the
// generated-artifact card.
function AssistantBlock({ block }: { block: ContentBlock }) {
  if (block.type === "trace") {
    return <ActivityTracePanel events={block.events} active={false} />;
  }
  if (block.type === "artifact") {
    return <GeneratedArtifactCard artifact={block.artifact} />;
  }
  return <AssistantProse>{block.content}</AssistantProse>;
}

function SentAttachments({ attachments }: { attachments: ComposerAttachment[] }) {
  const imageAttachments = attachments.filter(isImageAttachment);
  const fileAttachments = attachments.filter((attachment) => !isImageAttachment(attachment));
  return (
    <div className="mb-2 space-y-2">
      {imageAttachments.length > 0 && (
        <div className="flex flex-wrap justify-end gap-2">
          {imageAttachments.map((attachment) => (
            <SentImageAttachment key={attachment.id} attachment={attachment} />
          ))}
        </div>
      )}
      {fileAttachments.length > 0 && (
        <div className="flex flex-wrap justify-end gap-2">
          {fileAttachments.map((attachment) => (
            <SentFileAttachment key={attachment.id} attachment={attachment} />
          ))}
        </div>
      )}
    </div>
  );
}

function useRevokeSentPreview(previewUrl: string | undefined) {
  useEffect(() => {
    const currentPreviewUrl = previewUrl;
    return () => {
      if (isRevocablePreview(currentPreviewUrl)) URL.revokeObjectURL(currentPreviewUrl);
    };
  }, [previewUrl]);
}

function SentImageAttachment({ attachment }: { attachment: ComposerAttachment }) {
  useRevokeSentPreview(attachment.previewUrl);

  // Once sent, the image lives on the server as an artifact. Render it from that
  // stable download URL rather than the composer's ephemeral object URL, which is
  // revoked the moment the start screen is left (and gone after a reload) — that
  // revocation is exactly what turned the thumbnail into a placeholder. Fall back
  // to the blob only if the id is somehow missing. (Full reload-survival, i.e.
  // persisting the attachment record, is the later persistence stage.)
  const src =
    attachment.artifactId !== undefined
      ? `/api/artifacts/${encodeURIComponent(attachment.artifactId)}/download`
      : attachment.previewUrl;

  return (
    <AttachmentPreview
      mimeType={attachment.mimeType}
      filename={attachment.filename}
      previewUrl={src}
      overlayLabel
      testId="sent-image-attachment"
      className="h-[76px] w-[76px] overflow-hidden rounded-lg border border-[#3e3d39] bg-[#242421]"
    />
  );
}

function SentFileAttachment({ attachment }: { attachment: ComposerAttachment }) {
  useRevokeSentPreview(attachment.previewUrl);

  return (
    <div className="flex h-[92px] w-[210px] max-w-full overflow-hidden rounded-lg border border-[#3e3d39] bg-[#282826] text-left text-[#f3f0e8]">
      <AttachmentPreview
        mimeType={attachment.mimeType}
        filename={attachment.filename}
        previewUrl={attachment.previewUrl}
        className="grid h-full w-[82px] shrink-0 place-items-center bg-[#242421] text-[#c9c5bb]"
        fallbackBoxClassName="grid h-11 w-11 place-items-center rounded-md border border-[#55534d] bg-[#30302d]"
      />
      <div className="min-w-0 flex-1 px-3 py-2.5">
        <div className="ui-message-text truncate text-sm">{attachment.filename}</div>
        <div className="ui-meta-text mt-2 truncate text-[#aaa79e]">{sentAttachmentStatus(attachment)}</div>
      </div>
    </div>
  );
}

function sentAttachmentStatus(attachment: ComposerAttachment): string {
  if (attachment.status === "queued") return "Attached";
  if (attachment.status === "uploading") return "Uploading...";
  if (attachment.status === "processing") return "Processing...";
  if (attachment.status === "error") return attachment.error ?? "Upload failed";
  return formatAttachmentSize(attachment.sizeBytes);
}

function CodeBlock({ children, node: _node, ...props }: ComponentPropsWithoutRef<"pre"> & ExtraProps) {
  const preRef = useRef<HTMLPreElement | null>(null);
  const [copied, setCopied] = useState(false);
  const resetRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (resetRef.current !== null) window.clearTimeout(resetRef.current);
    };
  }, []);

  const handleCopy = useCallback(() => {
    const code = preRef.current?.textContent ?? "";
    void copyResponse(code);
    setCopied(true);
    if (resetRef.current !== null) window.clearTimeout(resetRef.current);
    resetRef.current = window.setTimeout(() => setCopied(false), 1500);
  }, []);

  return (
    <div className="ui-codeblock">
      <button
        type="button"
        className="ui-codeblock-copy"
        onClick={handleCopy}
        aria-label={copied ? "Kopiert" : "Code kopieren"}
        title={copied ? "Kopiert" : "Code kopieren"}
      >
        {copied ? <CheckIcon className="h-4 w-4" /> : <Icon name="copy" size="1rem" />}
      </button>
      <pre ref={preRef} {...props}>
        {children}
      </pre>
    </div>
  );
}

export function ProseMarkdown({
  children,
  streaming = false,
}: {
  children: string;
  streaming?: boolean;
}) {
  return (
    <div className="ui-message-text ui-markdown text-[#f3f0e8]">
      <Markdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={streaming ? [rehypeHighlight, rehypeStreamFade] : [rehypeHighlight]}
        components={{
          a({ children, ...props }) {
            return (
              <a {...props} target="_blank" rel="noreferrer">
                {children}
              </a>
            );
          },
          pre: CodeBlock,
        }}
      >
        {children}
      </Markdown>
    </div>
  );
}

// AssistantProse renders one run of assistant prose with the
// downloadable/pending-fenced-artifact detection, but no action/metrics row — the
// single bottom footer is rendered once per message by MessageBubble. Used for
// each committed text block and for the live streaming text block.
export function AssistantProse({
  children,
  streaming = false,
}: {
  children: string;
  streaming?: boolean;
}) {
  // Memoize on the text so the parsed artifact keeps a stable identity across
  // re-renders. SvgResponseBubble's blob effect keys on artifact.content; for a
  // data-URL SVG that content is a freshly-allocated buffer each parse, so without
  // this the effect would revoke + recreate the object URL on every parent render.
  const downloadable = useMemo(() => downloadableResponse(children), [children]);
  const pendingArtifact = useMemo(() => pendingFencedArtifact(children), [children]);

  if (downloadable !== null) {
    const { artifact, before, after } = downloadable;
    // SVG renders inline like a raster image; every other downloadable format
    // shows the plain download card.
    const Bubble = artifact.extension === "svg" ? SvgResponseBubble : DownloadResponseBubble;
    if (before === "" && after === "") {
      return <Bubble artifact={artifact} />;
    }
    return (
      <div className="ui-assistant-message group w-full space-y-3">
        {before !== "" && <ProseMarkdown streaming={streaming}>{before}</ProseMarkdown>}
        <Bubble artifact={artifact} />
        {after !== "" && <ProseMarkdown streaming={streaming}>{after}</ProseMarkdown>}
      </div>
    );
  }

  if (pendingArtifact !== null) {
    const { before, label, receivedBytes } = pendingArtifact;
    if (before === "") {
      return <PendingDownloadResponseBubble label={label} receivedBytes={receivedBytes} />;
    }
    return (
      <div className="ui-assistant-message group w-full space-y-3">
        <ProseMarkdown streaming={streaming}>{before}</ProseMarkdown>
        <PendingDownloadResponseBubble label={label} receivedBytes={receivedBytes} />
      </div>
    );
  }

  return (
    <div className="ui-assistant-message group w-full">
      <ProseMarkdown streaming={streaming}>{children}</ProseMarkdown>
    </div>
  );
}

function MessageActions({
  copyLabel,
  copyText,
  retryLabel,
  onRetry,
  metricsMessage,
  category,
  speakable = false,
  alignRight = false,
  streaming = false,
}: {
  copyLabel: string;
  copyText: string;
  retryLabel: string;
  onRetry?: () => void;
  metricsMessage?: Message;
  category?: string;
  speakable?: boolean;
  alignRight?: boolean;
  streaming?: boolean;
}) {
  const [copied, setCopied] = useState(false);
  const [speaking, setSpeaking] = useState(false);
  const speakingRef = useRef(false);
  speakingRef.current = speaking;

  // Stop any in-progress narration started here when the bubble unmounts.
  useEffect(() => () => void (speakingRef.current && window.speechSynthesis?.cancel()), []);

  async function handleCopy() {
    await copyResponse(copyText);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1200);
  }

  function endSpeech() {
    window.speechSynthesis?.cancel();
    setSpeaking(false);
  }

  // Replicates AnythingLLM's native TTS exactly (it works reliably in Safari):
  // guard on the engine's own `speaking` flag, no cancel()/resume() before speak,
  // attach an `end` listener, then speak and flag speaking.
  function handleSpeak() {
    const synth = window.speechSynthesis;
    if (!synth) return;
    // Pausing this message while it speaks ends it; if another message is
    // speaking, ignore the click until that one is paused.
    if (synth.speaking && speakingRef.current) {
      endSpeech();
      return;
    }
    if (synth.speaking && !speakingRef.current) return;
    const utterance = new SpeechSynthesisUtterance(copyText);
    utterance.addEventListener("end", endSpeech);
    synth.speak(utterance);
    setSpeaking(true);
  }

  // While the answer is still streaming we render no action row at all: the
  // speaker, copy and retry icons appear together with the metrics footer
  // (model name, token cost) only once the message has settled.
  if (streaming) return null;

  return (
    <div className={`mt-2 flex items-center gap-1 ${alignRight ? "justify-end" : ""}`}>
      {speakable && (
        <button
          className={`grid h-6 w-6 place-items-center transition-colors hover:text-[#f3f0e8] ${
            speaking ? "text-[#f3f0e8]" : "text-[#858178]"
          }`}
          onClick={handleSpeak}
          type="button"
          title={speaking ? "Stop" : "Read aloud"}
          aria-label={speaking ? "Stop reading" : "Read aloud"}
        >
          <Icon name="volume" size="1.15rem" />
        </button>
      )}
      <button
        className="grid h-6 w-6 place-items-center text-[#858178] hover:text-[#f3f0e8]"
        onClick={handleCopy}
        type="button"
        title="Copy"
        aria-label={copyLabel}
      >
        {copied ? <CheckIcon className="h-[1.15rem] w-[1.15rem]" /> : <Icon name="copy" size="1.15rem" />}
      </button>
      {onRetry !== undefined && (
        <button
          className="grid h-6 w-6 place-items-center text-[#858178] hover:text-[#f3f0e8]"
          onClick={onRetry}
          type="button"
          title="Retry"
          aria-label={retryLabel}
        >
          <Icon name="retry" size="1.15rem" />
        </button>
      )}
      {metricsMessage && <MessageMetrics message={metricsMessage} category={category} />}
    </div>
  );
}

function PendingDownloadResponseBubble({ label, receivedBytes }: { label: string; receivedBytes: number }) {
  const progressText =
    receivedBytes > 0 ? `Receiving file... ${formatReceivedKB(receivedBytes)} received` : "Receiving file...";
  return (
    <div className="max-w-[26rem] rounded-lg border border-[#3e3d39] bg-[#282826] px-4 py-3 text-[#f3f0e8]">
      <div className="flex items-center gap-3">
        <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
          <FileIcon />
        </div>
        <div className="min-w-0 flex-1">
          <div className="ui-message-text truncate">{label} response</div>
          <div className="ui-meta-text text-[#aaa79e]">{progressText}</div>
        </div>
      </div>
    </div>
  );
}

function DownloadResponseBubble({ artifact }: { artifact: DownloadableResponse }) {
  return (
    <div className="max-w-[26rem] rounded-lg border border-[#3e3d39] bg-[#282826] px-4 py-3 text-[#f3f0e8]">
      <div className="flex items-center gap-3">
        <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
          <FileIcon />
        </div>
        <div className="min-w-0 flex-1">
          <div className="ui-message-text truncate">{artifact.label} response</div>
          <div className="ui-meta-text text-[#aaa79e]">Ready to download</div>
        </div>
        <button
          className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd] transition-colors hover:bg-[#454540] hover:text-[#f3f0e8]"
          onClick={() => downloadEmbeddedArtifact(artifact)}
          type="button"
          title={`Download ${artifact.label} response`}
          aria-label={`Download ${artifact.label} response`}
        >
          <DownloadIcon />
        </button>
      </div>
    </div>
  );
}

// SvgResponseBubble renders a model-emitted SVG inline, the same way generated
// raster images appear, instead of offering only a download. The SVG is loaded
// through an <img> blob URL — never inline DOM or an <iframe> — so the browser's
// secure-image mode applies and any embedded <script>/onload in the (semi-trusted)
// model output cannot execute. The blob is typed image/svg+xml explicitly because
// <img> ignores blobs declared as anything else. SVGs usually carry only a
// viewBox (no intrinsic size), so the preview uses a min-height floor rather than
// reserving an aspect-ratio box.
function SvgResponseBubble({ artifact }: { artifact: DownloadableResponse }) {
  const [previewUrl, setPreviewUrl] = useState("");
  const [lightboxOpen, setLightboxOpen] = useState(false);

  useEffect(() => {
    const url = URL.createObjectURL(new Blob([artifact.content], { type: "image/svg+xml" }));
    setPreviewUrl(url);
    return () => URL.revokeObjectURL(url);
  }, [artifact.content]);

  const altText = `${artifact.label} response`;

  return (
    <div className="max-w-[28rem] overflow-hidden rounded-lg border border-[#3e3d39] bg-[#282826] text-[#f3f0e8]">
      <button
        className="block min-h-[16rem] w-full cursor-zoom-in bg-[#1f1f1d]"
        onClick={() => previewUrl !== "" && setLightboxOpen(true)}
        type="button"
        title={`Preview ${altText}`}
        aria-label={`Preview ${altText}`}
      >
        {previewUrl !== "" && (
          <img className="block max-h-[28rem] w-full object-contain" src={previewUrl} alt={altText} loading="lazy" />
        )}
      </button>
      <div className="flex items-center gap-3 px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="ui-message-text truncate">{altText}</div>
          <div className="ui-meta-text text-[#aaa79e]">Ready to download</div>
        </div>
        <button
          className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd] transition-colors hover:bg-[#454540] hover:text-[#f3f0e8]"
          onClick={() => downloadEmbeddedArtifact(artifact)}
          type="button"
          title={`Download ${altText}`}
          aria-label={`Download ${altText}`}
        >
          <DownloadIcon />
        </button>
      </div>
      {lightboxOpen && previewUrl !== "" && (
        <ImageLightbox src={previewUrl} alt={altText} onClose={() => setLightboxOpen(false)} fill />
      )}
    </div>
  );
}

async function copyResponse(content: string) {
  await navigator.clipboard?.writeText(content);
}

function downloadEmbeddedArtifact(artifact: DownloadableResponse) {
  const url = URL.createObjectURL(new Blob([artifact.content], { type: artifact.mimeType }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `ui-response.${artifact.extension}`;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
