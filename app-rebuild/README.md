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
- [x] Step 11.1 — public REST and loopback-only internal gRPC listeners
- [x] Step 11.2 — internal Go service client
- [x] Step 11.3 — private-network mutual TLS and service identity
- [x] Step 12.1 — standalone internal token-read gRPC service
- [x] Step 12.2 — REST-to-gRPC token reader adapter
- [x] Step 12.3 — switch REST token reads and remove duplicate ownership
- [x] Step 13.1 — standalone internal token-creation gRPC service
- [x] Step 13.2 — REST-to-gRPC token-creation adapter
- [x] Step 13.3 — switch REST token creation and remove duplicate ownership
- [x] Step 14.1 — standalone internal upload gRPC service
- [x] Step 14.2 — REST-to-gRPC upload adapter
- [x] Step 14.3 — switch REST uploads and remove duplicate ownership
- [x] Step 15 — remove the API-owned gRPC listener
- [x] Step 16 — separate JWT verification from the SIWE auth service
- [x] Step 17 — asymmetric Ed25519 JWT signing and verification

# Data Flow
```mermaid
flowchart LR
    Browser[Browser / Frontend]
    BFF[REST BFF<br/>cmd/api :38081]

    Browser -->|HTTP JSON| BFF

    subgraph Auth["Authentication inside BFF"]
        AuthService[auth.Service]
        PrivateKey[Ed25519 JWT private key]
        Challenges[(Redis / Memory<br/>SIWE challenges)]
        Users[(PostgreSQL<br/>users)]
    end

    BFF --> AuthService
    PrivateKey -->|sign JWT| AuthService
    AuthService <--> Challenges
    AuthService <--> Users
    AuthService -->|EdDSA JWT| Browser

    subgraph Internal["Standalone internal gRPC services"]
        TokenRead[Token service<br/>:39100]
        TokenCreate[Token-creation service<br/>:39200]
        Upload[Upload service<br/>:39300]
        PublicKey[Ed25519 JWT public key]
    end

    BFF -->|gRPC + mTLS| TokenRead
    BFF -->|gRPC + JWT + mTLS| TokenCreate
    BFF -->|gRPC + JWT + mTLS| Upload

    PublicKey -->|verify JWT| TokenCreate
    PublicKey -->|verify JWT| Upload

    TokenRead --> TokenDB[(PostgreSQL<br/>tokens)]
    TokenCreate --> CreationDB[(PostgreSQL<br/>creation requests)]

    Upload -->|sign PUT permission| BFF
    BFF -->|presigned URL| Browser
    Browser -->|PUT image bytes directly| COS[Tencent COS]

    TokenCreate -->|signed creation intent| BFF
    BFF --> Browser
    Browser -->|MEMECore.createToken transaction| Chain[Blockchain]

    Chain -->|contract events| Indexer[Indexer]
    Indexer --> TokenDB
```

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
| `HTTP_HOST` | `0.0.0.0` | REST listener host; all interfaces by default |
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

Generate the local Ed25519 JWT keypair before starting the API:

```bash
./scripts/generate-dev-jwt-keys.sh
```

The API defaults to `.local-jwt/private.pem`. By default, local development still
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

The backend ABI-encodes the token creation parameters, predicts the deterministic CREATE2 token address, signs the exact hash that the smart contract will verify, and returns the data plus signature to the client. It uses Ethereum primitives like CREATE2, ABI encoding, keccak256, and ECDSA.

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

> Steps 10 and 11 document the earlier parallel-transport checkpoints. Step 15
> removes the API-owned `:39090` listener after all internal capabilities move
> to standalone services; those historical `:39090` commands no longer apply
> to the final runtime.

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

## Step 11.1: explicit public and internal listener boundaries

The two transports now have separate host configuration as well as separate
ports:

| Transport | Default address | Intended caller |
| --- | --- | --- |
| REST | `0.0.0.0:38081` | Frontend, browser, and public HTTP clients |
| gRPC | `127.0.0.1:39090` | Internal Go processes on the same machine |

`0.0.0.0` means the REST process listens on every network interface; firewall,
load-balancer, and deployment rules still decide whether it is internet
reachable. `127.0.0.1` means gRPC accepts only connections originating from
the same machine, so it is not accidentally exposed on a developer laptop.

