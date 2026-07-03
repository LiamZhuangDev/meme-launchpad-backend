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
- [x] Step 7 — separate blockchain event indexer
- [x] Step 8 — event projections for tokens, trades, and K-lines
- [x] Step 9 — Redis nonce cache and presigned image uploads
- [x] Step 10.1 — parallel gRPC server and standard health service
- [x] Step 10.2 — protobuf contracts and gRPC token read service
- [x] Step 10.3 — gRPC wallet authentication
- [x] Step 10.4 — gRPC token creation and upload authorization
- [x] Step 10.5 — transport parity tests and permanent dual transports

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

For a reproducible local setup, start PostgreSQL in Docker from the
`app-rebuild` directory:

```bash
docker run --name meme-launchpad-postgres \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=meme_launchpad \
  -p 5432:5432 \
  -v meme-launchpad-postgres-data:/var/lib/postgresql/data \
  -d postgres:16-alpine
```

The named volume preserves the database when the container stops. Wait until
PostgreSQL reports that it is ready:

```bash
docker exec meme-launchpad-postgres pg_isready -U postgres
```

Apply the Step 3 migration and start the API:

```bash
export DATABASE_URL='postgres://postgres:postgres@localhost:5432/meme_launchpad?sslmode=disable'
docker exec -i meme-launchpad-postgres \
  psql -U postgres -d meme_launchpad < migrations/001_create_users.sql
go run ./cmd/api
```

Later checkpoints add more migrations; Step 3 intentionally creates only the
`users` table. On later development sessions, restart the existing container
instead of running `docker run` again:

```bash
docker start meme-launchpad-postgres
```

If the API reports `connect: connection refused`, verify the container and
inspect its logs:

```bash
docker ps --filter name=meme-launchpad-postgres
docker logs meme-launchpad-postgres
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

Set `JWT_SECRET` outside local development. By default, local development still
uses an in-memory challenge store. Step 9 adds an optional Redis-backed store
so authentication works across multiple API processes.
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

## Step 7: separate blockchain event indexer

The API still never performs chain reads while serving a request. A separate
`cmd/indexer` process polls logs emitted by the configured `MEMECore` contract,
writes them to an append-only `chain_events` ledger, and atomically records the
last fully persisted block in `chain_event_checkpoints`.

```text
MEMECore logs -> cmd/indexer -> chain_events + checkpoint -> Step 8 projections
```

Apply the indexer migration once:

```bash
psql "$DATABASE_URL" -f migrations/004_create_chain_event_index.sql
```

Then run it in a second terminal. Unlike the API, the indexer requires these
settings:

```bash
export INDEXER_RPC_URL='https://your-bsc-testnet-rpc'
export INDEXER_CHAIN_ID=97
export INDEXER_CORE='0xYourDeployedMEMECoreAddress'
export INDEXER_START_BLOCK=12345678
go run ./cmd/indexer
```

Optional controls are `INDEXER_BLOCK_BATCH_SIZE` (default `500`) and
`INDEXER_POLL_INTERVAL_SECONDS` (default `5`). On startup the process verifies
the RPC chain ID. It resumes from its database checkpoint, or from
`INDEXER_START_BLOCK` on its first run. A range is only checkpointed in the
same transaction that inserts its logs; re-reading an already saved log is safe
because its `(chain_id, transaction_hash, log_index)` key is unique.

This step deliberately stores raw topics and data without decoding them into
the `tokens` table. Step 8 will be the explicit boundary that turns these raw,
replayable chain facts into token, trade, and K-line read models.

## Step 8: event projections for tokens, trades, and K-lines

Apply the projection migration:

```bash
psql "$DATABASE_URL" -f migrations/005_create_event_projections.sql
```

The indexer now decodes these `MEMECore` events:

```text
TokenCreated -> token_created_events + tokens
TokenBought  -> token_bought_events + trades + tokens + klines
TokenSold    -> token_sold_events + trades + tokens + klines
TokenGraduated -> token_graduated_events + tokens.status
```

The important boundary is still the same: the API reads query-friendly tables,
and the indexer is the only process that turns chain facts into those tables.

```text
MEMECore log
  -> raw chain_events row
  -> decoded event row
  -> public read models: tokens, trades, klines
  -> checkpoint advance
