# MEME Launchpad Backend Rebuild

This is an isolated, step-by-step rebuild of the backend in the sibling
`apps/api` and `apps/indexer` directories. It intentionally starts small:
each step must run and be understandable before the next responsibility is
introduced.

The original backend is not imported or modified by this project.

## Learning path

- [x] Step 1 — runnable HTTP foundation and health endpoint
- [ ] Step 2 — configuration and application dependencies
- [ ] Step 3 — PostgreSQL connection and first `users` repository
- [ ] Step 4 — wallet-signature login and JWT authentication
- [ ] Step 5 — token read APIs and database models
- [ ] Step 6 — token-creation intent, CREATE2 prediction, and signing
- [ ] Step 7 — separate blockchain event indexer
- [ ] Step 8 — event projections for tokens, trades, and K-lines
- [ ] Step 9 — Redis nonce cache and presigned image uploads

## Step 1: run the foundation

```bash
cd app-rebuild
go test ./...
PORT=38081 go run ./cmd/api
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
process. Step 2 will make configuration an explicit dependency.
