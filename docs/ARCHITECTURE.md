# Sonicore 技术架构

## 1. 核心设计原则

1. **永不修改源文件** — 音频文件是只读的，元数据全量存于 PostgreSQL，绝不写入文件标签
2. **多用户 + 库共享** — 每个用户可拥有和共享多个音乐库，权限精细控制
3. **可扩展插件体系** — 元数据源、下载源均为接口驱动，方便后续扩展
4. **异步优先** — 扫描、下载等重操作走后台 Worker，API 始终保持低延迟

---

## 2. 技术选型

| 层级 | 技术 | 理由 |
|------|------|------|
| **后端语言** | Go 1.26 | 低资源、高并发流媒体、单二进制分发 |
| **HTTP 框架** | gorilla/mux | 轻量稳定，标准库兼容 |
| **数据库** | PostgreSQL 16 | 多用户、多库、复杂查询 |
| **数据库驱动** | lib/pq | 原生 PostgreSQL 驱动，无 ORM 抽象层 |
| **缓存/会话** | Redis (go-redis/v9) | 会话存储、流媒体令牌、缓存 |
| **配置管理** | viper | TOML 配置 + 环境变量覆盖 |
| **前端** | React 19 + TypeScript 6.0 + Vite 8.1 | 生态最强 |
| **UI 组件** | TailwindCSS v4 + Lucide React | 轻量可定制 |
| **状态管理** | Zustand 5 | 轻量、TS 友好 |
| **路由** | React Router v7 | SPA 路由 |
| **流媒体** | HTTP Range Requests（直传） | 零转码，浏览器原生播放 |
| **音频元数据** | ffprobe (静态二进制) | 只读读取，支持所有主流格式 |
| **服务端播放** | ffplay | 可选的 ALSA/PulseAudio 输出 |
| **容器化** | Docker + Docker Compose | 一键部署，PostgreSQL + Redis 开箱即用 |
| **基础镜像** | alpine:3.20 | 极小运行时 |

**关键 Go 库：**

| 功能 | 库 |
|------|-----|
| HTTP 路由 | `github.com/gorilla/mux` |
| PostgreSQL 驱动 | `github.com/lib/pq` |
| Redis 客户端 | `github.com/redis/go-redis/v9` |
| 配置加载 | `github.com/spf13/viper` |
| 密码哈希 | `golang.org/x/crypto/bcrypt` |
| JWT 认证 | `golang.org/x/crypto + 自定义` |
| 音频探测 | ffprobe 外部进程调用 |
| 音频播放 | ffplay 外部进程调用（服务端 jukebox） |

---

## 3. 整体架构（分层 + DDD）

```
+-------------------------------------------------------------------+
|                       客户端层 (Clients)                            |
|     Web UI (React)     Subsonic 客户端     API 调用                 |
+---------------------------+---------------------------------------+
                            | HTTP / WebSocket / Subsonic API
+---------------------------v---------------------------------------+
|                      API 层 (Transport)                            |
|   +----------+  +----------------+  +--------------------------+  |
|   | REST API |  | Subsonic API  |  | WebSocket (实时推送)      |  |
|   +----+-----+  +-------+-------+  +------------+-------------+  |
+--------+--------------+-----------------------+-------------------+
         |              |                       |
+--------v--------------v-----------------------v-------------------+
|                   应用服务层 (Application)                         |
|   +----------+ +----------+ +----------+ +-------------------+    |
|   | Library  | | Scanner  | | Download | | Auth              |    |
|   | Service  | | Service  | | Manager  | | (内置)            |    |
|   +----+-----+ +----+-----+ +----+-----+ +--------+----------+    |
|   +----------+ +----------+                                      |
|   | Metadata | | Player   |                                      |
|   | Resolver | | Engine   |                                      |
|   +----------+ +----------+                                      |
+--------+------------+------------+----------------+----------------+
         |            |            |                |
+--------v------------v------------v----------------v---------------+
|                      领域层 (Domain)                               |
|   +----------+ +----------+ +--------+ +-----------------------+   |
|   | 聚合根:   | | 值对象:  | | 仓库   | | 领域服务:             |   |
|   | User,     | | Genre,   | | 接口   | | (当前在 infrastructure |   |
|   | Library,  | | Path     | |        | |  层直接实现)          |   |
|   | Album,    | | Permission| |        | |                      |   |
|   | Artist,   | |          | |        | |                      |   |
|   | Track     | |          | |        | |                      |   |
|   +----------+ +----------+ +--------+ +-----------------------+   |
+--------------------------------------------------------------------+
                            |
+---------------------------v---------------------------------------+
|                     基础设施层 (Infrastructure)                     |
|   +--------+ +----------+ +----------+ +----------------------+   |
|   | lib/pq | | Download | | Metadata | | Storage              |   |
|   | +PGSQL | | Source   | | Provider | | (Local / 预留 S3)    |   |
|   +--------+ | (接口)   | | (ffprobe)| +----------------------+   |
|   +--------+ +----------+ +----------+ +----------------------+   |
|   | ffprobe | | Scanner  | | JWT Auth | | Player Engine        |   |
|   | +ffplay | | Engine   | | +bcrypt  | | (ffplay 服务端)      |   |
|   +--------+ +----------+ +----------+ +----------------------+   |
+--------------------------------------------------------------------+
```

