import { render } from "@testing-library/react";
import { expect, test, vi } from "vitest";

const markdownRenders = vi.hoisted(() => ({ count: 0 }));
vi.mock("react-markdown", () => ({
  default: (props: { children?: string }) => {
    markdownRenders.count += 1;
    return <div>{props.children}</div>;
  },
}));

import { AssistantProse } from "./messages";

// The transcript re-renders on every streamed token; a settled message's prose
// must not run the markdown pipeline again when nothing about it changed.
test("AssistantProse does not re-run markdown for identical props", () => {
  const view = render(<AssistantProse>hello **world**</AssistantProse>);
  expect(markdownRenders.count).toBe(1);

  view.rerender(<AssistantProse>hello **world**</AssistantProse>);
  expect(markdownRenders.count).toBe(1);

  view.rerender(<AssistantProse>hello **there**</AssistantProse>);
  expect(markdownRenders.count).toBe(2);
});
