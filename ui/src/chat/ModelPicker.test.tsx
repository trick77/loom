import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { contextWindowFor } from "../useContextWindow";
import { ModelPicker } from "./ModelPicker";

const slots = [
  {
    key: "standard",
    model: "fast-model",
    displayName: "Fast Model",
    contextWindow: 200000,
  },
  {
    key: "deep",
    model: "deep-model",
    displayName: "Deep Model",
    contextWindow: 1000000,
  },
];

test("is a plain name with one slot", () => {
  render(
    <ModelPicker
      slots={slots.slice(0, 1)}
      value="standard"
      onChange={vi.fn()}
    />,
  );
  expect(screen.getByText("Fast Model")).toBeInTheDocument();
  expect(screen.queryByRole("button")).toBeNull();
});

test("is a plain name without a switch", () => {
  render(<ModelPicker slots={slots} value="deep" />);
  expect(screen.getByText("Deep Model")).toBeInTheDocument();
  expect(screen.queryByRole("button")).toBeNull();
});

test("falls back to the chat model's name before slots are known", () => {
  render(
    <ModelPicker
      slots={[]}
      value="standard"
      fallbackName="Old Backend Model"
    />,
  );
  expect(screen.getByText("Old Backend Model")).toBeInTheDocument();
});

test("opens a menu of the slots, each with its description, and switches", () => {
  const onChange = vi.fn();
  render(<ModelPicker slots={slots} value="standard" onChange={onChange} />);
  fireEvent.click(screen.getByRole("button", { name: /Fast Model/ }));

  const items = screen.getAllByRole("menuitemradio");
  expect(items).toHaveLength(2);
  expect(items[0]).toHaveAttribute("aria-checked", "true");
  expect(items[1]).toHaveTextContent("Deep Model");
  expect(items[1]).toHaveTextContent("Thinks longer, for hard problems");
  expect(items[0]).toHaveTextContent("Fast, for everyday questions");

  fireEvent.click(items[1]);
  expect(onChange).toHaveBeenCalledWith("deep");
  expect(screen.queryByRole("menu")).toBeNull();
});

test("picking the current slot again changes nothing; Escape closes", () => {
  const onChange = vi.fn();
  render(<ModelPicker slots={slots} value="deep" onChange={onChange} />);
  fireEvent.click(screen.getByRole("button", { name: /Deep Model/ }));
  fireEvent.click(screen.getAllByRole("menuitemradio")[1]);
  expect(onChange).not.toHaveBeenCalled();

  fireEvent.click(screen.getByRole("button", { name: /Deep Model/ }));
  fireEvent.keyDown(document, { key: "Escape" });
  expect(screen.queryByRole("menu")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: /Deep Model/ }));
  fireEvent.pointerDown(document.body);
  expect(screen.queryByRole("menu")).toBeNull();
});

test("a message's context window is its own model's", () => {
  const info = {
    model: "fast-model",
    displayName: "Fast Model",
    contextWindow: 200000,
    slots,
  };
  expect(contextWindowFor(info, "deep-model")).toBe(1000000);
  expect(contextWindowFor(info, "fast-model")).toBe(200000);
  expect(contextWindowFor(info, "retired-model")).toBe(200000);
  expect(contextWindowFor(info)).toBe(200000);
  expect(contextWindowFor(null, "deep-model")).toBeUndefined();
});
