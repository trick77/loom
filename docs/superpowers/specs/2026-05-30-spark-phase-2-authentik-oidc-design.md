# Slop Phase 2 Authentik OIDC Design

## Goal

Phase 2 adds real authentication and a usable authenticated frontend by delegating identity to
authentik via OpenID Connect. Slop owns only app-local sessions, role mapping, and app profile data;
authentik remains the source of truth for credentials, MFA, account lifecycle, and group membership.

## Scope

Included:

- OIDC authorization-code login against authentik.
- Server-side Slop session cookie after successful OIDC callback.
- Logout that clears the Slop session and redirects to a configured post-logout URL.
- Local `users` table keyed by the OIDC subject claim.
- Local `sessions` table keyed by opaque random session tokens.
- Admin role mapping from a configured authentik group claim.
- `/api/me` for the current authenticated user.
- Auth middleware for API routes that must be protected.
- Frontend signed-out screen, authenticated shell, user menu, logout, and basic admin user list.
- `slop.png` moved into the frontend asset tree and used as the main brand image.
- README setup instructions for configuring authentik and Slop.

Excluded:

- Local usernames and passwords.
- Local password reset or MFA.
- Public registration.
- Creating or deleting authentik users from Slop.
- Full admin settings UI beyond reading app-local users and their mapped roles.

## OIDC Model

Slop is an OIDC relying party. It discovers authentik metadata from `SLOP_OIDC_ISSUER`, redirects
users through authentik, exchanges the callback code for tokens, verifies the ID token, validates the
callback state and nonce, and extracts claims.

Required claims:

- `sub`: immutable external identity key.
- `preferred_username` or `email`: display/login identifier.

Optional claims:

- `name`: display name.
- `email`: contact identifier.
- `groups`: used for admin role mapping.

The admin group name is configured with `SLOP_OIDC_ADMIN_GROUP`. If a user's groups include that
value, Slop stores the user as `admin`; otherwise Slop stores the user as `user`. Role mapping is
refreshed on every login so authentik remains authoritative.

## Backend Components

`backend/internal/auth` owns OIDC and session behavior:

- `Provider`: discovers OIDC metadata and builds authorization, token-exchange, token-verification,
  and logout URLs.
- `StateStore`: creates and validates short-lived login state and nonce values using signed,
  httponly cookies.
- `SessionStore`: creates, looks up, refreshes, and revokes Slop sessions in SQLite.
- `UserStore`: upserts users by OIDC subject and lists users for admins.
- `Middleware`: attaches the authenticated user to request context and rejects unauthenticated or
  unauthorized requests.

`backend/internal/httpapi` exposes auth routes:

- `GET /api/auth/login`: starts OIDC login.
- `GET /api/auth/callback`: validates callback, upserts user, creates Slop session, redirects to `/`.
- `POST /api/auth/logout`: revokes the current session and returns the configured post-logout
  redirect URL.
- `GET /api/me`: returns the current user or `401`.
- `GET /api/admin/users`: returns app-local users; admin only.

Health endpoints remain public. Static SPA files remain public so the frontend can render the
signed-out screen. Future business APIs should use the auth middleware by default.

## Database

Add a new migration; do not edit `0001_init.sql`.

`users`:

- `id TEXT PRIMARY KEY`
- `oidc_subject TEXT NOT NULL UNIQUE`
- `username TEXT NOT NULL`
- `email TEXT NOT NULL DEFAULT ''`
- `display_name TEXT NOT NULL DEFAULT ''`
- `role TEXT NOT NULL CHECK (role IN ('admin', 'user'))`
- `response_language TEXT NOT NULL DEFAULT 'auto'`
- `created_at TEXT NOT NULL DEFAULT (datetime('now'))`
- `updated_at TEXT NOT NULL DEFAULT (datetime('now'))`
- `last_seen_at TEXT`

`sessions`:

- `token_hash TEXT PRIMARY KEY`
- `user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE`
- `created_at TEXT NOT NULL DEFAULT (datetime('now'))`
- `expires_at TEXT NOT NULL`
- `last_seen_at TEXT NOT NULL DEFAULT (datetime('now'))`

Only a hash of the random session token is stored. Session cookies are `HttpOnly`, `Secure` when
served over HTTPS, and `SameSite=Lax`.

## Frontend

The frontend starts by calling `/api/me`.

- `200`: render the authenticated app shell with current user state.
- `401`: render a signed-out Slop screen with a Sign in button linking to `/api/auth/login`.
- Other errors: render a compact service error state.

The authenticated shell keeps the current Phase 1 layout, adds Slop branding from
`frontend/src/assets/slop.png`, and adds a bottom user menu with Logout. Admin users see an Admin
view entry and a user list sourced from `/api/admin/users`.

The frontend does not collect passwords. All sign-in credentials are entered only in authentik.

## Configuration

New required configuration for Phase 2:

- `SLOP_PUBLIC_URL`
- `SLOP_OIDC_ISSUER`
- `SLOP_OIDC_CLIENT_ID`
- `SLOP_OIDC_CLIENT_SECRET`
- `SLOP_OIDC_REDIRECT_URL`
- `SLOP_OIDC_POST_LOGOUT_REDIRECT_URL`
- `SLOP_OIDC_ADMIN_GROUP`

`SLOP_SESSION_SECRET` remains required and signs transient auth state. `SLOP_ADMIN_INITIAL_PASSWORD`
is removed from the required boot path because authentik owns credentials.

## Security Notes

- Validate OIDC `state` and ID-token `nonce`.
- Verify ID token issuer, audience, expiry, and signature through `go-oidc`.
- Store only opaque Slop session cookies in the browser.
- Store only session token hashes in SQLite.
- Keep static SPA public but protect all non-health API routes that expose user data.
- Do not trust frontend-provided role or identity fields.
- Treat authentik group membership as authoritative on login.

## Testing

Backend tests cover:

- OIDC callback rejects invalid state and missing ID token.
- OIDC claims upsert a local user and map admin role from groups.
- Session creation stores only token hashes.
- `/api/me` returns `401` without a valid session and user JSON with one.
- Admin users can list users; regular users get `403`.

Frontend tests cover:

- Signed-out state renders Slop branding and sign-in action after `/api/me` returns `401`.
- Authenticated state renders the shell and current user.
- Admin users see the Admin view; regular users do not.
- Logout calls the backend and navigates to the returned redirect URL.

Manual smoke test:

1. Configure an authentik OAuth2/OpenID Connect provider for Slop.
2. Start Slop with OIDC env vars.
3. Visit Slop and sign in through authentik.
4. Confirm `/api/me` returns the mapped user.
5. Confirm an authentik group member appears as admin.
6. Confirm a non-group member appears as user and cannot call admin APIs.
7. Log out and confirm the next `/api/me` returns `401`.
