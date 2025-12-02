# Environment Manager

A Git-based Docker container management platform. All configuration is stored as YAML files in a Git repository - no database required.

## Features

- **Container Management**: CRUD containers, start/stop/restart, view real-time logs
- **Volume Management**: Create volumes with automatic daily backups via Git LFS
- **Docker Compose Support**: Import and manage compose projects
- **Git-Based Persistence**: All state saved as files, automatically committed to Git
- **Networking**: Built-in Traefik reverse proxy + CoreDNS for subdomain-based access
- **Webhooks**: Automatic sync on Git push events

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│    CoreDNS      │     │    Traefik      │     │   Env Manager   │
│   (DNS Server)  │────▶│ (Reverse Proxy) │────▶│    (This App)   │
│   172.20.0.2    │     │   172.20.0.3    │     │                 │
└─────────────────┘     └─────────────────┘     └─────────────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │  Your Containers    │
                    │  *.your-domain.com  │
                    └─────────────────────┘
```

## Quick Start

1. **Clone and configure**
   ```bash
   git clone <your-repo>
   cd environment-manager
   cp .env.example .env
   # Edit .env to set your BASE_DOMAIN
   ```

2. **Start the stack**
   ```bash
   docker compose up -d
   ```

3. **Access the UI**
   - Manager: `http://manager.{BASE_DOMAIN}` (or `http://localhost:8080` in dev)
   - Traefik Dashboard: `http://traefik.{BASE_DOMAIN}:8081`

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `BASE_DOMAIN` | Base domain for all services | `localhost` |
| `GIT_REMOTE` | Git remote URL for sync | (none) |

### Data Directory Structure

```
data/
├── containers/          # Container configs (*.yaml)
├── volumes/             # Volume configs (*.yaml)
├── compose/             # Compose projects
├── network/
│   ├── config.yaml      # Network settings
│   └── Corefile         # CoreDNS config
├── state/
│   └── desired-state.yaml
└── backups/             # Volume backups (Git LFS)
```

## Development

### Prerequisites
- Go 1.22+
- Node.js 20+
- pnpm
- Docker

### Backend
```bash
cd backend
go mod download
go run ./cmd/server
```

### Frontend
```bash
cd frontend
pnpm install
pnpm dev
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/containers` | List containers |
| POST | `/api/v1/containers` | Create container |
| POST | `/api/v1/containers/:id/start` | Start container |
| POST | `/api/v1/containers/:id/stop` | Stop container |
| WS | `/ws/containers/:id/logs` | Stream logs |
| GET | `/api/v1/volumes` | List volumes |
| POST | `/api/v1/volumes/:name/backup` | Trigger backup |
| GET | `/api/v1/compose` | List compose projects |
| POST | `/api/v1/git/sync` | Sync from Git |
| POST | `/api/v1/webhook/github` | GitHub webhook |

## Webhooks

Configure your Git provider to send push events:

- **GitHub**: `POST /api/v1/webhook/github`
- **GitLab**: `POST /api/v1/webhook/gitlab`
- **Generic**: `POST /api/v1/webhook/generic`

## License

MIT