The listener settings are:

| Environment variable | Default |
| --- | --- |
| `HTTP_HOST` | `0.0.0.0` |
| `HTTP_PORT` | `38081` |
| `GRPC_HOST` | `127.0.0.1` |
| `GRPC_PORT` | `39090` |

Start both listeners in the same API process:

```bash
go run ./cmd/api
```

The frontend-facing REST check remains:

```bash
curl http://localhost:38081/healthz
```

A local internal client can check gRPC:

```bash
grpcurl -plaintext -d '{"service":"meme-launchpad-rebuild-api"}' \
  localhost:39090 grpc.health.v1.Health/Check
```

For containers or Kubernetes, internal services need a routable listener. Step
11.3 combines `GRPC_HOST=0.0.0.0` with mandatory mTLS. Attach the API and
callers to a private network, expose only `38081` through public ingress, and
do not publish `39090` publicly:

```text
Internet --> ingress --> REST 0.0.0.0:38081
Internal private network --> gRPC 0.0.0.0:39090
```

## Step 11.2: internal Go service client

`cmd/internal-client` is a separate Go process representing an internal
service. It does not call REST or manually construct HTTP/2 requests. It uses
the generated `TokenServiceClient` through a small reusable wrapper in
`internal/grpcclient`:

```text
cmd/internal-client
  -> grpcclient.Client
  -> generated TokenServiceClient
  -> standalone token service :39100
  -> grpcapi token handler
  -> TokenRepository
```

Start the token service first:

```bash
go run ./cmd/token-service
```

In another terminal, run the internal client:

```bash
go run ./cmd/internal-client
```

It checks the standard gRPC health service, calls `ListTokens` with page 1 and
page size 20, and prints the Protobuf response as readable JSON. Configure it
with:

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `INTERNAL_GRPC_TARGET` | `127.0.0.1:39100` | Standalone token-service address |
| `INTERNAL_GRPC_TIMEOUT` | `5s` | Overall connect and RPC deadline |
| `TOKEN_PAGE` | `1` | Token page to request |
| `TOKEN_PAGE_SIZE` | `20` | Tokens per page |

For example:

```bash
TOKEN_PAGE=2 TOKEN_PAGE_SIZE=5 go run ./cmd/internal-client
```

For container-to-container traffic, use the token service's private DNS name together
with the Step 11.3 client certificate settings:

```bash
INTERNAL_GRPC_TARGET=meme-token-service:39100 \
INTERNAL_GRPC_CA_FILE=/certs/ca.crt \
INTERNAL_GRPC_CERT_FILE=/certs/internal-client.crt \
INTERNAL_GRPC_KEY_FILE=/certs/internal-client.key \
INTERNAL_GRPC_SERVER_NAME=meme-api \
go run ./cmd/internal-client
```

This checkpoint uses plaintext transport credentials only when the server is
left in its default loopback-only development mode. The next section adds
mutual TLS for private-network deployments.

## Step 11.3: mutual TLS and internal service identity

When TLS configuration is present, the gRPC server requires mutual TLS:

```text
Internal client certificate
  -> signed by configured internal CA
  -> TLS encryption and certificate verification
  -> URI SAN or Common Name checked against service allowlist
  -> gRPC handler
```

The server settings must be supplied together:

| Environment variable | Purpose |
| --- | --- |
| `GRPC_TLS_CERT_FILE` | API server certificate |
| `GRPC_TLS_KEY_FILE` | API server private key |
| `GRPC_TLS_CLIENT_CA_FILE` | CA trusted to issue internal client certificates |
| `GRPC_ALLOWED_CLIENT_IDS` | Comma-separated URI SANs or Common Names allowed to call gRPC |

The internal client settings must also be supplied together:

| Environment variable | Purpose |
| --- | --- |
| `INTERNAL_GRPC_CA_FILE` | CA trusted to issue the API server certificate |
| `INTERNAL_GRPC_CERT_FILE` | Internal service client certificate |
| `INTERNAL_GRPC_KEY_FILE` | Internal service private key |
| `INTERNAL_GRPC_SERVER_NAME` | DNS identity expected in the server certificate |

Generate disposable local development certificates. The generated directory
is ignored by Git:

