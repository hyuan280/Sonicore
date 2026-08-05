# Sonicore

> 自托管音乐管理中心 · Self-hosted music management center

Sonicore 是一款自托管的音乐管理中心，提供服务端播放（Jukebox）、音乐库管理、多终端控制等核心功能。

Sonicore is a self-hosted music management center offering server-side playback (Jukebox), library management, and multi-terminal control.

> **状态 / Status**: 核心功能可用，部分高级功能正在开发中。  
> Core features are functional; some advanced features are under development.

---

## 特性 / Features

| 特性 | 状态 | 说明 |
|------|------|------|
| **🔊 Jukebox 服务端播放** | ✅ 完成 | ffplay + PulseAudio，多引擎并发，WebSocket 实时控制 |
| **🔍 音乐库管理** | ✅ 完成 | 扫描、分类、浏览歌曲/专辑/艺人，ffprobe 元数据解析 |
| **📋 播放列表** | ✅ 完成 | 创建/管理播放列表，收藏/历史记录，多选批量操作 |
| **👥 多用户与权限** | ✅ 完成 | super_admin / admin / user 三级角色 + 库级权限 |
| **📱 浏览器播放** | ✅ 完成 | React SPA 播放器，MSE 流式，循环/随机模式，队列管理 |
| **📦 Docker 部署** | ✅ 完成 | 一键 docker compose 启动，nginx 反向代理 |
| **🎵 元数据刮削** | ✅ 完成 | ffprobe 解析 + MusicBrainz enrichment 已接入扫描流程 |
| **📄 歌词支持** | ✅ 完成 | 多来源歌词（内嵌/侧边/网络/用户），LRC 解析，桌面歌词窗口 |
| **🎚️ 音频转码** | ✅ 完成 | 不支持的编码自动转码（AAC 256/320、FLAC），缓存 + 音质切换 |
| **🔀 多版本歌曲** | ✅ 完成 | 相同 MBID 归并，默认版本 + 版本切换（播放栏/队列），路径自动提取版本描述 |
| **⭐ 收藏与历史** | ✅ 完成 | 收藏（含多版本联动）、播放历史 |
| **🔄 WebSocket 同步** | 🚧 部分完成 | Jukebox 状态实时推送（浏览器播放器待补充） |
| **📱 Subsonic API** | 🚧 部分完成 | 12/40+ 端点已实现（ping, 浏览, 搜索, 流媒体等） |
| **🎵 多来源下载** | 📋 计划中 | 下载管理器框架就绪，仅支持 HTTP 直链；更多音源待接入 |**

---

## 架构 / Architecture

```
┌──────────────────┐ :2880 ┌──────────┐     ┌───────────┐     ┌────────────┐
│     Browser      │──────▶│          │────▶│           │────▶│ PostgreSQL │
│   (React SPA)    │       │          │     │           │     └────────────┘
└──────────────────┘       │          │     │ Go Server │
         │                 │          │     │  :4530    │     ┌────────────┐
         ▼                 │          │     │           │────▶│   Redis    │
┌──────────────────┐       │  nginx   │     │           │     └────────────┘
│    反代/FRP      │ :28880│ (docker) │     │  ffplay   │
│ (proxy protocol) │──────▶│          │     └───────────┘
└──────────────────┘       │          │
         ▲                 │          │
         │                 │          │
┌──────────────────┐ :2880 │          │
│ Subsonic         │──────▶│          │
│ Client           │       └──────────┘
└──────────────────┘
```

| 服务 / Service | 端口 / Port | 说明 / Purpose |
|---------------|-------------|----------------|
| nginx | 2880 (exposed) | 反向代理（直连 / 内网，不验证客户端 header） |
| nginx | 28880 (exposed) | 反向代理（前置带 proxy_protocol 的反代，信任 `X-Real-IP`） |
| sonicore | 4530 (internal) | Go API 服务 + WebSocket |
| PostgreSQL | 5432 (internal) | 主数据库 |
| Redis | 6379 (internal) | 缓存 / 会话存储 |

### 技术栈 / Tech Stack

**Backend**
- Go 1.26, gorilla/mux, gorilla/websocket
- PostgreSQL (lib/pq), Redis (go-redis/v9)
- JWT 认证, bcrypt 密码加密
- ffprobe 元数据解析, MusicBrainz API 刮削
- PulseAudio 音频设备管理, ffplay 服务端播放

**Frontend**
- React 19 + TypeScript 6 + Vite 8
- TailwindCSS v4 + Zustand 5 + React Router 7
- lucide-react 图标

---

## 快速开始 / Quick Start

### Docker 部署

```bash
# 1. 克隆仓库
git clone https://github.com/your-org/sonicore.git
cd sonicore

# 2. 构建前端
cd web && npm install && npm run build && cd ..

# 3. 启动所有服务
cd deploy
cp .env.example .env
# 编辑 .env，修改 JWT 密钥：openssl rand -hex 32
docker compose up -d
```

打开 **http://localhost:28880**，首次访问注册账号后即可使用。

### 非 Docker 部署

需要自行安装 Go 1.26+、Node.js 22+、PostgreSQL 16+、Redis。

```bash
# 后端
cp config.example.toml config.toml
# 编辑 config.toml 配置数据库等
go run ./cmd/sonicore

# 前端
cd web
npm install
npm run dev     # 开发模式
npm run build   # 构建生产版本
```

---