---

## 4. 核心模块设计

### 4.1 元数据策略（永不修改源文件）

```
Metadata Pipeline (只读扫描):
  Raw File → ffprobe (JSON 输出) → 解析标签
           → 生成文件 SHA256 指纹
           → 入库 (绝不回写文件)

当前实现:
  - ffprobe 读取: 支持 mp3/flac/ogg/m4a/wav/dsf/aiff/opus 等
  - 嵌入式标签: title, artist, album, track, disc, year, genre
  - 扩展字段: composer, comment, MusicBrainz Track ID
  - 封面检测: 通过 stream.codec_type=video 识别内嵌封面

未来规划:
  - MusicBrainz Provider (MBID 精确匹配 → 文件名模糊匹配)
  - AcoustID Provider (Chromaprint 指纹 → AcoustID → MB)
  - 侧车文件 (sidecar) 写入 .sonicore/<file>.json

Image 存储策略:
  原则: 永不写入音频目录，文件系统缓存 + DB 元数据引用

  来源 (优先级递减):
    1. 音频文件内嵌封面图 (从 ffprobe 识别，由 cover.go 提取)
    2. 同目录 cover.jpg / folder.jpg (扫描时复制到缓存)
    3. 用户 Web UI 手动上传 (预留)

  存储路径: {images_dir}/{library_id}/
    文件命名: {type}_{owner_id}.{format}    (例: album_01HABCDE123.jpg)

  HTTP 服务:
    GET /api/images/:id  → 原图
    (缩略图预留，待实现)
```

### 4.2 下载管理（可扩展源接口）

```
DownloadSource 接口:
  type DownloadSource interface {
    Name() string
    Match(url string) bool
    Resolve(ctx, url) (*SourceInfo, error)
    Fetch(ctx, job *DownloadJob) error
  }

当前实现:
  - 基础框架: Create/List/Get/Cancel 下载任务 API
  - DownloadJob 持久化到 PostgreSQL
  - 下载管理器: 状态管理 (queued → running → completed/failed)

内置 Source:
  - 无内置实现，接口已定义

未来可能源:
  - DirectURLSource — HTTP 直链下载
  - YouTube/SoundCloud/Bandcamp — yt-dlp 包装
  - QobuzSource — API 集成

下载流程:
  用户提交 URL → 遍历 Source.Match() → 匹配源接管 →
  Resolve() 返回可用格式 → 用户选择 →
  Worker → Fetch() → PostProcess 刮削 → 导入 Library
```

### 4.3 多用户与库共享

```
User 模型:
  - ID, Username, Email, PasswordHash
  - 可拥有多个 Library
  - 可被邀请加入其他用户的 Library

Library 模型:
  - ID, Name, OwnerID, Path (文件系统路径)
  - MetadataStorageMode: database (当前仅支持此模式)
  - 独立的空间: 文件、Artist/Album/Track 数据

LibraryMember:
  - LibraryID, UserID, Role
  - Role: owner | admin | contributor | viewer

权限矩阵:
  ┌─────────────┬──────┬──────┬────────┬──────┐
  │ 操作         │ owner│ admin│contrib │viewer│
  ├─────────────┼──────┼──────┼────────┼──────┤
  │ 浏览/播放    │  ✓   │  ✓   │   ✓    │  ✓   │
  │ 编辑元数据   │  ✓   │  ✓   │   ✓    │  ✗   │
  │ 扫描/管理   │  ✓   │  ✓   │   ✗    │  ✗   │
  │ 管理成员     │  ✓   │  ✗   │   ✗    │  ✗   │
  │ 删除库       │  ✓   │  ✗   │   ✗    │  ✗   │
  └─────────────┴──────┴──────┴────────┴──────┘

API:
  POST   /api/libraries                   创建库
  GET    /api/libraries                   我的库列表
  GET    /api/libraries/:id               库详情
  DELETE /api/libraries/:id               删除库
  POST   /api/libraries/:id/members       邀请成员
  DELETE /api/libraries/:id/members/:uid  移除成员
  PUT    /api/libraries/:id/members/:uid  修改角色
```

