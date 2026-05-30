# spark

Self-hosted, multi-user LLM chat app with a Go backend, embedded React frontend, SQLite, SSE, and
authentik-backed authentication.

## Development

Required for local backend startup:

- Go 1.25
- Node.js/npm for frontend builds
- `SPARK_SESSION_SECRET`
- OIDC configuration for authentik once Phase 2 is enabled

Common commands:

```bash
make test
make fe-test
make fe-build
make build
```

## Authentik OIDC Setup

Spark delegates sign-in to authentik through OpenID Connect. Authentik owns credentials, MFA, user
lifecycle, and group membership. Spark stores only app-local users and opaque app sessions.

### 1. Create an authentik provider

In authentik, create an **OAuth2/OpenID Provider** for Spark.

Use these settings:

- Flow: the default explicit consent or authorization flow used by your authentik installation.
- Client type: confidential.
- Redirect URI: `https://spark.example.com/api/auth/callback`
- Signing key: your normal authentik signing key.
- Scopes: include `openid`, `profile`, and `email`.
- Subject mode: stable per-user subject.

Record:

- Issuer URL, for example `https://auth.example.com/application/o/spark/`
- Client ID
- Client Secret

### 2. Create an authentik application

Create an authentik **Application** that uses the provider.

Suggested values:

- Name: `Spark`
- Slug: `spark`
- Launch URL: `https://spark.example.com/`

### 3. Configure admin group mapping

Create or choose an authentik group for Spark administrators, for example:

```text
spark-admins
```

Ensure authentik includes a `groups` claim in the ID token for the Spark provider. Spark maps users
to the local `admin` role when the configured admin group appears in that claim. Everyone else is
mapped to `user`.

### 4. Configure Spark environment

Set these variables for Spark:

```bash
SPARK_PUBLIC_URL=https://spark.example.com
SPARK_SESSION_SECRET=replace-with-a-long-random-secret
SPARK_OIDC_ISSUER=https://auth.example.com/application/o/spark/
SPARK_OIDC_CLIENT_ID=replace-with-authentik-client-id
SPARK_OIDC_CLIENT_SECRET=replace-with-authentik-client-secret
SPARK_OIDC_REDIRECT_URL=https://spark.example.com/api/auth/callback
SPARK_OIDC_POST_LOGOUT_REDIRECT_URL=https://spark.example.com/
SPARK_OIDC_ADMIN_GROUP=spark-admins
```

Keep secrets in environment variables or an uncommitted `.env` file. Do not commit client secrets or
session secrets.

### 5. Reverse proxy notes

Spark OIDC does not require authentik ForwardAuth headers. Your reverse proxy only needs to route
normal HTTPS traffic to Spark.

Required externally reachable paths:

- `/`
- `/api/auth/login`
- `/api/auth/callback`
- `/api/auth/logout`
- `/api/me`

The callback URL configured in authentik must exactly match `SPARK_OIDC_REDIRECT_URL`.

### 6. Smoke test

1. Start Spark with the environment above.
2. Open `https://spark.example.com/`.
3. Click **Sign in**.
4. Complete authentik login.
5. Confirm Spark opens the authenticated app shell.
6. Sign in as a member of `SPARK_OIDC_ADMIN_GROUP` and confirm admin features appear.
7. Sign out and confirm returning to Spark requires a new authenticated session.

### Logout behavior

Spark logout revokes the local Spark session and redirects to
`SPARK_OIDC_POST_LOGOUT_REDIRECT_URL`. It does not currently perform RP-initiated logout against
authentik's `end_session_endpoint`. If the browser still has an active authentik SSO session, clicking
**Sign in** again can immediately create a new Spark session without showing the authentik login form.
