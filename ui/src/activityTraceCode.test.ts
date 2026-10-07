import { describe, expect, test } from "vitest";
import {
  normalizeActivityTrace,
  summarizeToolCall,
  upsertTraceToolCall,
  upsertTraceToolResult,
  type ActivityTraceEvent,
  type ActivityTraceToolEvent,
} from "./activityTrace";

const call = {
  id: "py1",
  name: "run_python",
  arguments: JSON.stringify({ code: "print('strawberry'.count('r'))" }),
};

function runWithResult(content: string): ActivityTraceToolEvent {
  let events: ActivityTraceEvent[] = upsertTraceToolCall([], call);
  events = upsertTraceToolResult(events, {
    id: "py1",
    name: "run_python",
    content,
  });
  return events[0] as ActivityTraceToolEvent;
}

describe("run_python trace rows", () => {
  test("summarizes the call as a code step carrying the code", () => {
    expect(summarizeToolCall(call.name, call.arguments)).toEqual({
      kind: "code",
      title: "Running Python",
      code: "print('strawberry'.count('r'))",
    });
    // A query-like argument must not turn it into a web search row.
    expect(
      summarizeToolCall("run_python", JSON.stringify({ code: "x", query: "y" }))
        .kind,
    ).toBe("code");
  });

  test("a clean run shows its stdout and is done", () => {
    const event = runWithResult(
      "exit_code: 0\nfile: created artifact chart.png (10 bytes), shown to the user\nstdout:\n3",
    );
    expect(event.status).toBe("done");
    expect(event.preview).toEqual({
      kind: "codeResult",
      exitCode: 0,
      output: "3",
    });
  });

  test("a non-zero exit is failed and shows the error tail", () => {
    const event = runWithResult(
      "exit_code: 1\nstderr (tail):\n| Traceback\n| KeyError: 'Umsatz'\nstdout:\n(nothing printed)",
    );
    expect(event.status).toBe("failed");
    expect(event.preview).toEqual({
      kind: "codeResult",
      exitCode: 1,
      output: "Traceback\nKeyError: 'Umsatz'",
    });
  });

  test("printed lines that look like markers stay in the output", () => {
    const event = runWithResult(
      "exit_code: 0\nstdout:\nfiles: 3\nfile: created artifact x\nstderr (tail):\nno",
    );
    expect(event.preview).toMatchObject({
      output: "files: 3\nfile: created artifact x\nstderr (tail):\nno",
    });
  });

  test("a stderr line reading 'stdout:' cannot move the split", () => {
    const event = runWithResult(
      "exit_code: 1\nstderr (tail):\n| stdout:\n| boom\nstdout:\nreal output",
    );
    expect(event.preview).toEqual({
      kind: "codeResult",
      exitCode: 1,
      output: "stdout:\nboom",
    });
    const ok = runWithResult(
      "exit_code: 0\nstderr (tail):\n| stdout:\n| warn\nstdout:\nreal output",
    );
    expect(ok.preview).toMatchObject({ output: "real output" });
  });

  test("a run that printed nothing shows no output, not the marker", () => {
    const event = runWithResult(
      "exit_code: 0\nfile: created artifact a.png (1 bytes), shown to the user\nstdout:\n(nothing printed)",
    );
    expect(event.status).toBe("done");
    expect(event.preview).toEqual({
      kind: "codeResult",
      exitCode: 0,
      output: "",
    });
  });

  test("another tool's output starting with exit_code is not a failure", () => {
    let events: ActivityTraceEvent[] = upsertTraceToolCall([], {
      id: "f1",
      name: "fetch__fetch",
      arguments: JSON.stringify({ url: "https://ci.example/log" }),
    });
    events = upsertTraceToolResult(events, {
      id: "f1",
      name: "fetch__fetch",
      content: "exit_code: 1\nbuild failed",
    });
    expect((events[0] as ActivityTraceToolEvent).status).toBe("done");
  });

  test("an infrastructure failure is failed with its message", () => {
    const event = runWithResult("tool failed: the Python sandbox is busy");
    expect(event.status).toBe("failed");
    expect(event.preview).toEqual({
      kind: "codeResult",
      output: "tool failed: the Python sandbox is busy",
    });
  });

  test("persisted traces re-derive the code summary and status", () => {
    const [event] = normalizeActivityTrace([
      {
        id: "py1",
        type: "tool",
        name: "run_python",
        status: "done",
        rawArguments: call.arguments,
        rawOutput:
          "exit_code: 137\ntimed out: the program was killed after 1m0s\nstdout:\n(nothing printed)",
      } as ActivityTraceToolEvent,
    ])!;
    const tool = event as ActivityTraceToolEvent;
    expect(tool.summary.kind).toBe("code");
    expect(tool.status).toBe("failed");
  });
});