### 4.4 扫描与监听 (Scanner)

```
扫描模式:
  1. 全量扫描 (用户触发，按 Library) — 已实现
  2. 增量扫描 (fsnotify) — 预留
  3. 定时扫描 (Cron 表达式) — 预留

Scanner Pipeline (Per Library):
  Walk Dir → 过滤音频文件 (按扩展名) →
  ffprobe 读取标签 (绝不写入) →
  计算文件 SHA256 →
  比对 DB:
    ├── 新文件 → 创建 Track + 关联 Artist/Album
    ├── 已变更 (hash) → 更新元数据
    └── 已删除 → 软删除 (保留用户标注，可恢复)

当前限制:
  - 同步执行 (不依赖消息队列)
  - 不支持增量扫描 (fsnotify watcher 待实现)
  - 进度通过 API 轮询获取
```

### 4.5 流媒体引擎 (Streaming)

```
Stream Request → 会话令牌认证 →
  → Track 查询 → 文件存在性校验 →
  → 直接输出: 原生格式，HTTP Range，零转码

Stream URL 格式:
  /api/s/{session}/{id}      — 基于会话令牌的流媒体

响应头:
  Content-Type: audio/{format}
  Content-Length: {file_size}
  Accept-Ranges: bytes
  (无转码，直接 http.ServeFile 输出)

客户端:
  浏览器 <audio> 标签原生播放
```

### 4.6 播放器 (Player)

Sonicore 提供两种播放模式：

**1. 浏览器播放 (Web UI，默认模式)**

```
前端 Zustand Store:
  - queue: 当前队列 (Track[])
  - currentIndex: 当前播放索引
  - mode: normal | repeat_all | repeat_one | shuffle
  - playEpoch: 每次播放切换递增，用于竞态保护

播放状态持久化:
  - localStorage: player state (queue, index, mode, volume)
  - PUT /api/user/settings: 服务端持久化播放状态
  - PUT /api/user/queue: 服务端持久化队列 (仅 Track IDs)

队列显示:
  - 始终按原始添加顺序显示 (display queue)
  - 播放顺序由 shuffleOrder + shuffleIdx 控制 (shuffle 模式)

Play Mode 循环:
  normal → repeat_all → repeat_one → shuffle → normal ...
```

**2. 服务端播放 (Jukebox API)**

```
Player Engine (ffplay 后台进程):
  - PlayQueue (FIFO / 循环 / 单曲)
  - Audio Output: 宿主系统的 ALSA/PulseAudio/PipeWire
  - 音量控制
  - WebSocket 实时推送状态

Jukebox API:
  GET  /api/jukebox/status      播放状态+当前队列
  POST /api/jukebox/play/{id}   播放指定曲目
  POST /api/jukebox/stop        停止
  POST /api/jukebox/next        下一曲
  PUT  /api/jukebox/volume      音量
  PUT  /api/jukebox/loop        循环模式
  GET  /api/jukebox/queue       查看队列
  POST /api/jukebox/queue       添加队列
```

---

## 5. 数据模型

