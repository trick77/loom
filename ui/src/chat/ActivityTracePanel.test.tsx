import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { ActivityTracePanel, fencedPython } from "./ActivityTracePanel";

test("renders a run_python step with its output and the code on request", () => {
  render(
    <ActivityTracePanel
      active
      initiallyExpanded
      events={[
        {
          id: "py1",
          type: "tool",
          name: "run_python",
          status: "failed",
          summary: {
            kind: "code",
            title: "Running Python",
            code: "print(df['Umsatz'].sum())",
          },
          preview: {
            kind: "codeResult",
            exitCode: 1,
            output: "KeyError: 'Umsatz'",
          },
        },
      ]}
    />,
  );

  const trace = screen.getByRole("status", { name: /loom activity trace/i });
  expect(within(trace).getByText("Running Python")).toBeInTheDocument();
  expect(within(trace).getByText("KeyError: 'Umsatz'")).toBeInTheDocument();
  expect(within(trace).getByText("Failed")).toBeInTheDocument();
  expect(within(trace).queryByText(/df\['Umsatz'\]/)).toBeNull();

  const toggle = within(trace).getByRole("button", { name: "Show code" });
  fireEvent.click(toggle);
  expect(trace.querySelector(".ui-activity-code code")).toHaveTextContent(
    "print(df['Umsatz'].sum())",
  );
  fireEvent.click(within(trace).getByRole("button", { name: "Hide code" }));
  expect(trace.querySelector(".ui-activity-code")).toBeNull();
});

test("fencedPython outlasts backtick runs inside the code", () => {
  expect(fencedPython("x = 1")).toBe("```python\nx = 1\n```");
  expect(fencedPython('s = "````"')).toBe('`````python\ns = "````"\n`````');
});

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
