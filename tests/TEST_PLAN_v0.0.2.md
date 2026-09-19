# iZ-Deploy v0.0.2 Test Plan

## Phase 1: Core Survival
1. **zRAM & Swap**: Run `izdeploy init`. Verify that `setup-zram.sh` is executed or zRAM/Swap is configured to >= 2GB.
2. **Memory Limit**: Check default Nixpacks config when building. Verify `--memory-limit 1g` is injected.
3. **Automated GC**: Verify cronjob for `docker system prune -af --volumes --filter "until=168h"` is added.
4. **Worker Apps**: Provide `izdeploy.json` with `type: "worker"`. Verify Kamal-proxy is not configured and HTTP health checks are skipped.
5. **Ghost Containers**: Start an app, force stop it leaving a ghost. Start again. Verify the ghost container (`izdeploy.app=<name>`) is cleaned up.

## Phase 2: Production Safe
1. **Graceful Termination**: Stop an app. Verify Kamal-proxy waits and sends `SIGTERM` before `SIGKILL` (10-30s window).
2. **Cloudflare SSL**: Provision SSL with Cloudflare DNS proxy on. Verify DNS-01 challenge is used and succeeds.
3. **Secrets Management**: Run `izdeploy secret set`. Verify it loads from local `.env` and doesn't expose in `izdeploy.json`.
4. **Database Backup**: Run `izdeploy volume backup <name> --s3`. Verify backup tarball is created and uploaded to S3.

## Phase 3: Future & DX
1. **Monorepo**: Configure `workdir` in `izdeploy.json`. Build app. Verify only the subfolder is built.
2. **Private Submodules**: Pass `--build-arg SSH_KEY`. Verify build succeeds with private deps.
3. **Instant Rollback**: Deploy v1, deploy v2. Run `izdeploy rollback`. Verify traffic is routed back to v1.
4. **Logs Streaming**: Trigger Github Actions build. Verify local terminal streams the build logs.
