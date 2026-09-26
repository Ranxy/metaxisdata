# cli/AGENTS.md

`mxd`, the command line client. The root [AGENTS.md](../AGENTS.md) still applies.

- The CLI is a client of the API, never part of the server: `depguard` in `.golangci.yaml` blocks imports from anything under `backend/` except `backend/generated-go`. To share code, move it under `backend/generated-go` or duplicate the small piece — do not widen the allow list to reach the server's innards.
- It is a separate artifact from the server, built with `make build-cli` (`go build -ldflags "-w -s" -p=16 -o ./build/mxd ./cli`), not by the server builds.
- `cli/skill/SKILL.md` is the agent skill the CLI embeds (`//go:embed`) and installs with `mxd skill install`. Update it alongside any change to CLI behavior or flags.
- Go workflow, style and lint rules are the same as the rest of the repo — see [backend/AGENTS.md](../backend/AGENTS.md).
- The credential file and `METAXISDATA_SCOPES` handling are deliberate decisions, not bugs — see [docs/security-posture.md](../docs/security-posture.md).
