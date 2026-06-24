# MEME Launchpad Backend Rebuild

This is an isolated, step-by-step rebuild of the backend in the sibling
`apps/api` and `apps/indexer` directories. It intentionally starts small:
each step must run and be understandable before the next responsibility is
introduced.

The original backend is not imported or modified by this project.

## Learning path

- [x] Step 1 — runnable HTTP foundation and health endpoint
- [x] Step 2 — configuration and application dependencies
- [x] Step 3 — PostgreSQL connection and first `users` repository
- [x] Step 4 — Sign-In with Ethereum (SIWE) and JWT authentication
- [x] Step 5 — token read APIs and database models
- [x] Step 6 — token-creation intent, CREATE2 prediction, and signing
- [ ] Step 7 — separate blockchain event indexer
- [ ] Step 8 — event projections for tokens, trades, and K-lines
- [ ] Step 9 — Redis nonce cache and presigned image uploads

## Step 1: run the foundation

```bash
cd app-rebuild
go test ./...
HTTP_PORT=38081 go run ./cmd/api
```

In another terminal:

```bash
curl http://localhost:38081/healthz
```

Expected response:

```json
{"service":"meme-launchpad-rebuild-api","status":"ok"}
```

At this checkpoint, there is no database, wallet login, blockchain RPC, or
Redis. The only responsibility is starting and stopping a predictable HTTP
process.

## Step 2: explicit configuration and application wiring

`cmd/api/main.go` is now only the process lifecycle owner: it loads
configuration, creates the application, starts HTTP, and handles shutdown.

`internal/config` reads and validates configuration without using global state:

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `APP_NAME` | `meme-launchpad-rebuild-api` | Name returned by `/healthz` and used in logs |
| `HTTP_PORT` | `38081` | HTTP port; must be from 1 through 65535 |

`internal/app` is the dependency container. It currently wires the configured
service name into the HTTP handler. In Step 3 it will also own the PostgreSQL
pool, so handlers do not construct connections for themselves.

Try the configuration boundary:

```bash
APP_NAME=my-local-api HTTP_PORT=48080 go run ./cmd/api
curl http://localhost:48080/healthz
```

The expected response now contains `my-local-api`.

## Step 3: PostgreSQL and the first repository

Step 3 adds a single PostgreSQL pool for the process. `internal/app` creates
it once, injects it into `UserRepository`, and closes it during shutdown. A
handler never creates a database connection itself.

Set `DATABASE_URL` to a PostgreSQL connection string. Its safe local default
is `postgres://postgres:postgres@localhost:5432/meme_launchpad?sslmode=disable`.
Create the `users` table before starting the API:

```bash
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/meme_launchpad?sslmode=disable'
createdb meme_launchpad
psql "$DATABASE_URL" -f migrations/001_create_users.sql
go run ./cmd/api
```

`UserRepository` currently contains only two operations: `FindByAddress` and
`Create`. Step 4 will call them after verifying a wallet signature, so this
step intentionally exposes no user HTTP endpoint yet.

## Step 4: Sign-In with Ethereum (SIWE) and JWT authentication

The login flow is now runnable: request a one-time message, sign it in the
wallet, then send its signature to `POST /api/v1/user/wallet-login`. The server
generates an EIP-4361 Sign-In with Ethereum (SIWE) message, validates the
stored challenge's domain, URI, version, chain ID, address, nonce, issue time,
and expiry, recovers the Ethereum address from the ERC-191 personal-signature,
consumes the nonce after successful verification, finds or creates the user,
and returns a 24-hour JWT.
`GET /api/v1/user/me` requires that token in `Authorization: Bearer <token>`.

The SIWE message binds a login to the domain, request URI, BSC Testnet chain
ID, random nonce, issued time, and expiry. Configure those relying-party values
for your deployment:

| Environment variable | Local default |
| --- | --- |
| `SIWE_DOMAIN` | `localhost:38081` |
| `SIWE_URI` | `http://localhost:38081` |
| `SIWE_CHAIN_ID` | `97` |

Set `JWT_SECRET` outside local development. The in-memory nonce store is only
for this checkpoint; restarting the API invalidates outstanding messages.
Step 9 will replace it with Redis so authentication works across processes.
This checkpoint verifies externally owned accounts (EOAs); contract-wallet
signatures require ERC-1271 verification, which is outside the current scope.
It verifies a server-issued challenge, not an arbitrary SIWE message submitted
by a client; the latter would also require an ABNF-compliant SIWE parser.

