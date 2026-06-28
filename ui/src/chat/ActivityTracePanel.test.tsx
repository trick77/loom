import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { ActivityTracePanel } from "./ActivityTracePanel";

test("renders generated tools with a creation label and artifact glyph", () => {
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
          summary: { kind: "generated", title: "Creating PDF file" },
        },
      ]}
    />,
  );

  const trace = screen.getByRole("status", { name: /loom activity trace/i });
  expect(within(trace).getByText("Creating PDF file")).toBeInTheDocument();
  expect(within(trace).getByText("Running")).toBeInTheDocument();

  const icon = trace.querySelector(".ui-activity-trace-icon-generated");
  expect(icon).not.toBeNull();
  expect(icon).toHaveTextContent("");
});

test("tool titles never sweep, even while running", () => {
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
          summary: { kind: "generated", title: "Creating PDF file" },
        },
      ]}
    />,
  );

  const title = screen.getByText("Creating PDF file");
  expect(title).not.toHaveClass("ui-thinking-label-active");
  expect(title).not.toHaveAttribute("data-text");
});