```bash
scripts/generate-dev-mtls.sh
```

Start the API with mTLS and one allowed service identity:

```bash
GRPC_TLS_CERT_FILE=.local-certs/server.crt \
GRPC_TLS_KEY_FILE=.local-certs/server.key \
GRPC_TLS_CLIENT_CA_FILE=.local-certs/ca.crt \
GRPC_ALLOWED_CLIENT_IDS='spiffe://meme-launchpad/internal-client' \
go run ./cmd/api
```

Run the authenticated internal client:

```bash
INTERNAL_GRPC_CA_FILE=.local-certs/ca.crt \
INTERNAL_GRPC_CERT_FILE=.local-certs/internal-client.crt \
INTERNAL_GRPC_KEY_FILE=.local-certs/internal-client.key \
INTERNAL_GRPC_SERVER_NAME=localhost \
go run ./cmd/internal-client
```

A certificate signed by the CA but carrying an identity absent from
`GRPC_ALLOWED_CLIENT_IDS` receives `PermissionDenied`. A missing or unverified
client certificate is rejected during TLS or with `Unauthenticated`. Unary and
streaming RPCs use the same identity policy.

Production should issue short-lived certificates through its secret manager
or workload-identity system rather than use the development CA. With no TLS
variables configured, the `127.0.0.1:39090` development listener remains
plaintext for the local learning workflow. The configuration rejects a
non-loopback `GRPC_HOST` or `INTERNAL_GRPC_TARGET` when mTLS is absent.

## Step 12.1: standalone token-read gRPC service

Token reads now have an independently runnable service process:

```text
cmd/token-service :39100
  -> TokenService gRPC handler
  -> TokenRepository
  -> PostgreSQL tokens projection
```

This is the first extraction checkpoint. The public API still reads tokens
directly and continues serving its existing parallel gRPC surface. Keeping
that behavior unchanged makes the extraction reversible while the new process
is verified.

The standalone listener settings are:

| Environment variable | Default |
| --- | --- |
| `TOKEN_SERVICE_GRPC_HOST` | `127.0.0.1` |
| `TOKEN_SERVICE_GRPC_PORT` | `39100` |

Start PostgreSQL, then run the service:

```bash
go run ./cmd/token-service
```

Expected log:

```text
meme-token-service internal gRPC (loopback plaintext) listening on 127.0.0.1:39100
```

Use the internal Go client against the extracted process:

```bash
INTERNAL_GRPC_TARGET=127.0.0.1:39100 go run ./cmd/internal-client
```

Or call it with `grpcurl`:

```bash
grpcurl -plaintext -d '{"page":1,"pageSize":20}' \
  127.0.0.1:39100 meme.token.v1.TokenService/ListTokens
```

The process initializes only its PostgreSQL pool and `TokenRepository`; it does
not construct REST, Redis, SIWE, uploads, or token-creation signing. Existing
`GRPC_TLS_*` settings can also secure this listener with the Step 11.3 mTLS
policy.

Step 12.2 adds an outbound adapter that implements the REST layer's
`TokenReader` interface using the generated gRPC client. REST still keeps its
direct repository fallback until the following cutover checkpoint.

## Step 12.2: REST-to-gRPC token reader adapter

`internal/grpcclient.TokenReader` now adapts the generated gRPC client to the
same interface already consumed by the REST token handlers:

```text
REST TokenReader interface
  -> grpcclient.TokenReader
  -> generated TokenServiceClient
  -> standalone token service :39100
```

The adapter translates REST's `limit/offset` pagination into Protobuf's
`page/page_size`, calls both `ListTokens` and `GetToken`, and maps Protobuf
tokens back into the repository-shaped values expected by the unchanged JSON
handlers. It requires an offset aligned to the page size, which is exactly how
the REST pagination handler calls `TokenReader.List`.

This checkpoint deliberately does not construct a connection in `cmd/api` or
change `Application.HTTPServer()`. REST therefore still reads PostgreSQL
directly while the adapter is tested independently. Step 12.3 will wire this
adapter into REST and move token-read ownership fully behind the standalone
gRPC service.

## Step 12.3: route REST token reads through internal gRPC

