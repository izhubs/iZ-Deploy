# iZ-Deploy: Core Strategy & Product Vision

This document outlines the strategic "North Star" for iZ-Deploy. All future features, UI decisions, and architectural shifts must align with these core philosophies.

## 1. Product Positioning: AI-Native, Not Web-Native
Unlike traditional self-hosted PaaS solutions (Coolify, Dokploy) that focus on mimicking Vercel's Web Dashboard for *human* operators, iZ-Deploy is built as the first **AI-Native Deployment Engine**.
- **The Audience:** Solo developers, extreme minimalists, and AI Coding Agents (Cursor, Claude, Windsurf).
- **The Value Proposition:** Turning the user's AI into an elite DevOps engineer, rather than forcing the user to become one via a Web UI.

## 2. Extreme Resource Efficiency (The 512MB Rule)
PaaS tools fail on cheap $4/mo VPS instances because they run heavy Control Planes (PostgreSQL, Redis, UI servers) and build code locally, leading to Out of Memory (OOM) kernel panics.
- **Rule:** The iZ-Deploy agent must never consume more than 20MB of RAM.
- **Rule:** Heavy builds must default to off-node providers (GitHub Actions) to protect the VPS.
- **Rule:** zRAM configuration is a built-in standard, not an afterthought.

## 3. Weaponizing the Decentralized AI Workforce (Growth Hack)
We do not have a massive team to maintain hundreds of app templates (WordPress, Supabase, Redis). Instead, we leverage the AI of our users.
- **Implementation:** Through standardized AI instruction files (e.g., `.cursor/rules/izdeploy.mdc`), we instruct the user's local AI agent to convert raw `docker-compose` files into iZ-Deploy templates.
- **The Viral Loop:** Once a user's AI creates or fixes a template, the AI is instructed to ask the user: *"Would you like me to open a PR to the iZ-Deploy repository to share this?"*.
- **Result:** A self-sustaining, community-driven template ecosystem built entirely by AI agents.

## 4. Hardware Pre-flight Checks (AI as System Architect)
AI agents often blindly follow user instructions, which can lead to server crashes (e.g., deploying a 2GB RAM app on a 512MB server).
- **Implementation:** Every template manifest must include a strict `requirements` block (`min_ram_mb`, `min_cpu_cores`).
- **Safety:** The MCP server enforces these checks *before* deployment. The AI is forced to read the hardware specs, realize the limitation, and warn the human user, acting as a true System Architect rather than a blind executor.

## 5. Physical Guardrails Against AI Hallucination
AI is prone to hallucination. Giving an AI root SSH access is a critical security vulnerability.
- **Implementation:** The `.agent/izdeploy.lock` file acts as a cryptographic, physical barrier. 
- **Safety:** The AI can modify the desired state (JSON), but iZ-Deploy will block the deployment of destructive infrastructure changes (modifying routing, exposing dangerous ports, deleting database volumes) unless the lockfile is explicitly bypassed by a human command.

## 6. The Scaling Ceiling (Scale Up, Not Scale Out)
iZ-Deploy is designed to be the undisputed king of **Single-Node** deployments. 
- **Small VPS (512MB):** Operates securely by offloading builds and using zRAM.
- **Large VPS (32GB+):** Performs exceptionally well because the <20MB agent leaves ~100% of resources for business logic. Heavy builds can be shifted to local execution (`build.mode: "host"`).
- **The Boundary:** We explicitly do NOT support horizontal multi-node clustering (like Kubernetes or Docker Swarm). If a user requires load balancing across multiple physical servers, they have outgrown iZ-Deploy. We solve for 99% of use cases, not the 1% enterprise scale.

## 7. The Responsibility of Worker Management
Background tasks (Celery, Sidekiq, BullMQ) are crucial for modern apps. Whose responsibility are they?
- **The Application's Job:** Writing the queue logic and processing the tasks.
- **iZ-Deploy's Job:** Keeping the worker container alive, injecting environment variables, and aggregating its logs.
- **Strategy:** Currently, iZ-Deploy is heavily HTTP/Web-focused (requiring a `port` and `route`). We MUST introduce a `type: "worker"` (or allow omitting the port) in our manifest. This prevents developers from hacking dummy HTTP servers just to keep a background job alive. Providing native worker support is essential for feature parity with Heroku/Railway.
