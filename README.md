# Terraform Registry Mirror

A fast, robust, and flexible self-hosted mirror for the Terraform Provider Registry and HashiCorp tool binaries. Designed for air-gapped, enterprise, and CI/CD environments, it supports advanced filtering, Prometheus metrics, and multiple deployment options.

---

## Features

- **Provider & Binary Mirroring**: Downloads and serves Terraform providers and HashiCorp CLI tools.
- **Advanced Filtering**: Filter by provider, version, platform, and tool.
- **Prometheus Metrics**: Built-in `/metrics` endpoint for monitoring.
- **Flexible Deployment**: Run as a binary, Docker container, or via Helm in Kubernetes.
- **Atomic Metadata**: Generates `index.json` and `.tf-mirror-metadata.json` compatible with Terraform.
- **TLS Support**: Optional HTTPS for secure serving.
- **Health & Version Endpoints**: For easy monitoring and automation.
- **Single-Pod Mode**: Downloader and server can run together with a shared data volume.
- **Multi-Stage Docker Build**: Optimized for smaller image size.
- **Proxy support**: HTTP/HTTPS/SOCKS5 proxy support.

---

## Architecture

```
+-------------------+         +-------------------+
|                   |         |                   |
|   Downloader      |         |     Server        |
|-------------------|         |-------------------|
| - Downloads       |  <----> | - Serves registry |
|   providers/tools |  /data  |   mirror API      |
| - Updates index   |  (PVC)  | - /metrics, etc.  |
+-------------------+         +-------------------+
```

- **Downloader**: Fetches providers and binaries, updates metadata.
- **Server**: Serves files via Terraform Registry API, exposes metrics and health endpoints.
- **Shared `/data`**: Both components use a shared persistent volume.

---

## Installation

### 1. Pre-built Binaries

```sh
wget https://github.com/kastna404/tf-mirror/releases/latest/download/tf-mirror-linux-amd64
chmod +x tf-mirror-linux-amd64
./tf-mirror-linux-amd64 --help
```

### 2. Build from Source

```sh
git clone https://github.com/kastna404/tf-mirror.git
cd tf-mirror
go build -o tf-mirror ./cmd/tf-mirror
./tf-mirror --help
```

### 3. Manual Docker Run

```sh
docker run --rm -v $(pwd)/data:/data docker.io/ademidovx/tf-mirror:latest \
  --mode downloader --download-path /data --provider-filter=hashicorp/aws
docker run --rm -p 8080:8080 -v $(pwd)/data:/data docker.io/ademidovx/tf-mirror:latest \
  --mode server --data-path /data --listen-port 8080
```

### 4. Using Docker Compose

```yaml
version: '3'
services:
  downloader:
    image: docker.io/ademidovx/tf-mirror:latest
    command: ["--mode", "downloader", "--download-path", "/data"]
    volumes:
      - ./data:/data
  server:
    image: docker.io/ademidovx/tf-mirror:latest
    command: ["--mode", "server", "--data-path", "/data", "--listen-port", "8080"]
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
```
Run:
```sh
docker-compose up
```

### 5. Helm (Kubernetes)

```sh
helm install tf-mirror oci://registry-1.docker.io/ademidovx/tf-mirror --version 0.1.0 \
  --set kind=Deployment \
  --set server.port=8080 \
  --set ingress.enabled=true
```
See `helm/values.yaml` for all options.

---

## Usage

### Download Providers and Serve

```sh
# Download AWS and Helm providers for linux_amd64 only
./tf-mirror --mode downloader --download-path ./data \
  --provider-filter=hashicorp/aws,hashicorp/helm \
  --platform-filter=linux_amd64

# Serve the mirror
./tf-mirror --mode server --data-path ./data --listen-port 8080
```

### Download HashiCorp Binaries

```sh
./tf-mirror --mode downloader --download-path ./data \
  --download-binaries="consul>1.21.3,terraform>1.6.0"
```

---

## Command Line Options

