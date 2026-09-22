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
| **🔊 Jukebox 服务端播放** | ✅ 完成 | ffplay + PulseAudio/ALSA，多引擎并发，WebSocket 实时控制 |
| **🔍 音乐库管理** | ✅ 完成 | 扫描、分类、浏览歌曲/专辑/艺人，ffprobe 元数据解析 |
| **📋 播放列表** | ✅ 完成 | 创建/管理播放列表，收藏/历史记录，多选批量操作 |
| **👥 多用户与权限** | ✅ 完成 | super_admin / admin / user 三级角色 + 库级权限 |
| **📱 浏览器播放** | ✅ 完成 | React SPA 播放器，MSE 流式，循环/随机模式，队列管理 |
| **📦 Docker 部署** | ✅ 完成 | 一键 docker compose 启动，nginx 反向代理 |
| **🎵 元数据刮削** | ✅ 完成 | ffprobe 解析 + MusicBrainz / NetEase / 用户手动刮削 + 艺人头像自动获取 |
| **📄 歌词支持** | ✅ 完成 | 多来源歌词（内嵌/侧边/网络/用户），LRC 解析，桌面歌词窗口 |
| **🎚️ 音频转码** | ✅ 完成 | 不支持的编码自动转码（AAC 256/320、FLAC），缓存 + 音质切换 |
| **🔀 多版本歌曲** | ✅ 完成 | 相同 MBID 归并，默认版本 + 版本切换（播放栏/队列），路径自动提取版本描述 |
| **⭐ 收藏与历史** | ✅ 完成 | 收藏（含多版本联动）、播放历史 |
| **🔥 热度系统** | ✅ 完成 | 播放/完整播放/收藏/歌单加权，Valkey 防刷，热度徽标与排序 |
| **🧩 插件系统** | ✅ 完成 | go-plugin 子进程 + 插件市场（官方仓库同步、GitHub 下载） |
| **🔔 通知系统** | ✅ 完成 | 邮件 (SMTP/IMAP) 渠道 + 用户偏好 + 插件注册通道 |
| **⏰ 任务调度** | ✅ 完成 | 定时任务（转码缓存清理、插件仓库同步、限流器清理）+ 后台管理 |
| **🌐 外部音乐平台** | ✅ 完成 | NetEase 排行榜 / 搜索 / 详情（Discover 页面） |
| **🌍 国际化** | ✅ 完成 | i18next 中英文界面 |
| **🔄 WebSocket 同步** | 🚧 部分完成 | Jukebox 状态实时推送（浏览器播放器待补充） |
| **📱 Subsonic API** | 🚧 部分完成 | 30+ 端点已实现（浏览、搜索、流媒体、播放列表、用户、收藏等） |
| **🎵 多来源下载** | 📋 计划中 | 下载管理器框架就绪，仅支持 HTTP 直链；更多音源待接入 |

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
- ffprobe 元数据解析, MusicBrainz / NetEase API 刮削
- PulseAudio/ALSA 音频设备管理, ffplay 服务端播放
- hashicorp/go-plugin 插件系统, robfig/cron 定时任务

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

### 热度系统 / Heat

每首曲目有一个 `heat`（热度）分数，用于排序与展示。热度由用户行为事件累加得到，存储在 `track_events` 事件日志中，并缓存到 `tracks.heat` 聚合列（可随时按事件重算）。

**计分规则**

| 行为 | 权重 |
|------|------|
| 播放（有效收听） | +1 |
| 完整播放（听完） | 额外 +1 |
| 收藏 | +5 |
| 加入歌单 | +2（每个歌单每首曲目一次） |

- 取消收藏、移出歌单、删除歌单会**对称回退**对应热度；收藏/歌单重复添加不会重复计分。
- 下载权重已预留，但当前没有下载接口，故暂不计分。

**触发来源**

- 浏览器播放（`/api/user/history/add`，前端每 15 秒上报进度）
- Subsonic `scrobble`（需 `submission=true`）
- 服务端 Jukebox 播放（曲目结束/切歌/停止时结算）
- 收藏 / 歌单增删

**防刷（Valkey 限流 + 可信收听时长）**