```go
type User struct {
  ID           string
  Username     string
  Email        string
  PasswordHash string
  CreatedAt    time.Time
  UpdatedAt    time.Time
}

type Library struct {
  ID                    string
  Name                  string
  Path                  string
  OwnerID               string
  MetadataStorageMode   string    // "database" (当前仅此模式)
  ScanInterval          string    // Cron 表达式，预留
  LastScannedAt         *time.Time
  TrackCount            int
  Duration              float64
  CreatedAt             time.Time
  UpdatedAt             time.Time

  Owner   *User
  Members []*User
}

type LibraryMember struct {
  LibraryID string
  UserID    string
  Role      string    // "owner" | "admin" | "contributor" | "viewer"
  JoinedAt  time.Time
}

type Artist struct {
  ID           string
  LibraryID    string
  Name         string
  SortName     string
  MBID         string
  Biography    string
  CoverImageID *string    // 非空表示有封面，值为自身 ID（标记作用），封面文件路径由实体类型+ID 构造
  AlbumCount   int
  CreatedAt    time.Time
  UpdatedAt    time.Time
}

type Album struct {
  ID           string
  LibraryID    string
  Title        string
  ArtistID     string
  MBID         string
  Year         int
  Genre        string
  CoverImageID *string    // 非空表示有封面，值为自身 ID（标记作用），封面文件路径由实体类型+ID 构造
  SongCount    int
  Duration     float64
  CreatedAt    time.Time
  UpdatedAt    time.Time

  Artist *Artist
}

type Track struct {
  ID           string
  LibraryID    string
  Title        string
  AlbumID      string
  ArtistID     string
  TrackNumber  int
  DiscNumber   int
  Duration     float64
  BitRate      int
  SampleRate   int
  Channels     int
  FilePath     string
  FileSize     int64
  FileFormat   string    // mp3, flac, ogg, m4a, wav, dsf
  MBID         string
  AcoustID     string
  Hash         string    // SHA256 of audio data
  HasLyrics    bool
  Lyrics       string
  Heat         int       // 热度（待实现）
  PlayCount    int
  LastPlayedAt *time.Time
  Metadata     *TrackMetadata  // JSONB 扩展元数据
  CreatedAt    time.Time
  UpdatedAt    time.Time

  Album  *Album
  Artist *Artist
}

type TrackMetadata struct {
  Composer    string
  Conductor   string
  ISRC        string
  BPM         int
  Key         string
  Mood        string
  Grouping    string
  Comment     string
  MusicBrainz *MBMetadata
}

type MBMetadata struct {
  ArtistID       string
  AlbumID        string
  TrackID        string
  RecordingID    string
  WorkID         string
  ReleaseGroupID string
}

type Image struct {
  ID        string
  LibraryID string
  OwnerType string    // "album" | "artist"
  OwnerID   string
  Source    string    // "embedded" | "local_file" | "musicbrainz" | "lastfm" | "user_upload"
  Path      string
  Format    string    // "jpeg" | "png" | "webp"
  Width     int
  Height    int
  Size      int64
  Hash      string    // SHA256 去重
  Variants  ImageVariants  // JSONB 缩略图尺寸列表
  CreatedAt time.Time
  UpdatedAt time.Time
}

type Playlist struct {
  ID        string
  Name      string
  OwnerID   string
  IsPublic  bool
  TrackIDs  []string  // JSONB 曲目 ID 列表
  CreatedAt time.Time
  UpdatedAt time.Time
}

type DownloadJob struct {
  ID         string
  URL        string
  Source     string
  LibraryID  string
  Format     string
  Status     string    // queued | resolving | downloading | processing | completed | failed
  Progress   float64
  TargetPath string
  Metadata   string    // JSON
  Error      string
  CreatedAt  time.Time
  UpdatedAt  time.Time
}

type ScanJob struct {
  ID            string
  LibraryID     string
  Type          string    // full | incremental
  Status        string    // running | completed | failed
  TotalFiles    int
  Scanned       int
  NewTracks     int
  UpdatedTracks int
  DeletedTracks int
  Errors        string
  CreatedAt     time.Time
  CompletedAt   *time.Time
}

type RefreshToken struct {
  ID        string
  UserID    string
  TokenHash string
  ExpiresAt time.Time
  CreatedAt time.Time
}

type Favorite struct {
  UserID    string
  ItemType  string    // track | album | artist
  ItemID    string
  CreatedAt time.Time
}

type PlayHistory struct {
  ID       string
  UserID   string
  TrackID  string
  PlayedAt time.Time
}

type UserSetting struct {
  UserID string
  Key    string
  Value  string
}
```

---

## 6. API 设计

### 6.1 Subsonic API（兼容层）

位置: `internal/api/subsonic/handler.go`
- 基础 Subsonic API 兼容，路径前缀 `/rest`
- 支持多库（通过请求参数选择当前活动库）

### 6.2 Sonicore REST API

