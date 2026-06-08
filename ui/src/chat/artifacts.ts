import type { Artifact } from "../api";
import { formatDuration } from "../metrics";

export type DownloadableResponse = {
  extension: string;
  label: string;
  mimeType: string;
  content: BlobPart;
};

export type EmbeddedArtifact = {
  artifact: DownloadableResponse;
  before: string;
  after: string;
};

export type PendingArtifact = {
  label: string;
  before: string;
  receivedBytes: number;
};

export function buildImageStats(artifact: Artifact): string | null {
  const segments: string[] = [];
  if (artifact.model) segments.push(artifact.model);
  if (artifact.width && artifact.height) segments.push(`${artifact.width}×${artifact.height}`);
  if (artifact.durationMs && artifact.durationMs > 0) segments.push(formatDuration(artifact.durationMs));
  return segments.length > 0 ? segments.join(" · ") : null;
}

export function downloadableResponse(content: string): EmbeddedArtifact | null {
  const dataURL = dataURLArtifact(content);
  if (dataURL !== null) return { artifact: dataURL, before: "", after: "" };

  return fencedArtifact(content);
}

export function pendingFencedArtifact(content: string): PendingArtifact | null {
  const matches = [...content.matchAll(/(?:^|\n)```([a-z0-9_-]+)[ \t]*\n/gi)];
  if (matches.length !== 1) return null;

  const match = matches[0];
  const extension = extensionByLanguage.get(match[1].trim().toLowerCase());
  if (extension === undefined) return null;

  const start = match.index ?? 0;
  const artifactStart = start + match[0].length;
  if (content.slice(artifactStart).includes("\n```")) return null;

  return {
    label: extension.toUpperCase(),
    before: content.slice(0, start).trim(),
    receivedBytes: utf8ByteLength(content.slice(artifactStart)),
  };
}

export function formatReceivedKB(bytes: number): string {
  const kb = bytes / 1024;
  const rounded = kb >= 10 ? Math.round(kb).toString() : kb.toFixed(1);
  return `${rounded} KB`;
}

export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb >= 10 ? Math.round(kb).toString() : kb.toFixed(1)} KB`;
  const mb = kb / 1024;
  return `${mb >= 10 ? Math.round(mb).toString() : mb.toFixed(1)} MB`;
}

export function markdownToPlainText(content: string): string {
  return content
    .replace(/\r\n/g, "\n")
    .replace(/^```[a-z0-9_-]*\n([\s\S]*?)\n```$/gim, "$1")
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/^\s{0,3}>\s?/gm, "")
    .replace(/^\s*[-*+]\s+/gm, "")
    .replace(/^\s*\d+\.\s+/gm, "")
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/(\*\*|__)(.*?)\1/g, "$2")
    .replace(/(\*|_)(.*?)\1/g, "$2")
    .replace(/~~(.*?)~~/g, "$1")
    .replace(/`([^`]+)`/g, "$1")
    .trim();
}

function utf8ByteLength(content: string): number {
  return new TextEncoder().encode(content).length;
}

function fencedArtifact(content: string): EmbeddedArtifact | null {
  const matches = [...content.matchAll(/(?:^|\n)```([a-z0-9_-]+)[ \t]*\n([\s\S]*?)\n```(?=\n|$)/gi)];
  const downloadable = matches.flatMap((match) => {
    const extension = extensionByLanguage.get(match[1].trim().toLowerCase());
    return extension === undefined ? [] : [{ match, extension }];
  });

  if (downloadable.length !== 1) return null;

  const { match, extension } = downloadable[0];
  const start = match.index ?? 0;
  return {
    artifact: {
      extension,
      label: extension.toUpperCase(),
      mimeType: DOWNLOAD_FORMATS[extension].mimeType,
      content: match[2],
    },
    before: content.slice(0, start).trim(),
    after: content.slice(start + match[0].length).trim(),
  };
}

type DownloadFormat = { mimeType: string; languages: string[]; mimeTypes: string[] };

const DOWNLOAD_FORMATS: Record<string, DownloadFormat> = {
  csv: { mimeType: "text/csv;charset=utf-8", languages: ["csv"], mimeTypes: ["text/csv"] },
  html: { mimeType: "text/html;charset=utf-8", languages: ["html"], mimeTypes: ["text/html"] },
  json: { mimeType: "application/json;charset=utf-8", languages: ["json"], mimeTypes: ["application/json"] },
  svg: { mimeType: "application/xml;charset=utf-8", languages: ["svg"], mimeTypes: ["image/svg+xml"] },
  xml: { mimeType: "application/xml;charset=utf-8", languages: ["xml"], mimeTypes: ["application/xml", "text/xml"] },
  pptx: {
    mimeType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
    languages: [],
    mimeTypes: ["application/vnd.openxmlformats-officedocument.presentationml.presentation"],
  },
  xlsx: {
    mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    languages: [],
    mimeTypes: ["application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"],
  },
};

const extensionByLanguage = new Map<string, string>(
  Object.entries(DOWNLOAD_FORMATS).flatMap(([extension, format]) =>
    format.languages.map((language) => [language, extension] as const),
  ),
);

const extensionByMimeType = new Map<string, string>(
  Object.entries(DOWNLOAD_FORMATS).flatMap(([extension, format]) =>
    format.mimeTypes.map((mimeType) => [mimeType, extension] as const),
  ),
);

function dataURLArtifact(content: string): DownloadableResponse | null {
  const match = content.trim().match(/^data:([^;,]+)(;base64)?,([\s\S]+)$/i);
  if (match === null) return null;
  const mimeType = match[1].toLowerCase();
  const extension = extensionByMimeType.get(mimeType);
  if (extension === undefined) return null;
  const encoded = match[3];
  let artifactContent: BlobPart;
  try {
    artifactContent = match[2]
      ? Uint8Array.from(atob(encoded), (character) => character.charCodeAt(0))
      : decodeURIComponent(encoded);
  } catch {
    return null;
  }
  return {
    extension,
    label: extension.toUpperCase(),
    mimeType,
    content: artifactContent,
  };
}
