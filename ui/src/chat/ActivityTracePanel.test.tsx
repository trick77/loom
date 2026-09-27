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
  // A running tool row carries no pill: the sweeping trace label already says
  // the turn is in flight.
  expect(within(trace).queryByText("Running")).toBeNull();

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

test("sweeps the working title before any reasoning has streamed", () => {
  render(
    <ActivityTracePanel
      active
      sweep
      events={[]}
      workingTitle="Checking whether 1001 is prime"
    />,
  );

  expect(screen.getByText("Checking whether 1001 is prime")).toHaveClass(
    "ui-thinking-label-active",
  );
});

test("a reasoning title replaces the working title", () => {
  render(
    <ActivityTracePanel
      active
      sweep
      workingTitle="Checking whether 1001 is prime"
      events={[
        {
          id: "reasoning-1",
          type: "reasoning",
          content: "Try small factors.",
          status: "running",
          title: "Factoring 1001 by small primes",
        },
      ]}
    />,
  );

  expect(screen.getByText("Factoring 1001 by small primes")).toHaveClass(
    "ui-thinking-label-active",
  );
  expect(screen.queryByText("Checking whether 1001 is prime")).toBeNull();
});