| 端点 | 功能 |
|------|------|
| `GET /ping` | 服务存活检查 |
| `GET /api/health` | 详细健康检查 |
| `POST /api/auth/register` | 注册 |
| `POST /api/auth/login` | 登录 |
| `POST /api/auth/refresh` | 刷新令牌 |
| `POST /api/auth/logout` | 登出 |
| `GET /api/user/me` | 当前用户信息 |
| `PUT /api/user/password` | 修改密码 |
| `POST /api/libraries` | 创建音乐库 |
| `GET /api/libraries` | 列出我的库 |
| `GET /api/libraries/:id` | 库详情 |
| `DELETE /api/libraries/:id` | 删除库 |
| `GET /api/libraries/:id/members` | 成员列表 |
| `POST /api/libraries/:id/members` | 添加成员 |
| `DELETE /api/libraries/:id/members/:userId` | 移除成员 |
| `PUT /api/libraries/:id/members/:userId` | 修改角色 |
| `POST /api/libraries/:id/scan` | 触发扫描 |
| `GET /api/libraries/:id/scan/status` | 扫描进度 |
| `GET /api/data/:libId/tracks` | 曲目列表 (分页) |
| `GET /api/data/:libId/artists` | 艺术家列表 (分页) |
| `GET /api/data/:libId/artists/:artistId` | 艺术家详情 |
| `GET /api/data/:libId/albums` | 专辑列表 (分页) |
| `GET /api/data/:libId/albums/:albumId` | 专辑详情 |
| `GET /api/s/:session/:id` | 流媒体 (会话令牌) |
| `GET /api/user/favorites` | 收藏列表 |
| `POST /api/user/favorites` | 添加收藏 |
| `DELETE /api/user/favorites/:type/:id` | 取消收藏 |
| `GET /api/user/history` | 播放历史 |
| `POST /api/user/history` | 记录播放 |
| `GET /api/user/playlists` | 播放列表 |
| `POST /api/user/playlists` | 创建播放列表 |
| `GET /api/user/playlists/:id` | 播放列表详情 |
| `DELETE /api/user/playlists/:id` | 删除播放列表 |
| `POST /api/user/playlists/:id/tracks` | 添加曲目到播放列表 |
| `DELETE /api/user/playlists/:id/tracks/:trackId` | 移除曲目 |
| `GET /api/user/settings` | 获取用户设置 |
| `PUT /api/user/settings` | 更新用户设置 |
| `GET /api/user/queue` | 获取播放队列 |
| `PUT /api/user/queue` | 保存播放队列 |
| `POST /api/libraries/:id/downloads` | 提交下载 |
| `GET /api/libraries/:id/downloads` | 下载列表 |
| `GET /api/libraries/:id/downloads/:jobId` | 下载详情 |
| `DELETE /api/libraries/:id/downloads/:jobId` | 取消下载 |
| `GET /api/jukebox/status` | 服务端播放状态 |
| `POST /api/jukebox/play/:id` | 服务端播放 |
| `POST /api/jukebox/stop` | 停止 |
| `POST /api/jukebox/next` | 下一曲 |
| `PUT /api/jukebox/volume` | 音量 |
| `PUT /api/jukebox/loop` | 循环模式 |
| `GET/POST/DELETE /api/jukebox/queue` | 管理服务端队列 |
| `DELETE /api/jukebox/queue/:index` | 移除队列项 |
| `POST /api/jukebox/shuffle` | 随机播放 |
| `PUT /api/jukebox/queue/set` | 设置队列 |

### 6.3 WebSocket

```
WS /ws?token=<jwt>

事件 (预留):
  scan.progress       { libraryId, scanned, total }
  scan.completed      { libraryId, new, updated, del }
  download.progress   { jobId, progress, status }
  download.done       { jobId, trackId }
  player.state        { trackId, status, position }
  jukebox.state       { trackId, status }
```

---

## 7. 项目目录结构

