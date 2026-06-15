# Projects Design

## Goal

Make Projects a first-class chat organization feature. A chat can belong to one project or to no project. Projects v1 organizes chats only; it does not add upload, files, memory, sharing, or project context panels.

## Scope

Projects v1 includes:

- A `/projects` page with search, recent-activity sorting, project cards, and a `New project` action.
- A `/projects/:projectID` detail page with a back link, project title, description, project actions menu, composer, and the chats that belong to that project.
- Project create, edit details, archive, and delete flows.
- Chat project membership management:
  - Add a non-project chat to a project.
  - Move selected chats from the `/chats` bulk-selection toolbar into a project.
  - Remove a project chat from its project.
  - Create a new chat inside a project.
  - Keep normal project-less chats fully supported.
- Project-scoped chat lists that reuse normal chat rows and normal chat behavior.

Projects v1 explicitly excludes:

- Upload UI.
- Files panels.
- Memory panels.
- Project context cards.
- `Example project` tags.
- Share buttons.
- Shared/public projects.

## Data Model

The existing `threads.project_id` nullable column is the source of truth for project membership.

- `project_id = NULL`: normal project-less chat.
- `project_id = <project id>`: chat belongs to that project.
- A chat can belong to at most one project.
- Moving a chat into a project updates `threads.project_id`.
- Removing a chat from a project sets `threads.project_id = NULL`.
- Deleting a project cascades through its project chats and project artifacts as already defined by backend cleanup behavior.

The implementation must not introduce a separate `project_chats` table or a separate project-chat entity.

## Backend

The current project CRUD API remains:

- `GET /api/projects`
- `POST /api/projects`
- `PATCH /api/projects/{projectID}`
- `POST /api/projects/{projectID}/archive`
- `POST /api/projects/{projectID}/unarchive`
- `DELETE /api/projects/{projectID}`

The thread API gains project membership updates through the existing thread update endpoint:

- `PATCH /api/threads/{threadID}` with `{ "projectId": "<project id>" }` moves the chat into a project.
- `PATCH /api/threads/{threadID}` with `{ "projectId": null }` removes the chat from a project.
- Omitting `projectId` leaves membership unchanged.
- Updating `title` and `projectId` in one request is allowed.

Validation:

- Moving into a project verifies the target project exists for the same `user_id`.
- Moving into another user's project returns `404`.
- Updating a missing thread returns `404`.
- Title validation stays unchanged.
- Every query remains scoped by `user_id`.

Project thread listing uses the existing list endpoint:

- `GET /api/threads?projectId=<project id>` lists active chats in one project.
- `GET /api/threads?projectId=null` lists project-less chats.
- Existing starred, archived, search, and limit filters continue to work.

## Frontend Routes

The SPA handles:

- `/projects`: project list.
- `/projects/:projectID`: project detail.
- Existing `/new`, `/chat/:threadID`, and `/chats` routes continue to work.

The sidebar `Projects` primary item navigates to `/projects`. The sidebar project list links directly to each project detail page. The collapsed sidebar keeps only the primary Projects icon, matching current sidebar behavior.

## Project List Page

The project list page matches the provided reference at the product level while using Lume's existing warm editorial tokens and popup styling.

Layout:

- Header row: `Projects`, sort control, `New project`.
- Search field: placeholder `Search projects...`.
- Project cards in a responsive grid.
- Each card shows project name, description when present, and updated date.
- Cards do not show example tags.
- Cards do not show share actions.
- Each card has a kebab menu.

Sort:

- v1 ships with `Recent activity`.
- The control may render as a single-option menu if that fits the existing component style, but no unsupported sort modes should be shown.

Project card menu:

- `Edit details`
- `Archive`
- `Delete`

## Project Detail Page

The project detail page is a chat workspace scoped to one project.

Layout:

- Back link: `All projects`.
- Header: project title, description, project actions menu.
- No `Example project` tag.
- No `Share` button.
- Composer: sending from here creates or continues a project-scoped chat.
- Chat list: active chats whose `projectId` equals the current project id.

When there is no selected chat in the project detail view:

- The composer starts a new thread with `projectId` set to the current project.
- The created thread is inserted into the project chat list before the assistant response streams.

