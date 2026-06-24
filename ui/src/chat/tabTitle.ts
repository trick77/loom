import type { Project, Thread } from "../api";
import type { RouteState } from "./routing";

/**
 * Browser-Tab-Titel für die aktuelle Ansicht. An jeden Titel wird " - Loom"
 * (einfacher Bindestrich) angehängt; nur wenn für die aktive Ansicht (noch) kein
 * Name feststeht, fällt der Titel auf "Loom" zurück.
 */
export function tabTitle(
  route: RouteState,
  activeThread: Thread | null,
  activeProject: Project | null,
): string {
  let base: string | null;
  switch (route.view) {
    case "new":
      base = "New thread";
      break;
    case "threads":
      base = "Recents";
      break;
    case "artifacts":
      base = "Artifacts";
      break;
    case "memory":
      base = "Memories";
      break;
    case "projects":
      base = "Projects";
      break;
    case "project":
      base = activeProject?.name ?? "Projects";
      break;
    case "thread":
      base = activeThread?.title ?? null;
      break;
    default: {
      // Erschöpfend: ein neuer RouteState-View erzwingt hier einen Compile-Fehler.
      const _exhaustive: never = route;
      return _exhaustive;
    }
  }
  return base !== null ? `${base} - Loom` : "Loom";
}
