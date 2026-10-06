# S3 Sync

A lightweight, self-hosted dashboard for backing up and restoring S3-compatible object storage — MinIO, SeaweedFS, AWS S3, Cloudflare R2, Backblaze B2, Wasabi, Garage, Ceph and anything else that speaks S3.

- **Incremental, deduplicated snapshots.** Unchanged objects are never re-downloaded; identical content is stored once across all snapshots.
- **Point-in-time restore.** Restore a whole snapshot, or browse it and restore individual files and folders. You can restore to the original bucket or anywhere else.
- **Optional client-side encryption.** Per job, AES-256-GCM with an Argon2id-derived key. Object contents, names and metadata are all encrypted before upload.
- **S3 or local-disk destinations**, zstd compression, cron schedules, and retention that keeps the last N snapshots and/or N days.
- **Health dashboard.** Shows healthy, failing and stale jobs, live progress, and full run logs.
- **Single small container.** One Go binary with the embedded React UI and SQLite. No external database.

## Deploy on Coolify

1. Push this repository to your Git provider.
2. In Coolify: **New resource → Application → your repo**. Pick **Docker Compose** as the build pack (it uses `docker-compose.yml`), or **Dockerfile**.
3. Set the environment variables:
   | Variable | Required | Description |
   |---|---|---|
   | `MASTER_KEY` | recommended | Encrypts stored S3 credentials and job passphrases. Use a long random value (`openssl rand -hex 32`). If unset, one is generated into `/data/.master_key`. |
   | `TZ` | no | Time zone used for cron schedules (default `UTC`). |
   | `SECURE_COOKIES` | no | `true` forces the `Secure` cookie flag. This isn't needed behind Coolify's proxy, which sends `X-Forwarded-Proto`. |
   | `PORT` | no | Listen port (default `8080`). |
4. Attach a domain and use port **8080**.
5. Make sure `/data` is a persistent volume. The compose file already declares it. If you plan to use "Local disk" storages, also mount `/backups`.
6. Deploy, open the domain and create the admin account.

The container runs as a non-root user and has a built-in healthcheck (`/s3sync healthcheck` → `GET /healthz`).

### Reaching MinIO / SeaweedFS on the same server

If the S3 service runs in Coolify too, connect both resources to the same Docker network. Then use the service name as the endpoint, e.g. `http://minio:9000` or `http://seaweedfs:8333`, with **path-style addressing** enabled.

## Using it

1. **Storages → Add storage.** Add the S3 service to back up. Then add a destination: another S3 service or bucket, or a **Local disk** path such as `/backups`. Use **Test connection** to check credentials.
2. **Backup jobs → New backup job.** Choose the source bucket and prefix, the destination bucket and prefix (the *repository*), a schedule, retention and encryption.
3. **Run backup now**, or wait for the schedule. Each run creates a snapshot.
4. **Restore.** Open a job and either **Restore** a snapshot or **Browse** it, select files and folders, and **Restore selected**. Restores can overwrite existing objects or skip them.

### Disaster recovery

The repository is self-describing. If you lose the S3 Sync server, deploy it again, create a job pointing at the same destination with the same passphrase if it was encrypted, and click **Scan repository**. All snapshots are imported and can be restored.

> If you enable encryption, keep the passphrase somewhere safe. Without it, the data cannot be decrypted.

## How backups are stored

```
<bucket>/<prefix>/
  config.json            repository id + wrapped encryption key (if encrypted)
  data/ab/ab12…          object contents, addressed by SHA-256 (HMAC-SHA256 when encrypted)
  snapshots/<id>         manifest: every key, size, ETag, mtime, content type, metadata → blob
```

- **Change detection.** An object is skipped when its size, ETag and modification time match the previous snapshot. Everything else is downloaded, hashed and uploaded only if that content isn't already in the repository.
- **Object format.** Every stored object starts with a 6-byte header. The payload is optionally zstd-compressed, then optionally encrypted in 64 KiB AES-GCM chunks (STREAM construction, which detects truncation and reordering).
- **Retention and pruning.** Retention deletes expired snapshot manifests. A prune step then deletes data that no remaining snapshot references. Several jobs can share one repository to deduplicate across buckets.
- **Integrity checks on restore.** Restores re-hash every object and fail it on a mismatch.

## Development

Requirements: Go 1.27+ and Node 22+.

```sh
# backend (http://localhost:8080, data in ./data)
go run ./cmd/s3sync

# frontend with hot reload (http://localhost:5173, proxies /api to :8080)
cd web && npm install && npm run dev

# production build: UI is embedded into the Go binary
cd web && npm run build && cd .. && go build -o s3sync ./cmd/s3sync

# tests (includes an end-to-end run against an in-process fake S3)
go test ./internal/...
```

Project layout:

```
cmd/s3sync/          entry point (+ `healthcheck` subcommand)
internal/storage/    S3 (minio-go) and local-disk backends
internal/repo/       repository format: blobs, manifests, encryption, prune
internal/engine/     backup / restore / delete / scan runs, snapshot browsing
internal/scheduler/  cron scheduling
internal/store/      SQLite persistence
internal/api/        REST API, auth, embedded UI serving
web/                 React + Vite + Tailwind UI
```
