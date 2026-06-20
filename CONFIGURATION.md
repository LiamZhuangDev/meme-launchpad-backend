# Local configuration

The configuration files committed to this repository are safe templates. They
must not contain passwords, RPC API keys, private keys, or cloud-storage
credentials.

To run the API locally:

```bash
cp apps/api/etc/api.yaml apps/api/etc/api.local.yaml
cd apps/api
go run . -f etc/api.local.yaml
```

Set real values only in `api.local.yaml`, which Git ignores. In particular,
configure database access, JWT signing, the RPC URL, token bytecode if needed,
the server signer key, and COS credentials.

To run the indexer locally:

```bash
cp apps/indexer/config/config.yaml apps/indexer/config/config.local.yaml
cd apps/indexer
go run ./cmd/indexer -config config/config.local.yaml
```

Set database and RPC/WebSocket values only in `config.local.yaml`, which Git
ignores.
