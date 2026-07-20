import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import "../i18n";
import type { Thread } from "../api";
import { SearchModal } from "./SearchModal";
import type { SearchResult } from "../search/useThreadSearch";
import { useThreadSearch } from "../search/useThreadSearch";

vi.mock("../search/useThreadSearch", () => ({ useThreadSearch: vi.fn() }));

const useThreadSearchMock = vi.mocked(useThreadSearch);

function thread(id: string, title: string): Thread {
  return {
    id,
    title,
    starred: false,
    createdAt: "2026-07-01T00:00:00Z",
    updatedAt: "2026-07-01T00:00:00Z",
    lastMessageAt: "2026-07-01T00:00:00Z",
  };
}

function setResults(results: SearchResult[]) {
  useThreadSearchMock.mockReturnValue({ results, titleLoading: false, contentLoading: false });
}

// The rows are the only <li> elements in the modal, in result order.
function rowButtons(): HTMLElement[] {
  return screen.getAllByRole("listitem").map((li) => within(li).getByRole("button"));
}

const SELECTED_CLASS = "bg-[#3f3f3a]";

beforeEach(() => {
  useThreadSearchMock.mockReset();
  setResults([]);
});

describe("SearchModal", () => {
  it("renders a dialog with a focused search input", () => {
    render(<SearchModal onClose={vi.fn()} onSelectThread={vi.fn()} />);

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    const input = screen.getByRole("textbox", { name: "Search chats" });
    expect(input).toHaveFocus();
    expect(input).toHaveAttribute("placeholder", "Search chats…");
  });

  it("shows the recents empty state, then the no-match state once a query is typed", () => {
    render(<SearchModal onClose={vi.fn()} onSelectThread={vi.fn()} />);

    expect(screen.getByText("No chats yet.")).toBeInTheDocument();
    expect(screen.queryByText("Search results")).not.toBeInTheDocument();

    fireEvent.change(screen.getByRole("textbox", { name: "Search chats" }), {
      target: { value: "kayak" },
    });

    expect(screen.getByText("Search results")).toBeInTheDocument();
    expect(screen.getByText("No chats match your search.")).toBeInTheDocument();
    expect(screen.queryByText("No chats yet.")).not.toBeInTheDocument();
  });

  it("passes the typed query to the search hook", () => {
    render(<SearchModal onClose={vi.fn()} onSelectThread={vi.fn()} />);

    fireEvent.change(screen.getByRole("textbox", { name: "Search chats" }), {
      target: { value: "kayak" },
    });

    expect(useThreadSearchMock).toHaveBeenLastCalledWith("kayak");
  });

  it("renders one row per result and selects the first by default", () => {
    setResults([{ thread: thread("t1", "Alpha") }, { thread: thread("t2", "Beta") }]);
    render(<SearchModal onClose={vi.fn()} onSelectThread={vi.fn()} />);

    const rows = rowButtons();
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Alpha");
    expect(rows[1]).toHaveTextContent("Beta");
    expect(rows[0]).toHaveClass(SELECTED_CLASS);
    expect(rows[1]).not.toHaveClass(SELECTED_CLASS);
  });

  it("renders a snippet only when the result carries one", () => {
    setResults([
      { thread: thread("t1", "Alpha"), snippet: "packed a «kayak» for the trip" },
      { thread: thread("t2", "Beta"), snippet: "" },
    ]);
    render(<SearchModal onClose={vi.fn()} onSelectThread={vi.fn()} />);

    expect(screen.getByText(/packed a/)).toBeInTheDocument();
    const rows = rowButtons();
    expect(rows[1]).toHaveTextContent("Beta");
  });

  it("highlights matching terms in titles only while a query is present", () => {
    setResults([{ thread: thread("t1", "Kayak trip") }]);
    const { container } = render(<SearchModal onClose={vi.fn()} onSelectThread={vi.fn()} />);

    expect(container.querySelector("strong")).toBeNull();

    fireEvent.change(screen.getByRole("textbox", { name: "Search chats" }), {
      target: { value: "kayak" },
    });

    expect(container.querySelector("strong")).toHaveTextContent("Kayak");
  });

  it("moves the selection down with ArrowDown and wraps past the last row", () => {
    setResults([{ thread: thread("t1", "Alpha") }, { thread: thread("t2", "Beta") }]);
    render(<SearchModal onClose={vi.fn()} onSelectThread={vi.fn()} />);
    const input = screen.getByRole("textbox", { name: "Search chats" });

    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(rowButtons()[1]).toHaveClass(SELECTED_CLASS);
    expect(rowButtons()[0]).not.toHaveClass(SELECTED_CLASS);

    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(rowButtons()[0]).toHaveClass(SELECTED_CLASS);
  });

  it("moves the selection up with ArrowUp and wraps to the last row", () => {
    setResults([{ thread: thread("t1", "Alpha") }, { thread: thread("t2", "Beta") }]);
    render(<SearchModal onClose={vi.fn()} onSelectThread={vi.fn()} />);

    fireEvent.keyDown(screen.getByRole("textbox", { name: "Search chats" }), { key: "ArrowUp" });

    expect(rowButtons()[1]).toHaveClass(SELECTED_CLASS);
  });

  it("keeps the selection at zero when arrow keys are pressed with no results", () => {
    render(<SearchModal onClose={vi.fn()} onSelectThread={vi.fn()} />);
    const input = screen.getByRole("textbox", { name: "Search chats" });

    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "ArrowUp" });

    expect(screen.queryAllByRole("listitem")).toHaveLength(0);
  });

  it("moves the selection to the hovered row", () => {
    setResults([{ thread: thread("t1", "Alpha") }, { thread: thread("t2", "Beta") }]);
    render(<SearchModal onClose={vi.fn()} onSelectThread={vi.fn()} />);

    fireEvent.mouseMove(rowButtons()[1]);

    expect(rowButtons()[1]).toHaveClass(SELECTED_CLASS);
  });

  it("opens the selected thread on Enter and closes", () => {
    setResults([{ thread: thread("t1", "Alpha") }, { thread: thread("t2", "Beta") }]);
    const onClose = vi.fn();
    const onSelectThread = vi.fn();
    render(<SearchModal onClose={onClose} onSelectThread={onSelectThread} />);
    const input = screen.getByRole("textbox", { name: "Search chats" });

    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(onSelectThread).toHaveBeenCalledWith("t2");
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("ignores Enter when there is nothing to open", () => {
    const onClose = vi.fn();
    const onSelectThread = vi.fn();
    render(<SearchModal onClose={onClose} onSelectThread={onSelectThread} />);

    fireEvent.keyDown(screen.getByRole("textbox", { name: "Search chats" }), { key: "Enter" });

    expect(onSelectThread).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("opens a thread when its row is clicked", () => {
    setResults([{ thread: thread("t1", "Alpha") }, { thread: thread("t2", "Beta") }]);
    const onClose = vi.fn();
    const onSelectThread = vi.fn();
    render(<SearchModal onClose={onClose} onSelectThread={onSelectThread} />);

    fireEvent.click(rowButtons()[1]);

    expect(onSelectThread).toHaveBeenCalledWith("t2");
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("closes on Escape", () => {
    const onClose = vi.fn();
    render(<SearchModal onClose={onClose} onSelectThread={vi.fn()} />);

    fireEvent.keyDown(screen.getByRole("textbox", { name: "Search chats" }), { key: "Escape" });

    expect(onClose).toHaveBeenCalledOnce();
  });

  it("ignores unhandled keys", () => {
    const onClose = vi.fn();
    render(<SearchModal onClose={onClose} onSelectThread={vi.fn()} />);

    fireEvent.keyDown(screen.getByRole("textbox", { name: "Search chats" }), { key: "a" });

    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes on the close button", () => {
    const onClose = vi.fn();
    render(<SearchModal onClose={onClose} onSelectThread={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    expect(onClose).toHaveBeenCalledOnce();
  });

  it("closes on a backdrop click but not on a click inside the dialog", () => {
    const onClose = vi.fn();
    const { container } = render(<SearchModal onClose={onClose} onSelectThread={vi.fn()} />);

    fireEvent.click(screen.getByRole("dialog"));
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.click(container.firstElementChild!);
    expect(onClose).toHaveBeenCalledOnce();
  });
});
