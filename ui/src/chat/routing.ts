export type RouteState =
  | { view: "new" }
  | { view: "threads" }
  | { view: "artifacts" }
  | { view: "memory" }
  | { view: "thread"; threadID: string }
  | { view: "projects" }
  | { view: "project"; projectID: string };

export function routeFromPath(path: string): RouteState {
  if (path.startsWith("/thread/")) {
    const threadID = decodeURIComponent(path.slice("/thread/".length));
    if (threadID !== "") return { view: "thread", threadID };
  }
  if (path === "/threads") return { view: "threads" };
  if (path === "/artifacts") return { view: "artifacts" };
  if (path === "/memory") return { view: "memory" };
  if (path === "/projects") return { view: "projects" };
  if (path.startsWith("/projects/")) {
    const projectID = decodeURIComponent(path.slice("/projects/".length));
    if (projectID !== "") return { view: "project", projectID };
  }
  return { view: "new" };
}

export function routeFromLocation(): RouteState {
  return routeFromPath(window.location.pathname);
}

export function pathForRoute(route: RouteState): string {
  if (route.view === "new") return "/new";
  if (route.view === "threads") return "/threads";
  if (route.view === "artifacts") return "/artifacts";
  if (route.view === "memory") return "/memory";
  if (route.view === "projects") return "/projects";
  if (route.view === "project") return `/projects/${encodeURIComponent(route.projectID)}`;
  return `/thread/${encodeURIComponent(route.threadID)}`;
}

export function navigate(route: RouteState) {
  const path = pathForRoute(route);
  if (window.location.pathname !== path) {
    window.history.pushState({}, "", path);
  }
}
