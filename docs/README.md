# Documentation

Documentation for running and integrating with a Metaxisdata deployment. The maintenance references an agent needs before changing code live in [.agents/docs/](../.agents/docs/README.md).

| Doc | Covers |
| --- | --- |
| [deploy.md](deploy.md) ([中文](deploy_zh.md)) | Building and running the container image, and every environment variable it reads. |
| [local-trial.md](local-trial.md) ([中文](local-trial_zh.md)) | The compose stack, for a throwaway instance on your own machine. |
| [mcp.md](mcp.md) | Connecting an MCP client to this deployment: the two workspace settings it needs, the client configuration, the authorization flow, the read-only tools it exposes and the operational notes. |
| [security-posture.md](security-posture.md) | The deliberate, accepted security and deployment decisions — each one a trade-off chosen for a self-hosted, single-database deployment. Read it before "fixing" behavior it describes. |