```

Those writes happen in one PostgreSQL transaction. If a log is saved but a
projection fails, the checkpoint is not advanced; on retry the same chain range
is read again. Re-reading is safe because event tables and trades use unique
transaction/log keys, and K-lines are only updated when a new trade row is
inserted.

K-line candles are built for `1m`, `5m`, `15m`, `30m`, `1h`, `4h`, `1d`, and
`1w`. Price is calculated from each bonding-curve trade as:

```text
price = bnbAmount / tokenAmount
```

with 18 decimal places.

## Step 9: Redis nonce cache and presigned image uploads

The SIWE challenge store now has two implementations:

```text
local/default: in-memory challenge store
production:    Redis challenge store when REDIS_ADDR is set
```

Redis makes wallet login work across multiple API instances because the
server-issued SIWE challenge is no longer trapped inside one process.

Optional Redis settings:

| Environment variable | Purpose |
| --- | --- |
| `REDIS_ADDR` | Redis address, for example `localhost:6379` |
| `REDIS_PASSWORD` | Redis password, empty for local Redis |
| `REDIS_DB` | Redis DB number, default `0` |

This step also adds authenticated presigned image upload endpoints:

```text
GET  /api/v1/file/token-logo-presign?mimeType=image/png&chainId=97
GET  /api/v1/file/token-banner-presign?mimeType=image/webp&chainId=97
GET  /api/v1/file/activity-image-presign?mimeType=image/jpeg&chainId=97
POST /api/v1/file/upload-confirm
```

The presign routes require `Authorization: Bearer <JWT>`. They return a COS
PUT URL for direct browser upload plus the future public URL. The backend only authorizes and signs the upload. It does not handle the image bytes.

```
Frontend asks backend:
GET /api/v1/file/token-logo-presign

Backend Presigner returns:
{
  "uploadUrl": "temporary signed COS PUT URL",
  "publicUrl": "future image URL",
  "key": "token-logo/97/abc.png",
  "expiresAt": ..."
}

Frontend uploads image directly:
PUT uploadUrl with image bytes

Later app stores/uses publicUrl
```


Enable presigned uploads with Tencent COS-style settings:

| Environment variable | Purpose |
| --- | --- |
| `COS_SECRET_ID` | COS access key ID |
| `COS_SECRET_KEY` | COS secret key |
| `COS_BUCKET` | Bucket name |
| `COS_REGION` | Bucket region, for example `ap-guangzhou` |
| `COS_DOMAIN` | Optional CDN/custom public domain |

Example:

```bash
export REDIS_ADDR='localhost:6379'
export COS_SECRET_ID='replace-me'
export COS_SECRET_KEY='replace-me'
export COS_BUCKET='your-bucket'
export COS_REGION='ap-guangzhou'

go run ./cmd/api
```

Then call a presign endpoint with the JWT returned by wallet login:

```bash
curl 'http://localhost:38081/api/v1/file/token-logo-presign?mimeType=image/png&chainId=97' \
  -H "Authorization: Bearer $JWT"
```

## Step 10.1: parallel gRPC foundation

The API process now listens on two transport ports while sharing one
application dependency container:

```text
HTTP/JSON :38081 -> existing REST handlers
gRPC      :39090 -> standard grpc.health.v1.Health service
```

This checkpoint deliberately keeps every existing REST route unchanged. It
proves gRPC startup, service reflection, client calls, and graceful shutdown
before business methods are moved. Configure the gRPC listener with
`GRPC_PORT`; its default is `39090`.

With `grpcurl` installed, query the standard health contract:

```bash
grpcurl -plaintext -d '{"service":"meme-launchpad-rebuild-api"}' \
  localhost:39090 grpc.health.v1.Health/Check