```
├── cmd/
│   └── sonicore/main.go                 入口
├── internal/
│   ├── api/
│   │   ├── rest/                        REST API handlers
│   │   │   ├── auth.go                  认证
│   │   │   ├── browse.go                浏览 (tracks/artists/albums)
│   │   │   ├── download.go              下载管理
│   │   │   ├── health.go                健康检查
│   │   │   ├── helpers.go               工具函数
│   │   │   ├── jukebox.go               服务端播放器
│   │   │   ├── library.go               库 CRUD + 成员管理
│   │   │   ├── scan.go                  扫描触发/状态
│   │   │   ├── stream.go                流媒体
│   │   │   ├── user.go                  用户管理
│   │   │   └── user_data.go             收藏/历史/播放列表/设置/队列
│   │   ├── subsonic/                    Subsonic API 兼容
│   │   │   └── handler.go
│   │   ├── ws/                          WebSocket hub
│   │   │   └── hub.go
│   │   └── middleware/                  中间件
│   │       ├── auth.go                  JWT 认证
│   │       ├── permission.go            权限检查
│   │       └── ratelimit.go             限流
│   ├── config/                          配置加载
│   │   └── config.go
│   ├── core/
│   │   ├── domain/                      实体、值对象
│   │   │   └── domain.go                所有数据模型
│   │   ├── port/                        接口定义
│   │   │   ├── auth.go
│   │   │   ├── download.go
│   │   │   ├── metadata.go
│   │   │   ├── player.go
│   │   │   └── repository.go
│   │   └── service/                     应用服务
│   │       └── scanner_service.go       扫描编排
│   ├── infrastructure/
│   │   ├── auth/                        认证基础设施
│   │   │   ├── jwt.go
│   │   │   └── password.go
│   │   ├── cache/                       缓存基础设施
│   │   │   ├── session.go
│   │   │   ├── token_store.go
│   │   │   └── valkey.go
│   │   ├── download/                    下载管理器
│   │   │   ├── manager.go
│   │   │   └── source.go
│   │   ├── metadata/                    元数据引擎
│   │   │   ├── cover.go                 封面提取/缓存
│   │   │   ├── ffprobe.go               ffprobe 调用
│   │   │   ├── musicbrainz.go           MusicBrainz 查询 (预留)
│   │   │   └── resolver.go              编排各 Provider
│   │   ├── player/                      服务端播放
│   │   │   └── engine.go                ffplay 进程管理
│   │   ├── repository/                  数据仓库实现
│   │   │   ├── album_repo.go
│   │   │   ├── artist_repo.go
│   │   │   ├── db.go                    数据库连接 + 迁移
│   │   │   ├── download_repo.go
│   │   │   ├── image_repo.go
│   │   │   ├── library_repo.go
│   │   │   ├── playlist_repo.go
│   │   │   ├── scan_repo.go
│   │   │   ├── track_repo.go
│   │   │   └── user_repo.go
│   │   ├── scanner/                     扫描器
│   │   │   └── scanner.go
│   │   ├── search/                      全文搜索 (预留)
│   │   ├── sidecar/                     侧车文件 (预留)
│   │   └── storage/                     文件存储抽象 (预留)
│   └── server/                          依赖组装
│       └── server.go
├── pkg/
│   └── utils/                           工具包 (预留)
├── web/                                 React 前端
│   ├── src/
│   │   ├── main.tsx
│   │   ├── App.tsx
│   │   ├── index.css                    全局样式 (Tailwind)
│   │   ├── api/
│   │   │   └── client.ts               API 客户端
│   │   ├── components/
│   │   │   ├── AddToPlaylist.tsx
│   │   │   ├── PlayerBar.tsx           底部播放栏
│   │   │   └── ui/                     基础 UI 组件
│   │   │       ├── button.tsx
│   │   │       ├── card.tsx
│   │   │       └── input.tsx
│   │   ├── lib/
│   │   │   └── utils.ts                工具函数
│   │   ├── pages/
│   │   │   ├── LoginPage.tsx
│   │   │   ├── AlbumsPage.tsx
│   │   │   ├── AlbumDetailPage.tsx
│   │   │   ├── ArtistsPage.tsx
│   │   │   ├── ArtistDetailPage.tsx
│   │   │   ├── SongsPage.tsx
│   │   │   ├── PlaylistsPage.tsx
│   │   │   ├── PlaylistDetailPage.tsx
│   │   │   ├── FavoritesPage.tsx
│   │   │   ├── HistoryPage.tsx
│   │   │   ├── PlayerPage.tsx
│   │   │   └── SettingsPage.tsx
│   │   ├── stores/
│   │   │   ├── auth.ts                  认证状态
│   │   │   ├── library.ts              库选择状态
│   │   │   └── player.ts               播放器状态 (Zustand)
│   │   └── types/
│   │       └── index.ts                TypeScript 类型定义
│   ├── dist/                            构建产物
│   ├── index.html
│   ├── vite.config.ts
│   └── package.json
├── deploy/
│   ├── docker-compose.yml               Docker Compose 编排
│   ├── .env.example                     环境变量模板
│   ├── README.md                        部署说明
│   └── nginx/
│       └── sonicore.conf                nginx 反向代理配置
├── docs/
│   └── ARCHITECTURE.md                  本文档
├── Dockerfile                           多阶段构建
├── config.example.toml                  配置模板
├── config.toml                          (本地配置，已 gitignore)
├── go.mod
├── go.sum
└── README.md
```

