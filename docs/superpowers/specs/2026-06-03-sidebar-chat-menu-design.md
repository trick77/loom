# Sidebar Chat Menu Design

## Scope

Add per-chat actions to sidebar thread rows. Long inactive chat titles keep normal text ellipsis. The active row uses a right-edge text fade and a vertical menu button that opens chat actions. Move starring into that menu, add rename and delete flows, render a future "Add to project" entry without behavior, and move the MCP status indicator into the chat header.

## Sidebar Row Visuals

- Inactive thread rows keep `overflow: hidden`, `white-space: nowrap`, and `text-overflow: ellipsis`.
- The active thread row changes to a flex row with:
  - a title area that fades out over the final letters instead of showing an ellipsis;
  - a 24px vertical-ellipsis button at the far right;
  - an active background slightly brighter than the current selected sidebar row.
- The fade should cover about the last 3-4 letters and match the active row background.
- The menu button must not change the row height or push the row wider than the sidebar.

## Action Menu Visuals

The dropdown opens below the active row, shifted toward the right edge but clamped so it never crosses the sidebar divider. It has rounded corners, one consistent background through the full rounded shape, and menu entries with the selected-sidebar-entry color treatment.

Entries:

1. Star or Unstar, depending on current thread state.
2. Rename.
3. Add to project.
4. Short inset divider, brighter than the menu background.
5. Delete, using a muted red text color.

Icon rules:

- Add to project uses the same project/folder icon style as the sidebar Projects item.
- Delete uses a trashcan icon, sized slightly larger than the other menu icons for legibility.
- Delete remains red but not blood red.

## Menu Behavior

- Opening a menu is local UI state. Only one thread menu can be open at a time.
- Clicking outside the menu closes it.
- Pressing Escape closes the menu.
- Selecting Star/Unstar calls the existing star/unstar API and updates both the active thread and sidebar thread list.
- Selecting Rename closes the menu and opens the rename modal.
- Add to project renders as an inert menu entry for now. It must not call an API, open another UI, or change state. Project assignment is out of scope for this phase.
- Selecting Delete closes the menu and opens the delete confirmation modal.

## Rename Modal

Rename opens a centered modal over a dimmed app background.

Visual details:

- Title: "Rename chat", 22px, semibold.
- Body: one text input.
- Buttons: Cancel and Save, right-aligned below the input.
- The input focuses on open and selects the full current chat title.

Behavior:

- Cancel closes the modal without changes.
- Escape closes the modal without changes.
- Save trims the title and calls `PATCH /api/threads/{threadID}` with the new title.
- Empty titles are not submitted.
- On success, update `activeThread` when applicable and replace the thread in the sidebar list.
- On failure, keep the modal open and show an error near the modal or reuse the existing sidebar/chat error pattern.

## Delete Modal

Delete opens a centered confirmation modal over a dimmed app background.

Visual details:

- Title: "Delete chat".
- Text: "Are you sure you want to delete this chat?"
- Buttons: Cancel and Delete, right-aligned.
- Delete button background uses the locked muted red tone from the mockup, `#c9534b`, with light text.

Behavior:

- Cancel closes the modal without changes.
- Escape closes the modal without changes.
- Delete calls `DELETE /api/threads/{threadID}`.
- On success, remove the thread from the sidebar list and clear it from the starred list implicitly through state.
- If the deleted thread is active, navigate to `/new`, clear active thread/messages/streaming state, and abort any in-flight stream.
- On failure, keep the modal open and show an error.

## Header MCP Status

Remove the current top-right chat header Star/Unstar text button. Starring is available only from the sidebar action menu.

Move the MCP status indicator from the bottom sidebar account area into the freed top-right chat header slot. It keeps the same semantics:

- show only when status is loaded and configured MCP server count is greater than zero;
- use success styling when all configured servers are active;
- use danger styling otherwise;
- preserve the tooltip showing active/configured counts.

The bottom sidebar account area no longer renders the MCP status indicator.

## API Surface

Use existing backend endpoints:

- `POST /api/threads/{threadID}/star`
- `POST /api/threads/{threadID}/unstar`
- `PATCH /api/threads/{threadID}` for rename
- `DELETE /api/threads/{threadID}` for delete

Frontend API helpers should be added for rename/update and delete if missing. No backend migration or new route is expected.

## Accessibility

- The menu button has an accessible label such as "Open chat actions".
- The menu uses button elements for entries.
- Modal controls are keyboard reachable.
- Escape closes open menu or modal.
- Rename input auto-focuses and selects the full value on open.
- Delete requires explicit confirmation through the modal.

## Tests

Frontend tests should cover:

- inactive long titles still use truncation styling;
- active row renders the vertical menu button and opens the dropdown;
- Star/Unstar menu action calls the existing API and updates UI state;
- Rename opens the modal, selects the current title, saves through `PATCH /api/threads/{id}`, and updates UI state;
- Add to project renders but does not call an API;
- Delete opens confirmation, calls `DELETE /api/threads/{id}`, removes the thread, and navigates to `/new` when deleting the active thread;
- MCP status indicator renders in the chat header and no longer renders in the bottom sidebar account area.

Backend tests are not required unless implementation discovers the existing update/delete handlers are insufficient.
