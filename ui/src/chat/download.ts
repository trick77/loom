// downloadBlob hands a blob to the browser as a file download through a
// temporary anchor. The object URL is revoked on the next tick: revoking
// synchronously after click() cancels the download in Safari and Firefox,
// which resolve the URL after the handler returns.
export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 0);
}
