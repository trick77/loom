import { afterEach, expect, test, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import App from "../App";

/**
 * The resizer wired into the real shell, rather than rendered on its own.
 *
 * The unit tests next door cover the drag itself. What they cannot see is the
 * coupling: the handle has to be mounted by ThreadShell, the shell's grid has to
 * read --ui-sidebar-w for its first column, and the handle has to disappear when
 * the sidebar collapses to its 56px rail. Those are three separate places that a
 * refactor can break without failing a single unit test.
 */
function signedInFetch() {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url === "/api/me")
      return Response.json({
        id: "u1",
        username: "jan",
        role: "user",
        displayName: "Jan",
      });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30")
      return Response.json({ items: [], nextCursor: null });
    throw new Error(`unexpected fetch ${url}`);
  });
}

afterEach(() => {
  cleanup();
  localStorage.clear();
  document.documentElement.style.removeProperty("--ui-sidebar-pref");
  vi.restoreAllMocks();
});

test("mounts the resizer in the shell and drives the grid from the variable", async () => {
  vi.stubGlobal("fetch", signedInFetch());
  render(<App />);
  await screen.findByRole("button", { name: /new thread/i });

  const handle = document.querySelector(".ui-sidebar-resizer");
  expect(handle).not.toBeNull();
  expect(handle).toHaveAttribute("role", "separator");

  // The shell's first column must come from the variable the handle writes, or
  // the drag would move a number nothing reads.
  const shell = screen.getByRole("complementary").parentElement;
  expect(shell?.className).toContain("md:grid-cols-[var(--ui-sidebar-w)_1fr]");
  // position:relative is what the absolutely positioned handle pins against.
  expect(shell?.className).toContain("relative");
});

test("paints the stored width on the shell's first paint", async () => {
  localStorage.setItem("loom:sidebar-width", "460");
  vi.stubGlobal("fetch", signedInFetch());
  render(<App />);
  await screen.findByRole("button", { name: /new thread/i });

  expect(
    document.documentElement.style.getPropertyValue("--ui-sidebar-pref"),
  ).toBe("460px");
});

test("takes the handle away when the sidebar collapses to its rail", async () => {
  vi.stubGlobal("fetch", signedInFetch());
  render(<App />);
  await screen.findByRole("button", { name: /new thread/i });
  expect(document.querySelector(".ui-sidebar-resizer")).not.toBeNull();

  // Collapsed the sidebar is a fixed 56px rail: there is no width to size, and a
  // handle parked over the thread list would only be in the way.
  fireEvent.click(screen.getByRole("button", { name: /hide sidebar/i }));
  await waitFor(() =>
    expect(document.querySelector(".ui-sidebar-resizer")).toBeNull(),
  );
});
