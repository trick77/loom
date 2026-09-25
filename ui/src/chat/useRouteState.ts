import { useCallback, useEffect, useRef, useState } from "react";

import { navigate, routeFromLocation, type RouteState } from "./routing";

// useRouteState owns the shell's route: the current route, a ref holding the
// latest value for async flows that must not act on a stale one, the popstate
// listener, and go(), which pushes a route to the history and into state as
// one step. The shell used to keep these apart and repeat the
// navigate-then-setRoute pair at every navigation.
export function useRouteState() {
  const [route, setRoute] = useState<RouteState>(() => routeFromLocation());
  const routeRef = useRef(route);
  useEffect(() => {
    routeRef.current = route;
  }, [route]);

  useEffect(() => {
    if (window.location.pathname === "/") {
      window.history.replaceState({}, "", "/new");
      setRoute({ view: "new" });
    }
    function handlePopState() {
      setRoute(routeFromLocation());
    }
    window.addEventListener("popstate", handlePopState);
    return () => {
      window.removeEventListener("popstate", handlePopState);
    };
  }, []);

  const go = useCallback((next: RouteState) => {
    navigate(next);
    setRoute(next);
  }, []);

  return { route, setRoute, routeRef, go };
}
