import {
  type ComponentPropsWithoutRef,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import type { ExtraProps } from "react-markdown";
import Markdown from "react-markdown";
import rehypeHighlight from "rehype-highlight";
import remarkGfm from "remark-gfm";

import { type Message } from "../api";
import { MessageMetrics } from "../MessageMetrics";
import {
  downloadableResponse,
  formatReceivedKB,
  markdownToPlainText,
  pendingFencedArtifact,
  type DownloadableResponse,
} from "./artifacts";
import { AttachmentExtensionPill } from "./AttachmentExtensionPill";
import { attachmentExtensionLabel } from "./attachmentFiles";
import { MessageCitations } from "./Citations";
import { GeneratedArtifactCard } from "./GeneratedArtifactCard";
import { CheckIcon, DownloadIcon, FileIcon } from "./icons";
import { Icon } from "./Icon";
import { type ComposerAttachment } from "./useDocumentAttachments";

export function MessageBubble({
  message,
  retryContent,
  onRetry,
}: {
  message: Message & { attachments?: ComposerAttachment[] };
  retryContent: string | null;
  onRetry(content: string): void;
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
  return (
    <div className="max-w-[46rem] space-y-3">
      <AssistantText metricsMessage={message} onRetry={retryContent === null ? undefined : () => onRetry(retryContent)}>
        {message.content}
      </AssistantText>
      {message.artifacts?.map((artifact) => (
        <GeneratedArtifactCard key={artifact.id} artifact={artifact} />
      ))}
      <MessageCitations citations={message.citations} />
    </div>
  );
}

function SentAttachments({ attachments }: { attachments: ComposerAttachment[] }) {
  if (attachments.length === 0) return null;
  return (
    <div className="mb-2 space-y-2">
      <div className="flex flex-wrap justify-end gap-2">
        {attachments.map((attachment) => (
          <SentFileAttachment key={attachment.id} attachment={attachment} />
        ))}
      </div>
    </div>
  );
}

function SentFileAttachment({ attachment }: { attachment: ComposerAttachment }) {
  const extensionLabel = attachmentExtensionLabel(attachment.filename);

  return (
    <div className="flex h-[92px] w-[210px] max-w-full overflow-hidden rounded-lg border border-[#3e3d39] bg-[#282826] text-left text-[#f3f0e8]">
      <div className="relative grid h-full w-[82px] shrink-0 place-items-center bg-[#242421]">
        <div className="grid h-11 w-11 place-items-center rounded-md border border-[#55534d] bg-[#30302d] text-[#c9c5bb]">
          <FileIcon />
        </div>
        {extensionLabel !== null && <AttachmentExtensionPill>{extensionLabel}</AttachmentExtensionPill>}
      </div>
      <div className="min-w-0 flex-1 px-3 py-2.5">
        <div className="ui-message-text truncate text-sm">{attachment.filename}</div>
        <div className="ui-meta-text mt-1 truncate text-[#aaa79e]">{sentAttachmentStatus(attachment)}</div>
      </div>
    </div>
  );
}

function sentAttachmentStatus(attachment: ComposerAttachment): string {
  if (attachment.status === "queued") return "Attached";
  if (attachment.status === "uploading") return "Uploading...";
  if (attachment.status === "processing") return "Processing...";
  if (attachment.status === "error") return attachment.error ?? "Upload failed";
  return formatAttachmentBytes(attachment.sizeBytes);
}

function formatAttachmentBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb.toFixed(kb >= 10 ? 0 : 1)} KB`;
  const mb = kb / 1024;
  return `${mb.toFixed(mb >= 10 ? 0 : 1)} MB`;
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

export function ProseMarkdown({ children }: { children: string }) {
  return (
    <div className="ui-message-text ui-markdown text-[#f3f0e8]">
      <Markdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
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

export function AssistantText({
  children,
  onRetry,
  metricsMessage,
  streaming = false,
}: {
  children: string;
  onRetry?: () => void;
  metricsMessage?: Message;
  streaming?: boolean;
}) {
  const downloadable = downloadableResponse(children);

  if (downloadable !== null) {
    const { artifact, before, after } = downloadable;
    if (before === "" && after === "") {
      return <DownloadResponseBubble artifact={artifact} />;
    }
    return (
      <div className="ui-assistant-message group w-full space-y-3">
        {before !== "" && <ProseMarkdown>{before}</ProseMarkdown>}
        <DownloadResponseBubble artifact={artifact} />
        {after !== "" && <ProseMarkdown>{after}</ProseMarkdown>}
        <MessageActions
          copyLabel="Copy response"
          copyText={markdownToPlainText(children)}
          retryLabel="Retry response"
          onRetry={onRetry}
          metricsMessage={metricsMessage}
          streaming={streaming}
          speakable
        />
      </div>
    );
  }

  const pendingArtifact = pendingFencedArtifact(children);
  if (pendingArtifact !== null) {
    const { before, label, receivedBytes } = pendingArtifact;
    if (before === "") {
      return <PendingDownloadResponseBubble label={label} receivedBytes={receivedBytes} />;
    }
    return (
      <div className="ui-assistant-message group w-full space-y-3">
        <ProseMarkdown>{before}</ProseMarkdown>
        <PendingDownloadResponseBubble label={label} receivedBytes={receivedBytes} />
      </div>
    );
  }

  return (
    <div className="ui-assistant-message group w-full">
      <ProseMarkdown>{children}</ProseMarkdown>
      <MessageActions
        copyLabel="Copy response"
        copyText={markdownToPlainText(children)}
        retryLabel="Retry response"
        onRetry={onRetry}
        metricsMessage={metricsMessage}
        streaming={streaming}
        speakable
      />
    </div>
  );
}

function MessageActions({
  copyLabel,
  copyText,
  retryLabel,
  onRetry,
  metricsMessage,
  speakable = false,
  alignRight = false,
  streaming = false,
}: {
  copyLabel: string;
  copyText: string;
  retryLabel: string;
  onRetry?: () => void;
  metricsMessage?: Message;
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
    <div className={`mt-2 flex items-center gap-1 ${alignRight ? "justify-end" : "pl-1.5"}`}>
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
      {metricsMessage && <MessageMetrics message={metricsMessage} />}
    </div>
  );
}

// Shown while a document/image tool is still generating its artifact (before the
// backend emits the `artifact` event). Mirrors the download card layout so it
// settles into place without a layout shift once the real card replaces it. No
// download button (inactive) and no byte count — MiMo buffers the tool arguments,
// so there is no smooth byte signal to stream; the shimmer conveys liveness.
export function PendingArtifactCard({ label }: { label: string }) {
  return (
    <div className="max-w-[28rem] rounded-lg border border-[#3e3d39] bg-[#282826] px-4 py-3 text-[#f3f0e8]">
      <div className="flex items-center gap-3">
        <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
          <FileIcon />
        </div>
        <div className="min-w-0 flex-1">
          <div className="ui-message-text truncate">Generating {label}…</div>
          <span className="ui-thinking-label-active ui-meta-text" data-text="Working…">
            Working…
          </span>
        </div>
      </div>
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
