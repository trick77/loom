import { act, renderHook } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";

import { useRouteState } from "./useRouteState";

afterEach(() => {
  window.history.replaceState({}, "", "/");
});

test("the root path is rewritten to /new", () => {
  window.history.replaceState({}, "", "/");
  const { result } = renderHook(() => useRouteState());
  expect(result.current.route).toEqual({ view: "new" });
  expect(window.location.pathname).toBe("/new");
});

test("go pushes the path and updates the route and its ref together", () => {
  window.history.replaceState({}, "", "/new");
  const { result } = renderHook(() => useRouteState());

  act(() => {
    result.current.go({ view: "thread", threadID: "t1" });
  });

  expect(window.location.pathname).toBe("/thread/t1");
  expect(result.current.route).toEqual({ view: "thread", threadID: "t1" });
  expect(result.current.routeRef.current).toEqual({
    view: "thread",
    threadID: "t1",
  });
});

test("a popstate re-reads the location", () => {
  window.history.replaceState({}, "", "/new");
  const { result } = renderHook(() => useRouteState());

  act(() => {
    window.history.pushState({}, "", "/projects");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });

  expect(result.current.route).toEqual({ view: "projects" });
});

test("the ref holds the new route as soon as go returns, before a render", () => {
  window.history.replaceState({}, "", "/new");
  const { result } = renderHook(() => useRouteState());
  const { go, routeRef } = result.current;

  // No act: nothing renders in between, as for a stream event landing between
  // the navigation and the commit.
  go({ view: "project", projectID: "p2" });
  expect(routeRef.current).toEqual({ view: "project", projectID: "p2" });

  window.history.pushState({}, "", "/projects");
  window.dispatchEvent(new PopStateEvent("popstate"));
  expect(routeRef.current).toEqual({ view: "projects" });
});
