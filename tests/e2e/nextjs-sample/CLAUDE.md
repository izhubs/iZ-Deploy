# izDeploy Architecture & AI Contract

Application: my-nextjs-app
Configured Internal Port: 3000

## Guidelines for AI Pair Programming
- Contract file: '.agent/izdeploy.json'
- Lockfile: '.agent/izdeploy.lock'

### Immutable Infrastructure Rules
- Never change internal container port (3000) or memory boundaries without developer instruction.
- Never edit or tamper with '.agent/izdeploy.lock' directly; use 'izdeploy lint' or 'izdeploy deploy --force'.

### Diagnostic Error Gating (RFC 7807)
- Errors returned by izDeploy CLI and MCP tools follow RFC 7807:
  - "agent_actionable: true" indicates application/code-level issues (syntax error, missing files). AI should fix.
  - "agent_actionable: false" indicates host-level issues (OOM 137, network port collision, proxy 502). AI must defer to user.
