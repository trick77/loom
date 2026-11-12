export type RouteState =
  | { view: "new" }
  | { view: "threads" }
  | { view: "artifacts" }
  | { view: "memory" }
  // messageID opens the thread at that message (/thread/:id?message=:mid).
  | { view: "thread"; threadID: string; messageID?: string }
  | { view: "projects" }
  | { view: "project"; projectID: string }
  | { view: "schedules" }
  | { view: "schedule"; scheduleID: string }
  | { view: "ideas" }
  // The list with that idea expanded.
  | { view: "idea"; ideaID: string };

export function routeFromPath(path: string, search = ""): RouteState {
  if (path.startsWith("/thread/")) {
    const threadID = decodeURIComponent(path.slice("/thread/".length));
    const messageID = new URLSearchParams(search).get("message") ?? "";
    if (threadID !== "")
      return messageID === ""
        ? { view: "thread", threadID }
        : { view: "thread", threadID, messageID };
  }
  if (path === "/threads") return { view: "threads" };
  if (path === "/artifacts") return { view: "artifacts" };
  if (path === "/memory") return { view: "memory" };
  if (path === "/projects") return { view: "projects" };
  if (path.startsWith("/projects/")) {
    const projectID = decodeURIComponent(path.slice("/projects/".length));
    if (projectID !== "") return { view: "project", projectID };
  }
  if (path === "/schedules") return { view: "schedules" };
  if (path.startsWith("/schedules/")) {
    const scheduleID = decodeURIComponent(path.slice("/schedules/".length));
    if (scheduleID !== "") return { view: "schedule", scheduleID };
  }
  if (path === "/ideas") return { view: "ideas" };
  if (path.startsWith("/ideas/")) {
    const ideaID = decodeURIComponent(path.slice("/ideas/".length));
    if (ideaID !== "") return { view: "idea", ideaID };
  }
  return { view: "new" };
}

export function routeFromLocation(): RouteState {
  return routeFromPath(window.location.pathname, window.location.search);
}

export function pathForRoute(route: RouteState): string {
  if (route.view === "new") return "/new";
  if (route.view === "threads") return "/threads";
  if (route.view === "artifacts") return "/artifacts";
  if (route.view === "memory") return "/memory";
  if (route.view === "projects") return "/projects";
  if (route.view === "project")
    return `/projects/${encodeURIComponent(route.projectID)}`;
  if (route.view === "schedules") return "/schedules";
  if (route.view === "schedule")
    return `/schedules/${encodeURIComponent(route.scheduleID)}`;
  if (route.view === "ideas") return "/ideas";
  if (route.view === "idea")
    return `/ideas/${encodeURIComponent(route.ideaID)}`;
  const thread = `/thread/${encodeURIComponent(route.threadID)}`;
  return route.messageID === undefined
    ? thread
    : `${thread}?message=${encodeURIComponent(route.messageID)}`;
}

export function navigate(route: RouteState) {
  const path = pathForRoute(route);
  // Only a route that carries a query compares it: a foreign one (?utm=, a
  // callback's) must not turn a same-page navigation into a history entry.
  const current = path.includes("?")
    ? window.location.pathname + window.location.search
    : window.location.pathname;
  if (current !== path) {
    window.history.pushState({}, "", path);
  }
}