When the user opens a project chat:

- The normal chat route and chat panel behavior are used.
- The thread's `projectId` stays visible in state so project menu actions can offer `Remove from project`.

## Project Modals

New/edit project modal:

- Title is `New project` or `Edit details`.
- Fields:
  - `Name`
  - `Description`
- Buttons:
  - `Cancel`
  - `Create` or `Save`
- Empty names are rejected client-side and server-side.
- Server validation errors render inside the modal.

Delete project modal:

- Title: `Delete project`.
- Warns that deleting a project permanently deletes the project, its chats, and generated artifacts for those chats.
- Buttons:
  - `Cancel`
  - `Delete`
- Delete uses the existing project delete endpoint.

Archive project:

- Archives the project through the existing endpoint.
- Archived projects disappear from the active project list.
- Archived project restore UI is out of scope for v1.

## Chat Menus

Project chat menu:

- `Star` or `Unstar`
- `Rename`
- `Remove from project`
- `Archive`
- `Delete`

Non-project chat menu:

- `Star` or `Unstar`
- `Rename`
- `Add to project`
- `Archive`
- `Delete`

`Add to project` opens a project picker. Selecting a project updates the chat's `projectId`, removes it from project-less lists when applicable, and adds it to the target project's list on the next load.

The `/chats` page `Move to project` button is active when one or more chats are selected. It opens the same project picker, applies the selected project to every selected chat, exits selection mode on success, and reloads the chats list so moved project chats disappear from project-less views when applicable.

`Remove from project` sets `projectId` to `null`, removes the row from the current project list, and keeps the chat available in normal Recents/Chats.

Archive and delete behavior remains chat behavior:

- `Archive` hides the chat from active lists.
- `Delete` requires confirmation and permanently deletes the chat and its artifacts.

## Error Handling

Page-level loading failures render compact errors consistent with existing Chats page styling.

Membership update failures:

- Keep the chat in its current visible list.
- Close transient menus only after a successful update.
- Show a clear page-level or modal-level error such as `Chat failed to move.`.

Project not found:

- `/projects/:projectID` shows a not-found state with a link back to `All projects`.

Session expiry:

- Reuses existing `AuthExpiredError` handling and returns the app to signed-out state.

## Testing

Backend tests:

- Moving a thread into a same-user project persists `project_id`.
- Moving a thread out of a project sets `project_id` to `NULL`.
- Moving a thread into another user's project returns not found.
- Updating title and project membership in one request works.
- Project-scoped listing includes only that project's active chats.
- Project-less listing excludes project chats.

Frontend tests:

- `/projects` loads and renders projects.
- Creating a project adds it to the list.
- Editing project details updates visible title and description.
- Project detail loads project chats via `projectId`.
- Sending from a project detail creates a thread with the current project id.
- Non-project chat menu shows `Add to project`.
- Selected chats on `/chats` can be moved through the `Move to project` toolbar button.
- Project chat menu shows `Remove from project`.
- Moving a chat into a project calls `PATCH /api/threads/{threadID}` with `projectId`.
- Removing a chat from a project calls `PATCH /api/threads/{threadID}` with `projectId: null`.
- No `Example project` tag or `Share` button renders on project pages.

Visual verification:

- Render `/projects` on desktop and mobile.
- Render `/projects/:projectID` on desktop and mobile.
- Verify project card menu, project detail menu, edit modal, delete modal, add-to-project picker, and remove-from-project state.

## Implementation Notes

Keep frontend files focused. Add a `ui/src/projects/` area for project list/detail components and project-specific hooks/helpers instead of growing `ui/src/chat/ChatShell.tsx` further. Do not create god components or god classes: `ChatShell` may coordinate routes and shared state, but project page rendering, project modals, picker UI, and reusable project mutation helpers must live in focused files. Reuse existing shared menu styling from `ui/src/ThreadActionsMenu.tsx` where practical so project menus align with chat menus.

Use the existing Lume design tokens and avoid introducing a new visual system. Buttons, menus, modal shapes, search fields, and row/card hover treatments should match the current Chats and sidebar surfaces.
