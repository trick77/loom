export type RouteState =
  | { view: "new" }
  | { view: "chats" }
  | { view: "artifacts" }
  | { view: "memory" }
  | { view: "chat"; threadID: string }
  | { view: "projects" }
  | { view: "project"; projectID: string };

export function routeFromPath(path: string): RouteState {
  if (path.startsWith("/chat/")) {
    const threadID = decodeURIComponent(path.slice("/chat/".length));
    if (threadID !== "") return { view: "chat", threadID };
  }
  if (path === "/chats") return { view: "chats" };
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
  if (route.view === "chats") return "/chats";
  if (route.view === "artifacts") return "/artifacts";
  if (route.view === "memory") return "/memory";
  if (route.view === "projects") return "/projects";
  if (route.view === "project") return `/projects/${encodeURIComponent(route.projectID)}`;
  return `/chat/${encodeURIComponent(route.threadID)}`;
}

export function navigate(route: RouteState) {
  const path = pathForRoute(route);
  if (window.location.pathname !== path) {
    window.history.pushState({}, "", path);
  }
}
