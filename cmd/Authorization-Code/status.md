# Authorization Code Flow - Status

## Overview

Authorization Code Flow is the most secure OAuth 2.0 grant type, designed for server-side applications where the client secret can be kept confidential. It uses an authorization code as an intermediate credential, obtained through the resource owner's user-agent, which is then exchanged for an access token through a secure back-channel.

## Components & Ports

| Component | Port | Description |
|-----------|------|-------------|
| Client Application | `:8080` | Third-party app requesting access |
| Authorization Server | `:8081` | Authenticates user and issues tokens |
| Resource Server | `:8082` | Hosts protected resources |

## Endpoints

### Authorization Server (`:8081`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/authorize` | Authorization endpoint — shows login form with client info |
| `POST` | `/authorize` | Processes credentials and consent, redirects with `?code=xxx&state=yyy` |
| `POST` | `/token` | Token endpoint — exchanges authorization code for access token |
| `POST` | `/introspect` | Token introspection — validates token for resource server |
| `GET` | `/client` | Shows registered client information |

### Resource Server (`:8082`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/resource` | Protected resource — requires `Authorization: Bearer <token>` |

### Client Application (`:8080`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Home page |
| `GET` | `/login` | Initiates OAuth2 flow — redirects to authorization server |
| `GET` | `/callback` | Handles redirect back — exchanges code for token |
| `GET` | `/resource` | Fetches protected resource using stored access token |
| `GET` | `/debug` | Debug info showing component status and flow description |

## Complete Flow

```
User Agent          Client App          Auth Server         Resource Server
   |                     |                     |                     |
   |-- Visit :8080 ----->|                     |                     |
   |                     |                     |                     |
   |-- Click Login ----->|                     |                     |
   |                     |-- GET /authorize --->|                     |
   |                     |   ?response_type=code|                     |
   |                     |   &client_id=xxx     |                     |
   |                     |   &redirect_uri=yyy  |                     |
   |                     |   &state=zzz         |                     |
   |                     |                     |                     |
   |<--- Login Form -----|                     |                     |
   |                     |                     |                     |
   |-- Submit Creds ---->|                     |                     |
   |   + Approve         |-- POST /authorize -->|                     |
   |                     |                     |                     |
   |                     |<-- 302 Redirect -----|                     |
   |                     |   ?code=abc&state=zzz|                     |
   |                     |                     |                     |
   |<-- Redirect to -----|                     |                     |
   |   /callback         |                     |                     |
   |                     |                     |                     |
   |                     |-- POST /token ------>|                     |
   |                     |   grant_type=        |                     |
   |                     |   authorization_code |                     |
   |                     |   &code=abc          |                     |
   |                     |   &client_id=xxx     |                     |
   |                     |   &client_secret=yyy |                     |
   |                     |                     |                     |
   |                     |<-- access_token -----|                     |
   |                     |                     |                     |
   |                     |                     |                     |
   |                     |-- GET /resource ---->|                     |
   |                     |   Authorization:     |                     |
   |                     |   Bearer <token>     |                     |
   |                     |                     |-- POST /introspect ->|
   |                     |                     |   token=<token>     |
   |                     |                     |<-- {active:true} ---|
   |                     |                     |                     |
   |                     |<-- {data:...} -------|                     |
   |                     |                     |                     |
   |<-- Display Result --|                     |                     |
```

## Key Security Features

1. **State parameter** — CSRF protection. Client generates a random state value before redirecting, validates it on callback.
2. **Authorization code** — One-time use, 10-minute expiry. Mitigates interception risk.
3. **Client authentication** — Token endpoint requires `client_id` + `client_secret` to prove client identity.
4. **Redirect URI validation** — Authorization server validates redirect_uri matches the registered value.
5. **Access token** — 1-hour expiry, never exposed to the user-agent (obtained via server-to-server call).
6. **Back-channel token exchange** — Code is exchanged for token through direct server-to-server communication.

## How to Run

```bash
# Terminal 1 - Authorization Server
go run ./cmd/Authorization-Code/auth-server/

# Terminal 2 - Resource Server
go run ./cmd/Authorization-Code/resource-server/

# Terminal 3 - Client Application
go run ./cmd/Authorization-Code/client/
```

Then open http://localhost:8080 in a browser.
