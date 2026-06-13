import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { ActivityTracePanel } from "./ActivityTracePanel";

test("renders generated tools with a creation label and feather glyph", () => {
  render(
    <ActivityTracePanel
      active
      initiallyExpanded
      events={[
        {
          id: "call_pdf",
          type: "tool",
          name: "create_pdf_file",
          status: "running",
          summary: { kind: "generated", title: "Creating PDF file", label: "PDF file" },
        },
      ]}
    />,
  );

  const trace = screen.getByRole("status", { name: /slopr activity trace/i });
  expect(within(trace).getByText("Creating PDF file")).toBeInTheDocument();
  expect(within(trace).getByText("Running")).toBeInTheDocument();

  const icon = trace.querySelector(".ui-activity-trace-icon-generated");
  expect(icon).not.toBeNull();
  expect(icon).toHaveTextContent("\ue0ed");
});

test("sweeps generated tool title text only while running", () => {
  const { rerender } = render(
    <ActivityTracePanel
      active
      initiallyExpanded
      events={[
        {
          id: "call_pdf",
          type: "tool",
          name: "create_pdf_file",
          status: "running",
          summary: { kind: "generated", title: "Creating PDF file", label: "PDF file" },
        },
      ]}
    />,
  );

  expect(screen.getByText("Creating PDF file")).toHaveClass("ui-thinking-label-active");
  expect(screen.getByText("Creating PDF file")).toHaveAttribute("data-text", "Creating PDF file");
  expect(document.querySelector(".ui-activity-trace-icon-generated")).not.toHaveClass(
    "ui-thinking-label-active",
  );

  rerender(
    <ActivityTracePanel
      active
      initiallyExpanded
      events={[
        {
          id: "call_pdf",
          type: "tool",
          name: "create_pdf_file",
          status: "done",
          summary: { kind: "generated", title: "Creating PDF file", label: "PDF file" },
        },
      ]}
    />,
  );

  expect(screen.getByText("Creating PDF file")).not.toHaveClass("ui-thinking-label-active");
});
