# Sonicore Docker Deployment

## Quick Start

```bash
cd deploy
docker compose up -d
```

Wait for all services to become healthy, then open **http://localhost:28880**.

> The first time you register an account and scan your music library, you will need to mount your music directory (see below).

## Configuration

All settings are in `.env.example`, which is read by every container automatically. Edit the file directly and restart:

```bash
# Change JWT secret (required for production)
# Change database password
vim .env.example
docker compose restart
```

### Required Changes for Production

```bash
# Generate a secure JWT secret
openssl rand -hex 32
# → Paste into .env.example as SONICORE_JWT_SECRET
```

### Data Directories

The container expects two host directories mounted as volumes:

| Host path | Container path | Purpose |
|-----------|---------------|---------|
| Set via `MUSIC_DIR` | `/opt/sonicore/music` | Music files (read-only) |
| Set via `DATA_DIR` | `/opt/sonicore/data` | App data (images, cache) |

Default values (if not set in environment):

```bash
MUSIC_DIR=/opt/sonicore/music
DATA_DIR=/opt/sonicore/data
```

These can be overridden by creating a `.env` file alongside `.env.example`:

```bash
echo "MUSIC_DIR=/path/to/your/music" >> .env
echo "DATA_DIR=/path/to/data" >> .env
docker compose up -d
```

## Service Architecture

```
Host:28880 ──→ nginx:80 ──→ sonicore:4530 ──→ postgres:5432
                   │                              redis:6379
                   └── static files: web/dist
```

| Service | Internal port | Exposed | Purpose |
|---------|--------------|---------|---------|
| nginx | 80 | 28880 | Reverse proxy, static files |
| sonicore | 4530 | — | Go application server |
| postgres | 5432 | — | Database |
| redis | 6379 | — | Cache, session store |

## Building

```bash
# Build frontend first (required before first Docker build)
cd web && npm install && npm run build

# Build and start all services
cd deploy && docker compose up -d
```

To rebuild the application image after code changes:

```bash
cd deploy
docker compose build sonicore
docker compose up -d
```

## Directory Structure

```
deploy/
├── docker-compose.yml    # Service definitions
├── .env.example          # Shared environment variables
├── README.md             # This file
└── nginx/
    └── sonicore.conf     # nginx reverse proxy config
```

## Production Considerations

- **JWT Secret** — always regenerate with `openssl rand -hex 32`
- **Database Password** — change `POSTGRES_PASSWORD` / `SONICORE_DATABASE_PASSWORD`
- **Data Persistence** — ensure `DATA_DIR` points to a backed-up location
- **SSL Termination** — add a reverse proxy (e.g., Caddy, Traefik) in front of nginx for HTTPS
