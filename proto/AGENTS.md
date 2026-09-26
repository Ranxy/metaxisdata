# proto/AGENTS.md

Protocol definitions for the ConnectRPC API and the stored row shapes. The root [AGENTS.md](../AGENTS.md) still applies.

- `proto/v1/` defines the public ConnectRPC services (package `metaxisdata.v1`); `proto/store/` defines the row shapes stored in database JSONB columns.
- `cd proto && buf generate` regenerates Go code into `backend/generated-go/`, frontend types into `frontend/src/types/proto-es/` (only `metaxisdata.v1`), and API docs into `proto/gen/grpc-doc/`.
- Generated output is committed but never hand-edited. Commit the regenerated `backend/generated-go/`, `frontend/src/types/proto-es/`, and `proto/gen/grpc-doc/` output together with the proto change.
- Follow AIPs at https://google.aip.dev/general. When AIP and the proto guide conflict, AIP takes precedence — for example, enum values use `HELLO`, not `TYPE_HELLO`.
- Adding or removing a field in a `proto/store` message changes a JSONB column's shape; update the store and the schema in the same change — see [backend/AGENTS.md](../backend/AGENTS.md) and [backend/migrator/AGENTS.md](../backend/migrator/AGENTS.md).

## Workflow

After any proto change:

```bash
buf format -w proto
buf lint proto
cd proto && buf generate
```

Then commit the regenerated output described above.
