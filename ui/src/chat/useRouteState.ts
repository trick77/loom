import { useCallback, useEffect, useRef, useState } from "react";

import {
  navigate,
  pathForRoute,
  routeFromLocation,
  type RouteState,
} from "./routing";

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
      // eslint-disable-next-line react-hooks/set-state-in-effect -- the / redirect on mount
      setRoute({ view: "new" });
    }
    function handlePopState() {
      const next = routeFromLocation();
      routeRef.current = next;
      setRoute(next);
    }
    window.addEventListener("popstate", handlePopState);
    return () => {
      window.removeEventListener("popstate", handlePopState);
    };
  }, []);

  // The ref moves with the navigation, not a render later: a stream event
  // landing in between must already see where the user went.
  const go = useCallback((next: RouteState) => {
    navigate(next);
    routeRef.current = next;
    setRoute(next);
  }, []);

  // replace swaps the current history entry instead of adding one.
  const replace = useCallback((next: RouteState) => {
    window.history.replaceState({}, "", pathForRoute(next));
    routeRef.current = next;
    setRoute(next);
  }, []);

  return { route, routeRef, go, replace };
}
