import type { Project, Thread } from "../api";
import i18n from "../i18n";
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
      base = i18n.t("common.newThread");
      break;
    case "threads":
      base = i18n.t("tabTitle.recents");
      break;
    case "artifacts":
      base = i18n.t("tabTitle.artifacts");
      break;
    case "memory":
      base = i18n.t("tabTitle.memories");
      break;
    case "projects":
      base = i18n.t("tabTitle.projects");
      break;
    case "project":
      base = activeProject?.name ?? i18n.t("tabTitle.projects");
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