The API composition root now opens a persistent gRPC connection to the
standalone token service during startup. The public REST handlers are
unchanged, but their `TokenReader` implementation is now the Step 12.2 gRPC
adapter:

```text
Browser
  -> REST :38081
  -> httpapi token handler
  -> grpcclient.TokenReader
  -> TokenService gRPC :39100
  -> TokenRepository
  -> PostgreSQL
```

The API no longer constructs a `TokenRepository` for reads and no longer
registers `meme.token.v1.TokenService` on its own `:39090` gRPC listener. That
listener remains available for the auth, token-creation, and upload services.
The outbound token-service connection is closed with the rest of the API's
owned resources.

Start the processes in this order:

```bash
# Terminal 1: restart the PostgreSQL container created in Step 3
docker start meme-launchpad-postgres

# Terminal 2: extracted internal service
go run ./cmd/token-service

# Terminal 3: public REST API
go run ./cmd/api
```

This project does not include a Docker Compose file. On the first run, create
the PostgreSQL container with the `docker run` command in Step 3 instead of
`docker start`, then apply the migrations before starting the two Go processes.
After completing Step 13.3, also start `cmd/token-creation-service` before the
API; the final four-process run order is documented there.

Then the existing browser-facing request crosses the new internal boundary:

```bash
curl 'http://localhost:38081/api/v1/token/list?page=1&pageSize=20'
```

The API waits for `TOKEN_SERVICE_GRPC_HOST:TOKEN_SERVICE_GRPC_PORT` during
startup, verifies the `meme-token-service` health status, and fails fast if it
cannot connect to the expected service. Loopback development is plaintext.

For private-network mTLS, configure the API's client identity separately from
the token service's server identity:

| API environment variable | Purpose |
| --- | --- |
| `TOKEN_SERVICE_GRPC_CA_FILE` | CA that issued the token-service server certificate |
| `TOKEN_SERVICE_GRPC_CERT_FILE` | API's client certificate |
| `TOKEN_SERVICE_GRPC_KEY_FILE` | API's client private key |
| `TOKEN_SERVICE_GRPC_SERVER_NAME` | Expected token-service DNS identity |

All four client TLS variables must be set together, and a non-loopback target
is rejected without them.

Generate development certificates once from `app-rebuild`:

```bash
./scripts/generate-dev-mtls.sh
```

Start the token service with its server certificate and the client identity it
allows:

```bash
GRPC_TLS_CERT_FILE=.local-certs/server.crt \
GRPC_TLS_KEY_FILE=.local-certs/server.key \
GRPC_TLS_CLIENT_CA_FILE=.local-certs/ca.crt \
GRPC_ALLOWED_CLIENT_IDS='spiffe://meme-launchpad/internal-client' \
go run ./cmd/token-service
```

In another terminal, start the REST API with its token-service client
certificate:

```bash
TOKEN_SERVICE_GRPC_HOST=127.0.0.1 \
TOKEN_SERVICE_GRPC_PORT=39100 \
TOKEN_SERVICE_GRPC_CA_FILE=.local-certs/ca.crt \
TOKEN_SERVICE_GRPC_CERT_FILE=.local-certs/internal-client.crt \
TOKEN_SERVICE_GRPC_KEY_FILE=.local-certs/internal-client.key \
TOKEN_SERVICE_GRPC_SERVER_NAME=localhost \
go run ./cmd/api
```

During the handshake, the API verifies that the token-service certificate is
valid for `localhost`. The token service verifies that the API certificate was
issued by the development CA and carries the allowed
`spiffe://meme-launchpad/internal-client` identity.

## Step 13.1: standalone token-creation gRPC service

Token creation now has an independently runnable internal process:

```text
cmd/token-creation-service :39200
  -> JWT bearer verification
  -> TokenCreationService gRPC handler
  -> tokencreation.Service
  -> TokenCreationRepository
  -> PostgreSQL
```

This first extraction checkpoint does not change the REST handler. The API
still creates signed intents directly, and its existing `:39090` gRPC surface
still includes token creation until the adapter and cutover checkpoints are
complete.

The listener defaults are:

| Environment variable | Default |
| --- | --- |
| `TOKEN_CREATION_SERVICE_GRPC_HOST` | `127.0.0.1` |
| `TOKEN_CREATION_SERVICE_GRPC_PORT` | `39200` |