- 服务端维护“可信累计收听时长”：只在真实流逝的墙钟时间内累加，快进不增加累计、从任意位置续播不会误判，短曲目按播放比例判定。
- 一次播放需累计 **≥30 秒**（或短曲目 ≥50% 时长）才计为一次播放；**完整播放**需累计 **≥90% 时长**（快进到结尾不算）。
- 每个 `(user, track)` 在每个窗口（`max(30s, 曲目时长 + 5s)`）内**最多计 1 次播放 + 1 次完整播放**，无法通过反复上报刷分。
- 播放上报要求曲目所属音乐库的访问权限；Jukebox 按引擎真实播放时长结算，跳过/秒切不计。

**展示**

- 歌曲标题后显示热度徽标（🔥 + 数值，颜色随数值从灰到红，100 为最红）。
- 歌曲页支持按热度 / 播放次数 / 最近播放排序。
- 专辑、艺人、歌单、收藏、历史、点唱机队列等列表均显示热度。

### Subsonic API

兼容 Subsonic API（部分实现），可使用任意 Subsonic 客户端连接。

已实现端点：

- 基础：`ping`、`getLicense`、`getScanStatus`、`startScan`
- 浏览：`getIndexes`/`getArtists`、`getMusicFolders`、`getArtist`、`getAlbum`、`getSong`、`getMusicDirectory`、`getAlbumList`/`getAlbumList2`、`getGenres`、`getArtistInfo`
- 搜索：`search2`/`search3`
- 流媒体：`stream`、`getCoverArt`
- 播放上报：`scrobble`（计入热度与播放历史）
- 播放列表：`getPlaylists`、`getPlaylist`、`createPlaylist`、`updatePlaylist`、`deletePlaylist`
- 用户：`getUser`、`getUsers`、`createUser`、`updateUser`、`deleteUser`、`changePassword`
- 收藏：`star`、`unstar`、`getStarred`
- Jukebox：`jukeboxControl`

> 🚧 `getNowPlaying`、`getChatMessages`、`getInternetRadioStations`、`getAvatar` 为占位实现，更多端点持续添加中。

---

## 开发路线 / Roadmap

### 短期 / Short-term
- [x] 热度系统（Heat 字段落地：播放/完整播放/收藏/歌单加权 + Valkey 防刷 + 展示排序）
- [ ] Subsonic API 完善（`getRandomSongs`、`getSongsByGenre`、播客等）
- [ ] 浏览器播放器 WebSocket 状态同步
- [ ] 多版本 Work 聚合（Live/Remix 等不同录音归并）
- [ ] 播放队列跨设备同步

### 中期 / Mid-term
- [ ] 多来源下载扩展（YouTube, SoundCloud 等音源接入）
- [ ] 版本组用户自定义默认版本
- [ ] 移动端适配优化

### 长期 / Long-term
- [ ] 音乐推荐引擎

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
| hashicorp/go-plugin | 插件系统（子进程 + gRPC） |
| robfig/cron/v3 | 定时任务调度 |
| golang.org/x/image | 封面图像处理 |
| gopkg.in/natefinch/lumberjack | 日志轮转 |

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
| i18next / react-i18next | 国际化 |
| clsx / tailwind-merge | 样式类合并 |

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
│   │   ├── cache/         # Redis 会话 / 令牌
│   │   ├── download/      # 下载管理器
│   │   ├── external/      # 外部平台客户端（netease）
│   │   ├── lyrics/        # 歌词文件存储
│   │   ├── logger/        # 日志
│   │   ├── metadata/      # 元数据刮削（MusicBrainz/NetEase）
│   │   ├── notification/  # 通知（邮件渠道）
│   │   ├── player/        # ffplay 播放引擎
│   │   ├── repository/    # 数据库实现
│   │   ├── scanner/       # 音乐库扫描
│   │   ├── secrets/       # 凭据静态加密
│   │   ├── ssrf/          # SSRF 防护
│   │   ├── task/          # 定时任务调度
│   │   └── transcoder/    # ffmpeg 转码
│   ├── plugin/            # 插件系统（go-plugin）
│   └── server/            # HTTP 服务配置
├── web/                   # React 前端
│   ├── src/
│   │   ├── api/           # API 客户端
│   │   ├── components/    # 公共组件
│   │   ├── hooks/         # 自定义 Hooks
│   │   ├── i18n/          # 国际化
│   │   ├── lib/           # 工具库
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