## 配置 / Configuration

### 环境变量 / Environment Variables

关键环境变量（用于 Docker），完整列表见 `deploy/.env.example`：

| 变量 | 说明 |
|------|------|
| `SONICORE_JWT_SECRET` | JWT 签名密钥（生产环境必改） |
| `SONICORE_DATABASE_*` | PostgreSQL 连接信息 |
| `SONICORE_REDIS_*` | Redis 连接信息 |
| `MUSIC_DIR` | 音乐目录（宿主机路径） |
| `DATA_DIR` | 数据目录（宿主机路径） |
| `SONICORE_AUDIO_PULSE_SERVER` | PulseAudio socket 路径 |

### 配置文件 / Config File

非 Docker 部署使用 `config.toml`（参考 `config.example.toml`），支持所有环境变量对应的配置项。

---

## 使用说明 / Usage

### 首次设置

1. 注册第一个账号（自动成为 super_admin）
2. 在设置页面添加音乐库并扫描
3. 或在 Settings → 添加设备（Jukebox 播放需要 PulseAudio 设备）

### Jukebox 服务端播放

Sonicore 支持在服务端直接播放音乐，通过 ffplay + PulseAudio 输出音频。

1. 确保宿主机已安装 PulseAudio
2. 在 `deploy/.env` 中配置 `SONICORE_AUDIO_PULSE_SERVER` 指向 PulseAudio socket
3. 通过 Web UI 创建 Jukebox 并控制播放

### Web UI 控制

- **歌曲/专辑/艺人** — 浏览和搜索音乐库
- **播放器** — 浏览器端播放，支持循环/随机模式、音质切换、多版本切换
- **播放列表** — 创建和管理播放列表
- **Jukebox** — 创建多个播放引擎，独立控制
- **管理** — 用户管理、权限控制、音乐库管理（含版本描述编辑）

### Subsonic API

兼容 Subsonic API（部分实现），可使用任意 Subsonic 客户端连接。

已实现端点：`ping`, `getLicense`, `getArtists`/`getIndexes`, `getArtist`, `getAlbum`, `getSong`, `getAlbumList`/`getAlbumList2`, `search2`/`search3`, `stream`, `getCoverArt`, `getPlaylists`, `scrobble`, `getNowPlaying`。

> 🚧 `scrobble` 和 `getNowPlaying` 为占位实现，更多端点持续添加中。

---

## 开发路线 / Roadmap

### 短期 / Short-term
- [ ] 热度系统（Heat 字段落地：播放次数/收藏等综合热度计算与展示）
- [ ] Subsonic API 完善（`star`/`unstar`, `getRandomSongs`, `jukeboxControl` 等）
- [ ] 浏览器播放器 WebSocket 状态同步
- [ ] 多版本 Work 聚合（Live/Remix 等不同录音归并）
- [ ] 播放队列跨设备同步

### 中期 / Mid-term
- [ ] 多来源下载扩展（YouTube, SoundCloud 等音源接入）
- [ ] 版本组用户自定义默认版本
- [ ] 移动端适配优化

### 长期 / Long-term
- [ ] 音乐推荐引擎
- [ ] 插件系统
- [ ] 国际化（i18n）

---

## 依赖 / Dependencies

### 后端 Go 依赖

| 包 | 用途 |
|----|------|
| gorilla/mux | HTTP 路由 |
| gorilla/websocket | WebSocket |
| lib/pq | PostgreSQL 驱动 |
| go-redis/v9 | Redis 客户端 |
| viper | 配置管理 |
| golang.org/x/crypto | bcrypt 密码加密 |

### 前端依赖

| 包 | 用途 |
|----|------|
| React 19, React DOM | UI 框架 |
| React Router v7 | 路由 |
| Zustand 5 | 状态管理 |
| TailwindCSS v4 | CSS 框架 |
| Vite 8 | 构建工具 |
| lucide-react | 图标 |
| class-variance-authority | UI 组件样式 |

---

## 项目结构 / Project Structure

```
sonicore/
├── cmd/sonicore/          # 入口 main.go
├── internal/
│   ├── api/
│   │   ├── middleware/     # 认证/权限/限流中间件
│   │   ├── rest/          # REST API 处理器
│   │   ├── subsonic/      # Subsonic API 兼容层
│   │   └── ws/            # WebSocket 中心
│   ├── config/            # 配置加载
│   ├── core/
│   │   ├── domain/        # 领域模型
│   │   ├── port/          # 接口定义
│   │   └── service/       # 业务逻辑
│   ├── infrastructure/
│   │   ├── auth/          # JWT / 密码
│   │   ├── cache/         # Redis 会话
│   │   ├── download/      # 下载管理器
│   │   ├── metadata/      # 元数据刮削
│   │   ├── player/        # ffplay 播放引擎
│   │   └── repository/    # 数据库实现
│   └── server/            # HTTP 服务配置
├── web/                   # React 前端
│   ├── src/
│   │   ├── api/           # API 客户端
│   │   ├── components/    # 公共组件
│   │   ├── pages/         # 页面
│   │   ├── stores/        # Zustand 状态
│   │   └── types/         # TypeScript 类型
│   └── public/            # 静态资源
├── deploy/                # Docker 部署
│   ├── docker-compose.yml
│   ├── .env.example
│   └── nginx/
├── Dockerfile
├── config.example.toml
└── docs/                  # 文档
```

---

## License

MIT