## Step 5: token read model and public APIs

Apply the next migration before starting the API:

```bash
psql "$DATABASE_URL" -f migrations/002_create_tokens.sql
```

The public read endpoints are:

```text
GET /api/v1/token/list?page=1&pageSize=20
GET /api/v1/token/detail?address=0x...
```

`TokenRepository` reads the PostgreSQL `tokens` projection. It intentionally
does not write token data or call a chain RPC during an HTTP request. Step 7
will introduce the indexer that becomes the producer of this projection.

## Step 6: token-creation intent, CREATE2 prediction, and signing

Apply the intent table migration:

```bash
psql "$DATABASE_URL" -f migrations/003_create_token_creation_requests.sql
```

`POST /api/v1/token/create` requires the JWT returned by wallet login. It does
not deploy a token. Instead, it takes the token metadata, binds the creator to
the authenticated wallet, ABI-encodes `IMEMECore.CreateTokenParams`, predicts
the `MEMEFactory` CREATE2 address, and signs the exact hash verified by
`MEMECore.createToken(data, signature)`. It persists that signed intent before
returning it to the client, which can then submit it on-chain.

```text
wallet + JWT -> API intent -> PostgreSQL audit row -> client transaction -> MEMECore.createToken
```

The create route is enabled only when all of these environment variables are
set. Keeping them unset leaves the read/login API runnable without a signing
key.

| Environment variable | Purpose |
| --- | --- |
| `TOKEN_CREATION_CHAIN_ID` | The `MEMECore.CHAIN_ID` used in the signed hash |
| `TOKEN_CREATION_CORE` | Deployed `MEMECore` address |
| `TOKEN_CREATION_FACTORY` | Deployed `MEMEFactory` address |
| `TOKEN_CREATION_SIGNER_KEY` | Hex private key for an account with `SIGNER_ROLE` on `MEMECore` |
| `TOKEN_CREATION_BYTECODE` | Hex `MEMEToken` creation bytecode, used to calculate the CREATE2 init-code hash |

For example, after login:

```bash
curl -X POST http://localhost:38081/api/v1/token/create \
  -H "Authorization: Bearer $JWT" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Meme Coin","symbol":"MEME","launchTime":0,"initialBuyPercentage":1000}'
```

`initialBuyPercentage` is basis points, so `1000` means 10%. The route accepts
the contract's current range of 0 through 9990. The response fields
`createArg`, `signature`, `requestId`, `create2Salt`, `predictedAddress`,
`nonce`, and `timestamp` are the values needed to call `createToken`.

The compatibility details are intentionally tested: CREATE2's salt uses
`keccak256(abi.encodePacked(name, symbol, totalSupply, core, timestamp,
nonce))`, where each `uint256` is 32 bytes; the signed hash is
`keccak256(abi.encodePacked(data, chainId, core))`; and the returned signature
uses Ethereum's `v = 27/28` form accepted by OpenZeppelin `ECDSA.recover`.

```mermaid
sequenceDiagram
    participant Wallet as User Wallet
    participant API as Go API
    participant DB as PostgreSQL
    participant Core as MEMECore
    participant Factory as MEMEFactory
    participant Token as New MEMEToken

    Wallet->>API: POST token create with JWT and token details
    API->>API: Read creator address from JWT
    API->>API: Build CreateTokenParams
    API->>API: Calculate CREATE2 salt
    API->>API: Predict future token address
    API->>API: ABI encode params into data
    API->>API: Sign hash of data chainId and Core address

    API->>DB: Save signed creation intent
    DB-->>API: Intent saved
    API-->>Wallet: Return data signature and predicted address

    Note over Wallet: The token does not exist yet

    Wallet->>Core: Call createToken with data and signature
    Core->>Core: Decode data and verify signature
    Core->>Core: Check expiry and replay protection
    Core->>Factory: Call deployToken
    Factory->>Token: Deploy with CREATE2
    Token-->>Factory: Return deployed token address
    Factory-->>Core: Return token address
    Core-->>Wallet: Token creation transaction succeeds
```