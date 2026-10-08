# Hook22

![Go Version](https://img.shields.io/badge/go-1.27+-00ADD8?style=flat-square&logo=go)
![Docker Ready](https://img.shields.io/badge/docker-ready-2496ED?style=flat-square&logo=docker)
![License](https://img.shields.io/badge/license-Open--Core-emerald?style=flat-square)
![Architecture](https://img.shields.io/badge/architecture-Zero--Disk%20Stream-purple?style=flat-square)

**Hook22** is an open-core, high-performance SFTP-to-Webhook gateway designed for enterprise B2B data integrations. It bridges legacy SFTP file transfers with modern REST/API infrastructure through zero-disk streaming into cloud storage, in-flight OpenPGP decryption, dynamic multi-tenant authentication, and bi-directional outbound file pushes.

---

## Why Hook22?

- **Replaces AWS Transfer Family:** Eliminates $200+/month idle endpoint costs and complex S3 Event + Lambda decryption glue code.
- **Zero-Disk RAM Streaming:** Inbound files stream directly to cloud storage in memory without writing unencrypted data to host disks (simplifying SOC2 & HIPAA compliance).
- **Stateless & Database-Free:** Operates as a lean Go container with near-zero memory footprint and no required backing database.

---

## Key Features

- **Zero-Disk Staging:** Inbound SFTP streams are uploaded directly to target cloud storage backends in RAM with on-the-fly SHA-256 calculation.
- **Multi-Backend Storage Support:** Built-in driver abstraction for AWS S3, Google Cloud Storage (GCS), Azure Blob Storage, and Local Disk.
- **In-Flight OpenPGP Decryption:** Automatically decrypts encrypted incoming `.gpg` / `.pgp` streams before writing to cloud storage.
- **Dynamic Control Plane Authentication:** Validates client passwords and public keys via static YAML config or dynamic REST callback resolution.
- **Bi-Directional Gateway:** Accept inbound drop-box files via SFTP and trigger outbound SFTP transfers to partner servers via REST API.
- **Real-Time Observability:** Native Prometheus `/metrics` endpoint and Server-Sent Events (SSE) `/api/v1/logs/stream` log output.
- **Production Ready:** Built on Alpine Linux with graceful shutdown handling for active SSH sessions and pending webhook retries.

---

## Architecture Overview

```
[ External Partner ]
        |
    (SFTP Put)
        v
+-----------------------------------------------------------+
| Hook22 Gateway Server                                     |
|                                                           |
|   1. SSH Auth Callback -> (Static YAML / REST API)        |
|   2. In-Flight Reader  -> OpenPGP Decrypt + SHA256 Stream  |
|   3. Storage Driver    -> S3 / GCS / Azure / Disk       |
+-----------------------------------------------------------+
        |                                   |
  (Direct Stream)                 (Async Webhook Dispatch)
        v                                   v
[ Cloud Storage ]                   [ Internal API ]
```

---

## Quick Start (Docker Compose)

Run the entire local gateway stack alongside MinIO (S3 emulator) and a webhook receiver:

```bash
docker-compose up -d
```

### 1-Minute End-to-End Test
```bash
# 1. Boot the stack
docker-compose up -d

# 2. Upload a test file via SFTP (Password: e2e_password)
echo "id,amount\n1,100" > invoice.csv
sshpass -p "e2e_password" sftp -P 2222 -o StrictHostKeyChecking=no e2e_user@localhost <<< "put invoice.csv"

# 3. View the live JSON webhook received at http://localhost:3000
```

### Exposed Endpoints
| Service / Interface | Protocol | Port | Endpoint / Purpose |
|---|---|---|---|
| SFTP Gateway | SFTP / SSH | `2222` | Inbound partner file upload interface |
| Prometheus Metrics | HTTP | `8080` | `/metrics` |
| UI Authentication | HTTP | `8080` | `POST /api/v1/login` (Issues HttpOnly JWT Cookie) |
| Live SSE Log Stream | HTTP | `8080` | `/api/v1/logs/stream` (Protected) |
| Outbound Push API | HTTP | `8080` | `POST /api/v1/sftp/push` (Protected) |

---

## Configuration Reference (`config.yaml`)

```yaml
server:
  port: 2222
  host_key_path: "./keys/host_rsa"
  trusted_ca_path: "./keys/trusted_ca.pub"
  session_secret: "change_this_to_a_secure_random_string"

storage:
  driver: "s3" # Options: s3, gcs, azure, disk, mock
  s3:
    bucket: "my-sftp-drops"
    region: "us-east-1"
    endpoint: "http://minio:9000"
    access_key: "minioadmin"
    secret_key: "minioadmin"
  gcs:
    bucket: "my-gcs-bucket"
    credentials_file: "./gcp-creds.json"
  azure:
    container: "my-container"
    account_name: "myaccount"
    account_key: "mykey"
  disk:
    base_path: "./data"

pgp:
  enabled: true
  private_key_path: "./keys/pgp_private.asc"
  passphrase: "secret_passphrase"

webhook:
  url: "http://webhook-receiver:80/post"
  secret: "whsec_e2e_secret_999"
  workers: 5
  max_retries: 5
  base_backoff: 2

users:
  - username: "e2e_user"
    password: "e2e_password"
    public_keys:
      - "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ..."
```

---

## API Reference

### 1. Authenticate (Login)
Validates credentials against the static config or dynamic control plane and issues an `HttpOnly` JWT session cookie. This cookie is required for all subsequent API calls and SSE streams.

**Request:**
```http
POST /api/v1/login HTTP/1.1
Content-Type: application/json

{
  "username": "e2e_user",
  "password": "e2e_password"
}

**Response (`202 Accepted`):**
```json
Set-Cookie: hook22_auth=<jwt_token>; Path=/; HttpOnly; Secure; SameSite=Lax
Content-Type: application/json

{
  "status": "success"
}
```

### 2. Trigger Outbound SFTP Push
Pulls a file from storage and streams it to a remote partner's SFTP server:

*(Requires a valid `hook22_auth` session cookie).*

**Request:**
```http
POST /api/v1/sftp/push HTTP/1.1
Content-Type: application/json

{
  "remote_host": "sftp.partner.com:22",
  "username": "partner_user",
  "password": "partner_password",
  "source_key": "tenants/acme/2026/01/02/invoice.csv",
  "target_remote_path": "/incoming/invoice.csv"
}
```

**Response (`202 Accepted`):**
```json
{
  "partner": "sftp.partner.com:22",
  "status": "queued",
  "target": "/incoming/invoice.csv"
}
```

### 3. Inbound Upload Webhook Payload
Sent to your configured `webhook.url` after an upload completes and arrives in storage:

```json
{
  "event": "file.uploaded",
  "username": "e2e_user",
  "filepath": "/drops/invoice.csv",
  "size_bytes": 1048576,
  "sha256": "57bab3fee5406989fab22059700bd5ae3124eb107c285e4e0d466cdbf1df215c",
  "status": "success",
  "timestamp": "2026-10-06T15:00:00Z"
}
```

**Header Signature:** `Hook22-Signature: v1=<hmac_sha256_hex>`

---

## Development & Testing

Run unit and integration tests with coverage:

```bash
go test -v -cover ./...
```