```

Expected status:

```json
{"status":"SERVING"}
```

## Step 10.2: parallel gRPC token reads

The first project-owned contract is
`api/proto/token/v1/token.proto`. Its generated Go client, messages, and server
interfaces live under `gen/token/v1`. Two RPCs now run beside the unchanged
REST routes:

| REST | gRPC |
| --- | --- |
| `GET /api/v1/token/list?page=2&pageSize=5` | `meme.token.v1.TokenService/ListTokens` |
| `GET /api/v1/token/detail?address=0x...` | `meme.token.v1.TokenService/GetToken` |

The handlers are separate so their transport concerns are easy to compare,
but both depend on the same repository:

```text
REST tokenList handler ---------+
                                +--> TokenRepository --> PostgreSQL tokens
gRPC ListTokens method ---------+
```

Compare `internal/httpapi/server.go` with `internal/grpcapi/token.go`:

- REST reads URL query parameters and writes JSON plus HTTP status codes.
- gRPC receives generated request messages and returns generated response
  messages plus gRPC status codes.
- Both convert page 2 with page size 5 into repository arguments
  `limit = 5, offset = 5`.

Call the gRPC methods through reflection:

```bash
grpcurl -plaintext -d '{"page":2,"pageSize":5}' \
  localhost:39090 meme.token.v1.TokenService/ListTokens

grpcurl -plaintext \
  -d '{"contractAddress":"0x1111111111111111111111111111111111111111"}' \
  localhost:39090 meme.token.v1.TokenService/GetToken
```

The runtime flow is:
```
grpcurl
  → localhost:39090
  → TokenService.ListTokens
  → grpcapi.tokenService.ListTokens()
  → TokenRepository.List(limit=5, offset=5)
  → PostgreSQL
  → Protobuf response
```

Regenerate the checked-in Go types after changing the protobuf contract:

```bash
protoc -I api/proto \
  --go_out=. --go_opt=module=github.com/meme-launchpad/app-rebuild \
  --go-grpc_out=. --go-grpc_opt=module=github.com/meme-launchpad/app-rebuild \
  api/proto/token/v1/token.proto
```

This checkpoint does not remove, proxy, or modify either existing REST token
handler. The next section applies the same parallel approach to authentication.

## Step 10.3: parallel gRPC wallet authentication

`api/proto/auth/v1/auth.proto` defines a parallel gRPC boundary over the same
`auth.Service` used by REST:

| REST | gRPC |
| --- | --- |
| `GET /api/v1/user/sign-msg?address=0x...` | `meme.auth.v1.AuthService/RequestSignMessage` |
| `POST /api/v1/user/wallet-login` | `meme.auth.v1.AuthService/WalletLogin` |
| `GET /api/v1/user/me` | `meme.auth.v1.AuthService/GetCurrentUser` |

Both transports execute the same security flow: create and store a
server-issued SIWE challenge, verify its fields and wallet signature, consume
the nonce, find or create the user, and issue a JWT. Only the transport code
is different:

```text
REST auth handler ----+
                      +--> auth.Service --> ChallengeStore + UserRepository
gRPC auth method -----+
```

Request the message that the wallet must sign:

```bash
grpcurl -plaintext \
  -d '{"address":"0xYourWalletAddress"}' \
  localhost:39090 meme.auth.v1.AuthService/RequestSignMessage
```

After the wallet signs the returned `message`, exchange the signature for a
JWT:

```bash
grpcurl -plaintext \
  -d '{"address":"0xYourWalletAddress","signature":"0xWalletSignature"}' \
  localhost:39090 meme.auth.v1.AuthService/WalletLogin
```

REST receives the JWT in an HTTP `Authorization` header. gRPC carries the same
header value as request metadata:

```bash
export JWT='token-returned-by-WalletLogin'

grpcurl -plaintext \
  -H "authorization: Bearer $JWT" \
  -d '{}' \
  localhost:39090 meme.auth.v1.AuthService/GetCurrentUser
```

Regenerate the auth messages and service interfaces after editing the contract:

```bash
protoc -I api/proto \
  --go_out=. --go_opt=module=github.com/meme-launchpad/app-rebuild \
  --go-grpc_out=. --go-grpc_opt=module=github.com/meme-launchpad/app-rebuild \
  api/proto/auth/v1/auth.proto
