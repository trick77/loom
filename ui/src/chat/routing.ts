export type RouteState =
  | { view: "new" }
  | { view: "chats" }
  | { view: "library" }
  | { view: "chat"; threadID: string };

export function routeFromLocation(): RouteState {
  const path = window.location.pathname;
  if (path.startsWith("/chat/")) {
    const threadID = decodeURIComponent(path.slice("/chat/".length));
    if (threadID !== "") return { view: "chat", threadID };
  }
  if (path === "/chats") return { view: "chats" };
  if (path === "/library") return { view: "library" };
  return { view: "new" };
}

export function pathForRoute(route: RouteState): string {
  if (route.view === "new") return "/new";
  if (route.view === "chats") return "/chats";
  if (route.view === "library") return "/library";
  return `/chat/${encodeURIComponent(route.threadID)}`;
}

export function navigate(route: RouteState) {
  const path = pathForRoute(route);
  if (window.location.pathname !== path) {
    window.history.pushState({}, "", path);
  }
}