Apply `migrations/003_create_token_creation_requests.sql` and configure all
five `TOKEN_CREATION_*` values documented in Step 6. The standalone service
must receive the public verification key corresponding to the API's private
signing key:

```bash
JWT_PUBLIC_KEY_FILE='.local-jwt/public.pem' \
TOKEN_CREATION_CHAIN_ID='97' \
TOKEN_CREATION_CORE='0xYourMemeCoreAddress' \
TOKEN_CREATION_FACTORY='0xYourMemeFactoryAddress' \
TOKEN_CREATION_SIGNER_KEY='your-signer-private-key' \
TOKEN_CREATION_BYTECODE='0xYourCompiledTokenCreationBytecode' \
go run ./cmd/token-creation-service
```

The process only registers health and
`meme.tokencreation.v1.TokenCreationService`; it uses the auth component as a
JWT parser without exposing the auth RPC service.

To secure `:39200` with the development mTLS certificates, add the same server
variables used by the token-read service:

```bash
GRPC_TLS_CERT_FILE=.local-certs/server.crt \
GRPC_TLS_KEY_FILE=.local-certs/server.key \
GRPC_TLS_CLIENT_CA_FILE=.local-certs/ca.crt \
GRPC_ALLOWED_CLIENT_IDS='spiffe://meme-launchpad/internal-client' \
JWT_PUBLIC_KEY_FILE='.local-jwt/public.pem' \
TOKEN_CREATION_CHAIN_ID='97' \
TOKEN_CREATION_CORE='0xYourMemeCoreAddress' \
TOKEN_CREATION_FACTORY='0xYourMemeFactoryAddress' \
TOKEN_CREATION_SIGNER_KEY='your-signer-private-key' \
TOKEN_CREATION_BYTECODE='0xYourCompiledTokenCreationBytecode' \
go run ./cmd/token-creation-service
```

Step 13.2 will add a client adapter for the existing REST token-creation
handler without cutting it over yet.

## Step 13.2: REST-to-gRPC token-creation adapter

The REST transport now owns a small `TokenCreator` interface instead of
depending directly on `*tokencreation.Service`. Both the local signing service
and the new `grpcclient.TokenCreator` satisfy it:

```text
REST TokenCreator interface
  -> grpcclient.TokenCreator
  -> generated TokenCreationServiceClient
  -> standalone token-creation service :39200
```

Token creation is authenticated at both boundaries. After REST validates the
browser JWT, it places that raw token in the request context. The adapter sends
it as outgoing `authorization: Bearer <JWT>` gRPC metadata, and the standalone
handler validates it again using the JWT public key. The Protobuf request
does not contain a creator address; the internal handler derives the creator
from the verified JWT to prevent caller impersonation.

The adapter maps the existing domain request fields into
`CreateTokenRequest`, then maps the signed intent response back into the same
JSON-shaped `tokencreation.Response` used by REST today. RPC errors remain
wrapped so their gRPC status can still be inspected.

This checkpoint does not open a `:39200` connection from `cmd/api` and does not
switch the REST route. Step 13.3 will add that managed connection, verify the
`meme-token-creation-service` health identity, cut REST over, and remove token
creation from the API's own `:39090` gRPC listener.

## Step 13.3: route REST token creation through internal gRPC

The API composition root now opens and owns a second internal gRPC connection.
The unchanged browser-facing route crosses the token-creation service boundary:

```text
Browser
  -> POST /api/v1/token/create on REST :38081
  -> httpapi TokenCreator interface
  -> grpcclient.TokenCreator
  -> TokenCreationService gRPC :39200
  -> tokencreation.Service
  -> TokenCreationRepository
  -> PostgreSQL
```

`cmd/api` no longer constructs `tokencreation.Service`, loads the signing key,
or writes token-creation requests directly. It also no longer registers
`meme.tokencreation.v1.TokenCreationService` on its own `:39090` listener.
Only `cmd/token-creation-service` owns that business logic and the
`TOKEN_CREATION_*` contract configuration.

Run the complete local process set in this order:

```bash
# Terminal 1
docker start meme-launchpad-postgres

# Terminal 2: token reads
go run ./cmd/token-service

# Terminal 3: token creation; include the Step 13.1 JWT and contract variables
JWT_PUBLIC_KEY_FILE='.local-jwt/public.pem' \
TOKEN_CREATION_CHAIN_ID='97' \
TOKEN_CREATION_CORE='0xYourMemeCoreAddress' \
TOKEN_CREATION_FACTORY='0xYourMemeFactoryAddress' \
TOKEN_CREATION_SIGNER_KEY='your-signer-private-key' \
TOKEN_CREATION_BYTECODE='0xYourCompiledTokenCreationBytecode' \
go run ./cmd/token-creation-service

# Terminal 4: public REST API owns the private signing key
JWT_PRIVATE_KEY_FILE='.local-jwt/private.pem' go run ./cmd/api
```

The API verifies both internal health identities during startup:

```text
127.0.0.1:39100 -> meme-token-service
127.0.0.1:39200 -> meme-token-creation-service
```

For mTLS, the token-creation process uses the `GRPC_TLS_*` server variables
shown in Step 13.1. Configure the API's separate client role with all four of
these variables:

| API environment variable | Purpose |
| --- | --- |
| `TOKEN_CREATION_SERVICE_GRPC_CA_FILE` | CA that issued the token-creation server certificate |
| `TOKEN_CREATION_SERVICE_GRPC_CERT_FILE` | API's client certificate |
| `TOKEN_CREATION_SERVICE_GRPC_KEY_FILE` | API's client private key |
| `TOKEN_CREATION_SERVICE_GRPC_SERVER_NAME` | Expected token-creation server DNS identity |

The development certificates can be reused for both outbound connections:

```bash
JWT_PRIVATE_KEY_FILE='.local-jwt/private.pem' \
TOKEN_SERVICE_GRPC_CA_FILE=.local-certs/ca.crt \
TOKEN_SERVICE_GRPC_CERT_FILE=.local-certs/internal-client.crt \
TOKEN_SERVICE_GRPC_KEY_FILE=.local-certs/internal-client.key \
TOKEN_SERVICE_GRPC_SERVER_NAME=localhost \
TOKEN_CREATION_SERVICE_GRPC_CA_FILE=.local-certs/ca.crt \
TOKEN_CREATION_SERVICE_GRPC_CERT_FILE=.local-certs/internal-client.crt \
TOKEN_CREATION_SERVICE_GRPC_KEY_FILE=.local-certs/internal-client.key \
TOKEN_CREATION_SERVICE_GRPC_SERVER_NAME=localhost \
go run ./cmd/api
```

Both internal connections are closed during API shutdown. A non-loopback
target is rejected unless its complete client mTLS configuration is present.

## Step 14.1: standalone upload gRPC service

Presigned upload URL creation now has an independently runnable process:

```text
cmd/upload-service :39300
  -> JWT bearer verification
  -> UploadService gRPC handler
  -> upload.Service
  -> signed COS PUT URL
```

This checkpoint leaves the REST upload routes unchanged. The API still calls
its local upload service and continues exposing the parallel upload gRPC
handler on `:39090` until Steps 14.2 and 14.3 complete the cutover.

The standalone listener defaults are:

| Environment variable | Default |
| --- | --- |
| `UPLOAD_SERVICE_GRPC_HOST` | `127.0.0.1` |
| `UPLOAD_SERVICE_GRPC_PORT` | `39300` |

The process does not need PostgreSQL or Redis. It requires the JWT public key
plus the COS configuration documented in Step 9:

```bash
JWT_PUBLIC_KEY_FILE='.local-jwt/public.pem' \
COS_SECRET_ID='replace-me' \
COS_SECRET_KEY='replace-me' \
COS_BUCKET='your-bucket' \
COS_REGION='ap-guangzhou' \
go run ./cmd/upload-service
```

It registers health and `meme.upload.v1.UploadService` only. The auth component
is used strictly as a JWT parser; SIWE and login RPCs are not exposed by this
process. As before, the backend signs upload permission but never receives the
image bytes.

For development mTLS, add the server credentials and allowed API client
identity:

