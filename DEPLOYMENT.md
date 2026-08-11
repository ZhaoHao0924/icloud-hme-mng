# Deployment And Backup

This guide deploys iCloud HME with Docker Compose and protects the persistent data directory. The data directory contains account Cookies, App Passwords, platform-auth configuration, automation state, creation history, and operation logs. Treat it as secret material.

## Prerequisites

- Docker Engine with the Docker Compose plugin.
- A persistent host directory outside the repository for service data.
- A separate persistent host directory for backup archives.
- An API Token of at least 32 characters. Keep it in a secret manager or another protected location.

## Docker Compose Deployment

1. Set deployment variables in a local `.env` file based on `.env.example`. Do not commit `.env`.
2. Set `ICLOUD_HME_API_TOKEN` to a unique value of at least 32 characters.
3. Set `ICLOUD_HME_DATA_DIR` to an absolute, persistent host path in production. Keep it outside the repository.
4. Keep `ICLOUD_HME_BIND_ADDRESS=127.0.0.1` when a local reverse proxy handles external traffic. Set it to `0.0.0.0` only after configuring a firewall or reverse proxy.
5. Start the service:

```powershell
docker compose up -d --build
docker compose ps
```

The Compose health check calls `GET /api/auth/session`. It reports only platform-login status and does not expose account credentials. Open `http://127.0.0.1:8081` to create or sign in to the platform administrator account. The service itself listens on `0.0.0.0:8081` inside the container, so Compose always requires `ICLOUD_HME_API_TOKEN`.

Use the following commands for routine operations:

```powershell
docker compose logs --tail 200 app
docker compose restart app
docker compose down
```

`docker compose down` stops and removes the container but keeps the bind-mounted data directory. Do not use commands that remove the configured host data directory unless the data has been backed up and intentionally retired.

## Updating

For a command-by-command Linux Docker procedure, including offline `tar`
verification and image/data rollback, see
[LINUX_DOCKER_UPGRADE.md](LINUX_DOCKER_UPGRADE.md).

1. Pause any active alias automation rules in the Web UI.
2. Create and verify a backup as described below.
3. Update the source or image version in the deployment directory.
4. Run `docker compose up -d --build`.
5. Confirm `docker compose ps` reports the app as healthy, then sign in and inspect the account list and automation status.

## Offline Backup

Backups are intentionally offline. Stop the service before creating or restoring an archive so `accounts.json`, `platform-auth.json`, and logs represent a single point in time. The backup scripts never print file contents or credentials.

```powershell
docker compose stop app
.\scripts\backup-data.ps1 `
  -DataDir "D:\Services\icloud-hme\data" `
  -Destination "D:\Services\icloud-hme-backups" `
  -ConfirmServiceStopped
.\scripts\verify-data-backup.ps1 `
  -ArchivePath "D:\Services\icloud-hme-backups\icloud-hme-data-YYYYMMDDTHHMMSSZ.zip"
docker compose start app
```

Each ZIP contains a `backup-manifest.json` with a SHA-256 checksum and size for every archived data file. The verification script rejects missing, extra, duplicated, path-traversal, or checksum-mismatched entries. Store the archive in an access-controlled location separate from the service host when practical.

Use a maintenance window for scheduled backups: pause automation, stop the service, run backup and verification, then start the service. Retain several verified restore points according to the account recovery requirements. Do not run the offline scripts against a live data directory unless the service is known to be stopped.

## Restore

Restoring replaces the target data directory. The restore script requires two explicit confirmation switches and preserves the previous directory as a sibling rollback directory named `data.restore-before-<timestamp>`.

```powershell
docker compose stop app
.\scripts\restore-data.ps1 `
  -ArchivePath "D:\Services\icloud-hme-backups\icloud-hme-data-YYYYMMDDTHHMMSSZ.zip" `
  -DataDir "D:\Services\icloud-hme\data" `
  -ConfirmServiceStopped `
  -ConfirmRestore
docker compose start app
```

The archive is validated before extraction and the extracted files are validated again before replacing the target. After the service starts, sign in, inspect account and automation state, and retain the rollback directory until the restore has been accepted. A restored configuration can contain enabled automation rules; review and pause rules before starting the service when that would be unsafe.

## Script Smoke Test

The repository includes a credential-free smoke test that creates isolated temporary data, verifies a backup, restores it over stale data, and checks the retained rollback directory:

```powershell
.\scripts\backup-restore-smoke.ps1
```

The test never uses the configured service data directory. It complements, but does not replace, a Docker deployment check on the target host.