```

The gRPC boundary maps malformed addresses to `InvalidArgument`, invalid or
replayed signatures to `Unauthenticated`, and unexpected storage failures to
`Internal`. Existing REST routes remain unchanged. The next section applies
the same authenticated transport pattern to creation and uploads.

## Step 10.4: parallel token creation and upload authorization

Two more project-owned gRPC services now sit beside the existing REST routes:

| REST | gRPC |
| --- | --- |
| `POST /api/v1/token/create` | `meme.tokencreation.v1.TokenCreationService/CreateToken` |
| Three `/api/v1/file/*-presign` routes | `meme.upload.v1.UploadService/PresignImage` with an `ImageKind` enum |
| `POST /api/v1/file/upload-confirm` | `meme.upload.v1.UploadService/ConfirmUpload` |

Both gRPC handlers require `authorization: Bearer <JWT>` metadata. Token
creation takes the creator address only from verified JWT claims; there is no
creator field in `CreateTokenRequest` that a client could spoof.

```text
gRPC metadata JWT
  -> recover authenticated wallet
  -> tokencreation.Service.Create
  -> persist signed intent
  -> return contract data + signature + predicted address
```

With token-creation configuration enabled, create an intent using the JWT from
Step 10.3:

```bash
grpcurl -plaintext \
  -H "authorization: Bearer $JWT" \
  -d '{"name":"Meme Coin","symbol":"MEME","launchTime":"0","initialBuyPercentage":"1000"}' \
  localhost:39090 meme.tokencreation.v1.TokenCreationService/CreateToken
```

With COS configuration enabled, request a presigned upload URL:

```bash
grpcurl -plaintext \
  -H "authorization: Bearer $JWT" \
  -d '{"kind":"IMAGE_KIND_TOKEN_LOGO","mimeType":"image/png","chainId":97}' \
  localhost:39090 meme.upload.v1.UploadService/PresignImage
```

`ImageKind` replaces three nearly identical route handlers with one typed gRPC
method. It maps to the same storage folders: `token-logo`, `token-banner`, and
`activity-image`.

`ConfirmUpload` intentionally remains the same authenticated acknowledgment as
the REST placeholder:

```bash
grpcurl -plaintext \
  -H "authorization: Bearer $JWT" \
  -d '{}' \
  localhost:39090 meme.upload.v1.UploadService/ConfirmUpload
```

Regenerate both services after editing their protobuf contracts:

```bash
protoc -I api/proto \
  --go_out=. --go_opt=module=github.com/meme-launchpad/app-rebuild \
  --go-grpc_out=. --go-grpc_opt=module=github.com/meme-launchpad/app-rebuild \
  api/proto/tokencreation/v1/token_creation.proto \
  api/proto/upload/v1/upload.proto
```

The token-creation service is registered only when all `TOKEN_CREATION_*`
settings are present. The upload service is registered only when the required
`COS_*` settings are present. Existing REST routes remain unchanged.

## Step 10.5: verified parity and permanent REST plus gRPC

The architecture decision is to keep both transports. REST is not deprecated,
proxied through gRPC, or scheduled for removal. The same application process
continues to serve:

```text
Frontend / HTTP client --> REST handlers :38081 --+
                                                   +--> shared services and repositories
Internal / gRPC client --> gRPC handlers :39090 --+
```

Cross-transport tests now prove the important semantic boundaries rather than
requiring byte-for-byte JSON and Protobuf responses:

- page 2 with page size 5 becomes repository `limit=5, offset=5` in both;
- the same JWT resolves to the same user ID and wallet address in both;
- token creation binds the same authenticated wallet as creator in both;
- upload kind, MIME type, and chain ID reach the presigner identically;
- a registration test verifies health, token, auth, token-creation, and upload
  gRPC services are all exposed when their dependencies are configured.

Run the final transport suite:

```bash
go test ./internal/grpcapi -run 'Parity|RegistersEvery' -v
```

Or verify the complete API, generated contracts, and internal packages:

```bash
go test ./cmd/api ./gen/... ./internal/...
```

At this checkpoint every REST capability in `app-rebuild` has a parallel gRPC
surface. `upload-confirm` is still only an authenticated acknowledgment in
both transports; object verification and metadata persistence are a future
application feature, not unfinished gRPC transport work.
