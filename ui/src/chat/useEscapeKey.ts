import { useEffect, useRef } from "react";

// useEscapeKey gives Escape to the topmost surface only. Every dismissible
// surface (modal, lightbox, menu, the running answer) used to add its own
// window listener, so one keypress closed a lightbox AND stopped the answer
// behind it. Handlers form a stack in mount order; a keypress runs the last
// one registered and nothing else.
//
// options.active unregisters the handler without unmounting the caller;
// options.bottom registers below everything else, for a handler that must
// yield to any surface opened on top of it (stopping the stream).
export function useEscapeKey(
  handler: () => void,
  options: { active?: boolean; bottom?: boolean } = {},
): void {
  const { active = true, bottom = false } = options;
  const handlerRef = useRef(handler);
  handlerRef.current = handler;
  useEffect(() => {
    if (!active) return;
    const entry = () => handlerRef.current();
    if (bottom) stack.unshift(entry);
    else stack.push(entry);
    listen();
    return () => {
      const index = stack.lastIndexOf(entry);
      if (index >= 0) stack.splice(index, 1);
      if (stack.length === 0) unlisten();
    };
  }, [active, bottom]);
}

const stack: Array<() => void> = [];
let listening = false;

function onKeyDown(event: KeyboardEvent) {
  if (event.key !== "Escape" || stack.length === 0) return;
  event.preventDefault();
  stack[stack.length - 1]();
}

function listen() {
  if (listening) return;
  window.addEventListener("keydown", onKeyDown);
  listening = true;
}

function unlisten() {
  if (!listening) return;
  window.removeEventListener("keydown", onKeyDown);
  listening = false;
}
