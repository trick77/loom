import { describe, expect, it } from "vitest";

import { DOCUMENT_MAX_UPLOAD_BYTES } from "../api";
import {
  acceptAttachmentFiles,
  clipboardImageFiles,
  toSupportedImageFile,
} from "./attachmentFiles";

describe("acceptAttachmentFiles", () => {
  const small = (name: string) => new File(["x"], name);
  const large = (name: string) => {
    const file = new File(["x"], name);
    Object.defineProperty(file, "size", {
      value: DOCUMENT_MAX_UPLOAD_BYTES + 1,
    });
    return file;
  };
  const limit = "You can attach up to 5 files per message.";
  const tooLarge = "Files must be 25 MB or smaller.";

  it("takes every file that fits, with no note", () => {
    const files = [small("a.txt"), small("b.txt")];
    expect(acceptAttachmentFiles(files, 0)).toEqual({
      accepted: files,
      note: null,
    });
  });

  it("drops a file over the size limit", () => {
    const ok = small("a.txt");
    expect(acceptAttachmentFiles([ok, large("b.pdf")], 0)).toEqual({
      accepted: [ok],
      note: tooLarge,
    });
  });

  it("cuts at the per-message count, the limit note over the size one", () => {
    const files = [small("a.txt"), small("b.txt"), large("c.pdf")];
    expect(acceptAttachmentFiles(files, 4)).toEqual({
      accepted: [files[0]],
      note: limit,
    });
  });

  it("takes nothing once the composer is full", () => {
    expect(acceptAttachmentFiles([small("a.txt")], 5)).toEqual({
      accepted: [],
      note: limit,
    });
  });
});

describe("toSupportedImageFile", () => {
  it("synthesizes a filename for a nameless PNG", () => {
    const out = toSupportedImageFile(
      new File([new Uint8Array([1])], "", { type: "image/png" }),
    );
    expect(out?.name).toBe("pasted-image.png");
    expect(out?.type).toBe("image/png");
  });

  it("maps image/jpeg to a .jpg name", () => {
    const out = toSupportedImageFile(
      new File([], "image", { type: "image/jpeg" }),
    );
    expect(out?.name).toBe("pasted-image.jpg");
  });

  it("keeps a file that already has a supported extension", () => {
    const file = new File([], "shot.jpg", { type: "image/jpeg" });
    expect(toSupportedImageFile(file)).toBe(file);
  });

  it("returns null for an unsupported MIME type", () => {
    expect(
      toSupportedImageFile(new File([], "x.bmp", { type: "image/bmp" })),
    ).toBeNull();
  });
});

describe("clipboardImageFiles", () => {
  it("reads image files from clipboardData.files", () => {
    const png = new File([], "a.png", { type: "image/png" });
    const txt = new File([], "note.txt", { type: "text/plain" });
    const data = { files: [png, txt], items: [] } as unknown as DataTransfer;
    expect(clipboardImageFiles(data)).toEqual([png]);
  });

  it("falls back to clipboardData.items when files is empty", () => {
    const png = new File([], "a.png", { type: "image/png" });
    const data = {
      files: [],
      items: [
        { kind: "string", type: "text/plain", getAsFile: () => null },
        { kind: "file", type: "image/png", getAsFile: () => png },
      ],
    } as unknown as DataTransfer;
    expect(clipboardImageFiles(data)).toEqual([png]);
  });

  it("returns an empty array when there are no images", () => {
    const data = { files: [], items: [] } as unknown as DataTransfer;
    expect(clipboardImageFiles(data)).toEqual([]);
  });
});
