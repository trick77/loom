import "@testing-library/jest-dom/vitest";
import { act, render, screen } from "@testing-library/react";
import { StrictMode } from "react";
import { expect, test, vi } from "vitest";

import { ProjectDialog } from "./ProjectDialog";

function Opener() {
  return <button type="button">Open</button>;
}

test("the dialog keeps focus on its name field and hands it back on close", async () => {
  // StrictMode runs effect cleanups once right after mount; a restore there
  // used to pull focus out of the name field into the opener.
  const { rerender } = render(
    <StrictMode>
      <Opener />
    </StrictMode>,
  );
  const opener = screen.getByRole("button", { name: "Open" });
  opener.focus();

  rerender(
    <StrictMode>
      <Opener />
      <ProjectDialog
        project={null}
        error=""
        disabled={false}
        onCancel={vi.fn()}
        onSubmit={vi.fn()}
      />
    </StrictMode>,
  );
  await act(async () => {});
  const dialog = screen.getByRole("dialog", { name: "Create a project" });
  expect(dialog).toContainElement(document.activeElement as HTMLElement);

  rerender(
    <StrictMode>
      <Opener />
    </StrictMode>,
  );
  await act(async () => {});
  expect(opener).toHaveFocus();
});
