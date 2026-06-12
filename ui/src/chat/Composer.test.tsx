import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { Composer } from "./Composer";

const fileDrag = {
  dataTransfer: {
    types: ["Files"],
    files: [new File(["hello"], "notes.txt", { type: "text/plain" })],
    dropEffect: "none",
  },
};

test("shows drop guidance while files are dragged over the composer", () => {
  render(
    <Composer
      variant="chat"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={vi.fn()}
    />,
  );

  const composer = screen.getByRole("textbox").closest("form");
  expect(composer).not.toBeNull();
  expect(screen.queryByText("Drop files here to add to chat")).not.toBeInTheDocument();

  fireEvent.dragEnter(composer!, fileDrag);

  expect(screen.getByText("Drop files here to add to chat")).toBeInTheDocument();

  fireEvent.dragLeave(composer!, fileDrag);

  expect(screen.queryByText("Drop files here to add to chat")).not.toBeInTheDocument();
});

test("ignores non-file drags for composer drop guidance", () => {
  render(
    <Composer
      variant="start"
      draft=""
      isSending={false}
      placeholder="How can I help you today?"
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={vi.fn()}
    />,
  );

  const composer = screen.getByRole("textbox").closest("form");
  expect(composer).not.toBeNull();

  fireEvent.dragEnter(composer!, {
    dataTransfer: {
      types: ["text/plain"],
      files: [],
      dropEffect: "none",
    },
  });

  expect(screen.queryByText("Drop files here to add to chat")).not.toBeInTheDocument();
});
