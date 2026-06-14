import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { WindowFileDrop } from "./WindowFileDrop";

function windowFileDrag(files: File[]) {
  return {
    dataTransfer: {
      types: ["Files"],
      files,
      dropEffect: "none",
    },
  };
}

test("shows a centered window overlay while files are dragged over the page", () => {
  render(
    <WindowFileDrop
      enabled
      onAttachFiles={() => undefined}
      onAttachError={() => undefined}
    />,
  );

  expect(screen.queryByText("Drop files here to add it to the conversation")).not.toBeInTheDocument();

  fireEvent.dragEnter(window, windowFileDrag([new File(["hello"], "notes.txt", { type: "text/plain" })]));

  expect(screen.getByText("\ue06d")).toBeInTheDocument();
  expect(screen.getByText("Drop files here to add it to the conversation")).toBeInTheDocument();
});

test("routes supported dropped files from the window and reports unsupported files", () => {
  const onAttachFiles = vi.fn();
  const onAttachError = vi.fn();
  const note = new File(["hello"], "notes.txt", { type: "text/plain" });
  const image = new File(["png"], "screenshot.png", { type: "image/png" });
  const unsupported = new File(["binary"], "installer.exe", { type: "application/octet-stream" });
  render(
    <WindowFileDrop
      enabled
      onAttachFiles={onAttachFiles}
      onAttachError={onAttachError}
    />,
  );

  fireEvent.drop(window, windowFileDrag([note, image, unsupported]));

  expect(onAttachFiles).toHaveBeenCalledWith([note, image]);
  expect(onAttachError).toHaveBeenCalledWith(
    "Unsupported file type. Use PDF, DOCX, PPTX, XLSX, TXT, MD, CSV, JSON, HTML, PNG, JPG, WEBP, or GIF.",
  );
});
