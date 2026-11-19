import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { useComposerAttachments } from "./useComposerAttachments";
import type { ComposerAttachment } from "./useDocumentAttachments";

vi.mock("../api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api")>();
  return {
    ...actual,
    deleteArtifact: vi.fn(),
    deleteDocument: vi.fn(),
    uploadDocument: vi.fn(),
    uploadImageAttachment: vi.fn(),
  };
});

function file(name: string): File {
  return new File(["hello"], name, { type: "text/plain" });
}

type Send = (attachments: ComposerAttachment[]) => Promise<boolean>;

function setup(onSend: Send) {
  return renderHook(({ send }) => useComposerAttachments({}, send), {
    initialProps: { send: onSend },
  });
}

test("chips the send did not take come back", async () => {
  const onSend = vi.fn<Send>(async () => false);
  const { result } = setup(onSend);
  act(() => result.current.handleAttachFiles([file("report.txt")]));

  act(() => result.current.handleSendRequest());
  expect(onSend).toHaveBeenCalledWith([
    expect.objectContaining({ filename: "report.txt" }),
  ]);
  await waitFor(() =>
    expect(result.current.attachments.map((a) => a.filename)).toEqual([
      "report.txt",
    ]),
  );
});

test("chips the send took are gone", async () => {
  const onSend = vi.fn<Send>(async () => true);
  const { result } = setup(onSend);
  act(() => result.current.handleAttachFiles([file("report.txt")]));

  await act(async () => result.current.handleSendRequest());
  expect(result.current.attachments).toEqual([]);
});

test("chips that come back keep a file picked meanwhile", async () => {
  let answer: (taken: boolean) => void = () => {};
  const onSend = vi.fn<Send>(
    () =>
      new Promise((resolve) => {
        answer = resolve;
      }),
  );
  const { result } = setup(onSend);
  act(() => result.current.handleAttachFiles([file("report.txt")]));
  act(() => result.current.handleSendRequest());
  expect(result.current.attachments).toEqual([]);

  act(() => result.current.handleAttachFiles([file("later.txt")]));
  await act(async () => answer(false));
  expect(result.current.attachments.map((a) => a.filename)).toEqual([
    "report.txt",
    "later.txt",
  ]);
});