```bash
GRPC_TLS_CERT_FILE=.local-certs/server.crt \
GRPC_TLS_KEY_FILE=.local-certs/server.key \
GRPC_TLS_CLIENT_CA_FILE=.local-certs/ca.crt \
GRPC_ALLOWED_CLIENT_IDS='spiffe://meme-launchpad/internal-client' \
JWT_PUBLIC_KEY_FILE='.local-jwt/public.pem' \
COS_SECRET_ID='replace-me' \
COS_SECRET_KEY='replace-me' \
COS_BUCKET='your-bucket' \
COS_REGION='ap-guangzhou' \
go run ./cmd/upload-service
```

Step 14.2 will add a REST-facing gRPC client adapter while keeping the local
upload implementation wired as the rollback path.

## Step 14.2: REST-to-gRPC upload adapter

The REST upload boundary is now context-aware and includes both operations:

```text
Presign(ctx, folder, mimeType, chainID)
Confirm(ctx)
```

Both the local `upload.Service` and `grpcclient.UploadService` implement this
interface. The local confirmation remains the existing authenticated
acknowledgment; object verification and metadata persistence are still a future
checkpoint.

The adapter translates the REST folder into the Protobuf image kind, forwards
the validated browser JWT as outgoing gRPC authorization metadata, and maps
the Protobuf expiry timestamp back into the existing REST response:

```text
REST Presigner interface
  -> grpcclient.UploadService
  -> generated UploadServiceClient
  -> standalone upload service :39300
```

The same metadata forwarding applies to `ConfirmUpload`. The standalone
service validates the JWT again, so neither operation trusts an unauthenticated
internal caller.

This checkpoint does not connect `cmd/api` to `:39300`; REST still uses its
local COS signer. Step 14.3 will manage the upload-service connection, verify
the `meme-upload-service` health identity, cut REST over, and remove upload
from the API's own `:39090` gRPC listener.

## Step 14.3: route REST uploads through internal gRPC

The API composition root now owns a third internal gRPC connection. Both
presigning and confirmation cross the upload-service boundary:

```text
Browser
  -> REST upload route :38081
  -> httpapi Presigner interface
  -> grpcclient.UploadService
  -> UploadService gRPC :39300
  -> upload.Service
  -> signed COS URL
```

`cmd/api` no longer constructs `upload.Service`, reads COS credentials for its
own use, or exposes `meme.upload.v1.UploadService` on `:39090`. Only
`cmd/upload-service` owns COS signing. The API verifies the
`meme-upload-service` health identity during startup and closes the connection
during shutdown.

The complete local run order is now:

```bash
# Terminal 1
docker start meme-launchpad-postgres

# Terminal 2
go run ./cmd/token-service

# Terminal 3: include all Step 13.1 contract variables
JWT_PUBLIC_KEY_FILE='.local-jwt/public.pem' \
TOKEN_CREATION_CHAIN_ID='97' \
TOKEN_CREATION_CORE='0xYourMemeCoreAddress' \
TOKEN_CREATION_FACTORY='0xYourMemeFactoryAddress' \
TOKEN_CREATION_SIGNER_KEY='your-signer-private-key' \
TOKEN_CREATION_BYTECODE='0xYourCompiledTokenCreationBytecode' \
go run ./cmd/token-creation-service

# Terminal 4
JWT_PUBLIC_KEY_FILE='.local-jwt/public.pem' \
COS_SECRET_ID='replace-me' \
COS_SECRET_KEY='replace-me' \
COS_BUCKET='your-bucket' \
COS_REGION='ap-guangzhou' \
go run ./cmd/upload-service

# Terminal 5: only the API receives the private signing key
JWT_PRIVATE_KEY_FILE='.local-jwt/private.pem' go run ./cmd/api
```

The three internal health identities are:

```text
127.0.0.1:39100 -> meme-token-service
127.0.0.1:39200 -> meme-token-creation-service
127.0.0.1:39300 -> meme-upload-service
```

For mTLS, configure the API's upload-service client role separately:

| API environment variable | Purpose |
| --- | --- |
| `UPLOAD_SERVICE_GRPC_CA_FILE` | CA that issued the upload server certificate |
| `UPLOAD_SERVICE_GRPC_CERT_FILE` | API's client certificate |
| `UPLOAD_SERVICE_GRPC_KEY_FILE` | API's client private key |
| `UPLOAD_SERVICE_GRPC_SERVER_NAME` | Expected upload server DNS identity |

Development example:

```bash
JWT_PRIVATE_KEY_FILE='.local-jwt/private.pem' \
UPLOAD_SERVICE_GRPC_CA_FILE=.local-certs/ca.crt \
UPLOAD_SERVICE_GRPC_CERT_FILE=.local-certs/internal-client.crt \
UPLOAD_SERVICE_GRPC_KEY_FILE=.local-certs/internal-client.key \
UPLOAD_SERVICE_GRPC_SERVER_NAME=localhost \
go run ./cmd/api
```

When all three standalone services use mTLS, combine this group with the
token-read and token-creation client variables documented in Steps 12.3 and
13.3. A non-loopback upload target is rejected without all four client TLS
values.

## Step 15: REST-only API process

After token reads, token creation, and uploads were extracted, the API-owned
gRPC listener contained only the parallel auth transport. The final cleanup
removes that listener while leaving the REST auth implementation unchanged.

The final process boundaries are:

| Process | Listener | Responsibility |
| --- | --- | --- |
| `cmd/api` | REST `:38081` | Browser API and SIWE/JWT auth |
| `cmd/token-service` | gRPC `:39100` | Token reads |
| `cmd/token-creation-service` | gRPC `:39200` | Signed token-creation intents |
| `cmd/upload-service` | gRPC `:39300` | COS presigning and confirmation acknowledgment |

`Application.GRPCServer`, the API's gRPC startup/shutdown path, and the unused
`GRPC_HOST`/`GRPC_PORT` settings are gone. `GRPC_TLS_*` remains the common
server-side mTLS configuration used by each standalone service. The
service-specific `*_SERVICE_GRPC_*` settings configure listener addresses and
the API's outbound client identities.

The `grpcapi` package is intentionally retained: its token, token-creation,
and upload handlers receive requests inside the standalone gRPC processes.
Auth code is also retained for REST and for JWT parsing in protected internal
services, but auth is no longer exposed through an API-owned gRPC listener.

## Step 16: dedicated JWT verifier

Protected standalone services no longer construct a partially initialized
`auth.Service` with a nil user repository. They use the narrower component:

```go
publicKey, _ := auth.LoadJWTPublicKey(cfg.Auth.JWTPublicKeyFile)
tokenVerifier := auth.NewJWTVerifier(publicKey)
```

`JWTVerifier` implements only `ParseToken`. It has no SIWE configuration,
challenge store, or user repository, so the token-creation and upload processes
cannot accidentally call login or user-creation behavior. The REST API keeps
the complete `auth.Service`, which delegates token parsing to the same verifier
while retaining SIWE, JWT issuance, Redis challenges, and PostgreSQL users.

```text
REST API auth.Service
  -> issue JWT
  -> JWTVerifier.ParseToken for local validation

token-creation/upload services
  -> JWTVerifier.ParseToken only
```

Step 17 replaces the former shared HS256 secret with asymmetric keys.

## Step 17: asymmetric Ed25519 JWTs

JWT authority is now split by cryptographic capability:

```text
cmd/api
  -> JWT_PRIVATE_KEY_FILE
  -> EdDSA signs JWTs

cmd/token-creation-service and cmd/upload-service
  -> JWT_PUBLIC_KEY_FILE
  -> verify JWTs only
```

Generate development keys once:

```bash
./scripts/generate-dev-jwt-keys.sh
```

This writes an Ed25519 PKCS#8 private key and PKIX public key:

```text
.local-jwt/private.pem  # API only; never distribute to internal services
.local-jwt/public.pem   # safe to distribute to JWT-verifying services
```

The default paths already point there. Production deployments should mount
the private key only into `cmd/api` and mount the public key into token-creation
and upload services. Override paths when needed:

```bash
# REST auth API
JWT_PRIVATE_KEY_FILE=/run/secrets/jwt-private.pem go run ./cmd/api

# Protected internal services
JWT_PUBLIC_KEY_FILE=/run/config/jwt-public.pem go run ./cmd/token-creation-service
JWT_PUBLIC_KEY_FILE=/run/config/jwt-public.pem go run ./cmd/upload-service
```

Tokens now use EdDSA. Possession of the public key permits validation but
cannot produce a valid signature, so a compromised internal service can no
longer mint authentication tokens.