---

## 8. 数据流：一次完整的用户体验

### 场景 A: 新建库 + 扫描

```
用户注册/登录 → POST /api/libraries { name: "我的音乐", path: "/music" }
  → Library 创建 → 返回 libraryId
  → POST /api/libraries/:id/scan
  → Scanner Engine 启动
  → Walk /music → ffprobe 读取 (只读!)
  → 新文件 → 创建 Track + Artist + Album → 入库
  → 轮询 GET /api/libraries/:id/scan/status 获取进度
  → Web UI 显示库内容
```

### 场景 B: 共享库

```
用户 A: POST /api/libraries/:id/members { userId: "B", role: "viewer" }
  → LibraryMember 创建
  → 用户 B: GET /api/libraries → 看到 A 的库
  → 用户 B: 浏览、播放 (但不能编辑/扫描)
```

### 场景 C: 浏览器播放

```
用户点击曲目 → Zustand Player Store 更新队列
  → <audio> 设置 src = /api/s/{session}/{trackId}
  → HTTP Range 请求 → 服务端直接输出文件
  → 浏览器原生解码播放
  → 播放状态持久化: localStorage + PUT /api/user/settings
  → 队列持久化: PUT /api/user/queue
```

### 场景 D: 服务端 Jukebox 播放

```
用户点击"服务端播放" → POST /api/jukebox/play/{id}
  → Player Engine: 启动 ffplay 进程
  → 音频输出到宿主机声卡
  → WebSocket 推送状态变更
```

---

## 9. 开发路线

| 阶段 | 内容 | 状态 |
|------|------|------|
| **P0** | Go module + 目录结构 + 配置 + 数据库迁移 + 入口 | ✅ 已完成 |
| **P1** | User + Library + JWT 认证 | ✅ 已完成 |
| **P2** | Scanner + TagParser + 基础数据模型入库 | ✅ 已完成 |
| **P3** | Subsonic API 核心 + REST 查询 | ✅ 已完成 |
| **P4** | Web UI 基础 (库管理 + 专辑浏览 + 播放) | ✅ 已完成 |
| **P5** | MusicBrainz 刮削 + 元数据编辑 | 🔄 进行中 |
| **P6** | 库共享 + 权限系统 | ✅ 已完成 |
| **P7** | 下载管理器 + Jukebox 播放接口 | ✅ 已完成 |
| **P8** | 全文搜索 + 高级播放列表 + 播客 | ⏳ 待开发 |
| **P9** | WebSocket 实时推送 + 多端同步 | ⏳ 待开发 |

---

## 10. 容器化部署

```
Docker Compose 架构 (port 28880 对外):

  Host:28880 → nginx:80 → sonicore:4530
                         → postgres:5432 (内部)
                         → redis:6379 (内部)

Dockerfile:
  Stage 1 (builder):      golang:1.26-alpine → 编译静态二进制
  Stage 2 (runtime):      alpine:3.20 → ffmpeg + 二进制 + web/dist

运行时目录结构 (容器内):
  /opt/sonicore/
    ├── bin/sonicore        主程序
    ├── web/               前端静态文件
    ├── music/             音乐文件 (只读挂载)
    └── data/
        ├── images/        封面缓存
        └── cache/         其他缓存

环境变量配置 (SONICORE_ 前缀):
  SONICORE_SERVER_HOST / PORT / WEB_DIR
  SONICORE_DATABASE_HOST / PORT / USER / PASSWORD / DBNAME
  SONICORE_REDIS_HOST / PORT / PASSWORD / DB
  SONICORE_DATA_MUSIC_DIR / DATA_DIR / IMAGES_DIR / CACHE_DIR
  SONICORE_JWT_SECRET / EXPIRATION
```

---

## 11. 参考项目

- **Navidrome** — Go + React + Subsonic API，核心理念
- **MusicBrainz / Picard** — 元数据工作流、MBID 体系
- **Jellyfin** — 多用户、库管理、权限系统设计参考
- **Feishin / Sonixd** — 现代音乐客户端 UI/UX
- **Lidarr** — 音乐库自动化管理参考
