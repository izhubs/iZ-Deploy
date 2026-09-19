# [AI AGENT RULE]: Local Build & VPS Deployment via izDeploy

**Purpose**: Guide AI coding agents (Cursor, Windsurf, Claude Code) to build a project locally and deploy it to a remote VPS using izDeploy.

**Context**: You are an AI agent operating on a developer's local machine. The developer wants to deploy the current web application to their remote VPS. To avoid overloading small VPS servers (e.g. 1GB RAM) with heavy build processes, the compilation must occur locally, followed by a registry push and a webhook deployment trigger.

## Strict Operating Procedures (SOP)

### 1. Verify Configuration
- Inspect the `.agent/izdeploy.json` file in the project root.
- Extract the `image` field (e.g., `ghcr.io/username/myapp:latest`).
- *If the configuration is missing*, notify the developer and abort the deployment sequence.

### 2. Local Build Phase
- **DO NOT** execute builds directly on the VPS.
- Utilize `nixpacks` (preferred for zero-config) or `docker build` on the local machine.
- Example Nixpacks build:
  ```bash
  nixpacks build . --name myapp
  docker tag myapp <image_from_config>
  ```
- Example Dockerfile build:
  ```bash
  docker build -t <image_from_config> .
  ```

### 3. Registry Push Phase
- Verify authentication to the target container registry (e.g., `ghcr.io`, `docker.io`).
- Push the compiled OCI image:
  ```bash
  docker push <image_from_config>
  ```

### 4. Remote Deployment Trigger
- Invoke the `izdeploy-agent` on the remote VPS to execute a zero-downtime container swap.
- **Method A: via Local izDeploy CLI**
  ```bash
  izdeploy deploy --remote http://<VPS_IP>:8098 --token <WEBHOOK_SECRET>
  ```
- **Method B: via cURL Webhook**
  ```bash
  curl -X POST http://<VPS_IP>:8098/webhook/deploy \
    -H "Authorization: Bearer <WEBHOOK_SECRET>" \
    -H "Content-Type: application/json" \
    -d '{"app": "<app_name>", "image": "<image_from_config>"}'
  ```

## Diagnostic Gates & Error Handling
- **Authentication Failure**: If `docker push` outputs 401/403, halt and instruct the developer to execute `docker login`.
- **Webhook Unauthorized**: If the webhook returns HTTP 401, request the correct `WEBHOOK_SECRET` from the developer.
- **Port Conflicts/OOM**: If the webhook returns an RFC 7807 problem detail with `agent_actionable: false`, escalate to the developer (infrastructure issue).