| Option                | Description                                                      |
|-----------------------|------------------------------------------------------------------|
| --mode                | `downloader` or `server`                                         |
| --download-path       | Directory for downloads (downloader mode)                        |
| --data-path           | Directory to serve (server mode)                                 |
| --provider-filter     | Comma-separated providers (e.g. `hashicorp/aws`)                 |
| --platform-filter     | Comma-separated platforms (e.g. `linux_amd64`)                   |
| --download-binaries   | Comma-separated tools (e.g. `terraform>1.6.0,consul>1.21.3`)     |
| --check-period        | Check interval in hours (downloader)                             |
| --max-attempts        | Max download attempts                                            |
| --download-timeout    | Timeout per download (seconds)                                   |
| --listen-host         | Server listen address                                            |
| --listen-port         | Server port (default: 80)                                        |
| --hostname            | Server hostname (optional)                                       |
| --enable-tls          | Enable HTTPS                                                     |
| --tls-crt             | TLS certificate path                                             |
| --tls-key             | TLS key path                                                     |
| --debug               | Enable debug logging                                             |
| --help                | Show help                                                        |
| --version             | Show version                                                     |

---

## Environment Variables

| Variable           | Description (same as CLI unless noted)         |
|--------------------|-----------------------------------------------|
| TF_MIRROR_MODE     | Mode: downloader/server                       |
| PROXY              | Proxy URL                                     |
| CHECK_PERIOD       | Check period                                  |
| DOWNLOAD_PATH      | Download path                                 |
| PROVIDER_FILTER    | Provider filter                               |
| PLATFORM_FILTER    | Platform filter                               |
| MAX_ATTEMPTS       | Max attempts                                  |
| DOWNLOAD_TIMEOUT   | Download timeout                              |
| DOWNLOAD_BINARIES  | Binaries filter                               |
| DATA_PATH          | Data path (server)                            |
| LISTEN_HOST        | Listen host                                   |
| LISTEN_PORT        | Listen port                                   |
| HOSTNAME           | Hostname                                      |
| ENABLE_TLS         | Enable TLS                                    |
| TLS_CRT            | TLS cert path                                 |
| TLS_KEY            | TLS key path                                  |
| DEBUG              | Debug logging                                 |

---

## Filtering Examples

- **By Provider:**
  ```
  --provider-filter=hashicorp/aws,hashicorp/helm
  ```
  Downloads only AWS and Helm providers.

- **By Platform:**
  ```
  --platform-filter=linux_amd64,darwin_arm64
  ```
  Downloads only for Linux AMD64 and Mac ARM64.

- **By Version:**
  ```
  --provider-filter=hashicorp/aws
  --download-binaries=terraform>1.6.0,consul>1.21.3
  ```
  Downloads only versions >= 1.6.0 for terraform, >= 1.21.3 for consul.

- **Combined:**
  ```
  --provider-filter=hashicorp/aws
  --platform-filter=linux_amd64
  --download-binaries=terraform>1.6.0
  ```

---

## .terraformrc Example

```hcl
provider_installation {
  network_mirror {
    url = "http://tf-mirror.local:8080/"
    include = ["registry.terraform.io/*/*"]
  }
  direct {
    exclude = ["registry.terraform.io/*/*"]
  }
}
```

---

## API Endpoints

| Endpoint         | Method | Description                                 |
|------------------|--------|---------------------------------------------|
| `/`              | GET    | Root, health info                           |
| `/metrics`       | GET    | Prometheus metrics                          |
| `/health`        | GET    | Health check (JSON)                         |
| `/version`       | GET    | Version info (JSON)                         |

---

## Example Environments

### Development

- Use `kind: Deployment`
- Enable debug logging
- Use NodePort or port-forward for access
- Example:
  ```yaml
  kind: Deployment
  server:
    port: 8080
  downloader:
    args:
      - --provider-filter=hashicorp/aws
      - --platform-filter=linux_amd64
    env:
      - name: DEBUG
        value: "true"
  ```

### Production

- Use `kind: StatefulSet`
- Enable ingress with TLS
- Use persistent storage (e.g., SSD)
- Example:
  ```yaml
  kind: StatefulSet
  server:
    port: 80
  ingress:
    enabled: true
    tls:
      enabled: true
      secretName: tf-mirror-prod-tls
  data:
    size: 100Gi
    storageClassName: fast-ssd
  ```

---

## Download Directory Structure

```
/data/
  ├── hashicorp/
  │   ├── aws/
  │   │   └── linux_amd64.zip
  |   |   └── index.json
  |   |   └── 5.0.0.json
  │   └── helm/
  │       └── ...
  ├── terraform/
  │   └── terraform_1.6.0_linux_amd64.zip
  └── .tf-mirror-metadata.json
```

- Each provider: `namespace/name/provider.zip`
- Each tool: `tool_name/tool.zip`
- Metadata: `.tf-mirror-metadata.json`, `index.json` per provider

---

## License

MIT

---

## Maintainer

Aleksei Demidov
