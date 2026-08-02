# 音乐数据模型分析

> 基于当前代码实现 (`internal/core/domain/domain.go` + `internal/infrastructure/repository/db.go`) 的现状分析。
> 日期: 2026-08-02

---

## 1. 歌曲封面：单曲是否有独立封面？

### 现状 ✅ 已完成

**Track 有独立的封面字段。**

- `Track` 结构体 (`domain.go:103`) — `CoverImageID *string`
- `Album` (`domain.go:82`) / `Artist` (`domain.go:68`) — 均有 `CoverImageID`
- 扫描器通过 `CoverExtractor` 从音频文件提取内嵌封面，写入 `images` 表 + 缩略图
- `cover.go` 提供 `/api/data/cover` 端点，支持 `track` / `album` / `artist` 类型

---

## 2. 同一首歌出现在不同专辑

### 现状 ✅ 已完成

**通过 `track_albums` 多对多关联表实现。**

```sql
CREATE TABLE IF NOT EXISTS track_albums (
    track_id      VARCHAR(26) NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    album_id      VARCHAR(26) NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    track_number  INTEGER NOT NULL DEFAULT 0,
    disc_number   INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (track_id, album_id)
);
```

- 一首歌可关联多张专辑（合辑、精选集、原声带）
- 每张专辑内有独立的 `track_number` / `disc_number`
- 扫描器、专辑详情、歌手详情均已适配

---

## 3. 歌曲不同版本（不同歌手/专辑/版本）

### 现状 ✅ 已完成

**通过 MBID 分组 + `track_version_groups` 关联表实现。**

- `tracks.mbid`（Recording MBID）作为分组键
- `tracks.version` 字段语义：
  - `0` — 独版（无其他版本），列表正常显示
  - `1` — 多版本组中的默认版本（列表显示）
  - `2+` — 非默认版本（列表隐藏，可通过 ID 访问）
- `tracks.version_label` — 版本描述，从文件路径自动提取
- `track_version_groups (mbid, library_id, track_id)` 关联表，跨库记录同 MBID 的 track

```sql
CREATE TABLE IF NOT EXISTS track_version_groups (
    mbid       VARCHAR(36) NOT NULL,
    library_id VARCHAR(26) NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    track_id   VARCHAR(26) NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    PRIMARY KEY (mbid, track_id)
);
```

### 版本描述提取规则（`scanner.ExtractVersionLabel`）

| 优先级 | 规则 | 示例 |
|--------|------|------|
| 1 | 关键字匹配（live/acoustic/remaster/deluxe...） | `Deluxe · FLAC 1411kbps` |
| 2 | 路径分词，剔除歌名/专辑/艺人/年份/扩展名 | `Gold, Remaster · FLAC 900kbps` |
| 3 | 兜底 | `FLAC 900kbps` |

### 前端交互

- 浏览列表只显示默认版本，附加 `versions` 数组
- 添加队列按钮（`AddQueueBtn`）：多版本时弹出候选列表
- 播放栏版本切换按钮（`V1`）：播放中即时切换版本
- 管理界面可编辑 `version_label`；MBID 修改后自动重排版本组

---

## 4. 艺术家角色分类

### 现状 ✅ 已完成

**通过 `track_artists` 多对多关联表实现角色分类。**

```sql
CREATE TABLE IF NOT EXISTS track_artists (
    track_id   VARCHAR(26) NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    artist_id  VARCHAR(26) NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    role       VARCHAR(30) NOT NULL DEFAULT 'performer',
    sort_order SMALLINT NOT NULL DEFAULT 0,
    PRIMARY KEY (track_id, artist_id, role)
);
```

- 角色枚举：`performer` / `album_artist` / `composer` / `lyricist` / `arranger` / `producer` / `conductor` / `remixer`
- `TrackArtists []*TrackArtist` 支持一首歌多个艺人、多个角色
- 前端 `ArtistLink` 按角色展示，歌手详情页按角色分组显示

---

## 5. 多格式/多品质文件（FLAC + MP3）

### 现状 🚧 部分解决（由版本分组覆盖）

**同一首歌的不同格式文件仍是独立 Track 记录，但已通过版本分组关联。**

- `album/01-song.flac` 和 `album/01-song.mp3`（同 MBID）：
  - 两条 Track 记录，不同路径、哈希、格式、位率
  - 通过 `resolveVersions` 归并为同一版本组
  - 默认选择高品质格式（flac > alac > wav > aiff > mp3 > ...）
  - 播放时可通过版本切换选择任意格式

### 遗留差异 vs 原始 `track_files` 方案

原始方案设想"逻辑曲目一个、物理文件多个"（`track_files` 子表）。当前实现是**每文件一条 track + 版本分组**：

| 维度 | track_files 方案 | 版本分组方案（当前） |
|------|-----------------|---------------------|
| 存储 | 1 track + N files | N tracks |
| 播放统计 | 统一 | 各版本独立 |
| 收藏 | 统一 | 收藏自动扩展全部版本 |
| 元数据 | 共享 | 各版本可独立编辑（标题/封面等） |

两方案各有取舍，当前版本分组方案更灵活（版本可带独立描述），且已实现。

---

## 综合路线状态

| 优先级 | 项目 | 复杂度 | 状态 |
|--------|------|--------|------|
| P0 | 封面完整链路（提取+存储+API+展示） | 中 | ✅ 已完成 |
| P1 | 多品质文件合并（track_files） | 中 | 📋 被版本分组方案取代 |
| P2 | 多专辑关联（track_albums） | 低 | ✅ 已完成 |
| P3 | 多角色贡献者（track_artists） | 中 | ✅ 已完成 |
| P4 | Work/版本归并（MBID 分组） | 高 | ✅ 已完成 |

---

## 已知限制 / 未来方向

- **MBID 为空时**：无法分组，同歌不同版本（无 MBID）仍显示为多首独立歌曲
- **Work 概念缺失**：当前按 Recording MBID 分组，Live/Remix 等不同录音（不同 MBID）不会归并为同一 Work。若需聚合，可后续引入 `works` 表
- **版本组默认选择**：目前固定按格式+位率排序，用户不可自定义偏好
- **播放列表引用非默认版本**：保留原始 track ID，不自动转换

---

# 完整数据模型总览

> 以下覆盖音乐实体之外的其余数据库表（`internal/infrastructure/repository/db.go` 全量 schema）。

## 6. 用户与权限

### `users`
| 字段 | 类型 | 说明 |
|------|------|------|
| id | VARCHAR(26) PK | 雪花 ID |
| username | VARCHAR(64) UNIQUE | 登录名 |
| email | VARCHAR(255) UNIQUE | 邮箱 |
| password_hash | VARCHAR(255) | bcrypt 哈希 |
| role | VARCHAR(20) | `super_admin` / `admin` / `user`（全局角色） |
| created_at / updated_at | TIMESTAMPTZ | |

### `library_members` — 库级权限
| 字段 | 说明 |
|------|------|
| library_id / user_id | 联合主键 |
| role | `owner` / `admin` / `contributor` / `viewer` |

权限模型分两层：
- **全局角色**（`users.role`）：系统级能力
- **库角色**（`library_members.role`）：对某个媒体库的访问级别，通过 `middleware.PermissionChecker` 校验（`HasRole`/`IsMember`）

## 7. 媒体库

### `libraries`
| 字段 | 说明 |
|------|------|
| id / name / path | 基本信息 |
| owner_id | 所有者（引用 users） |
| metadata_storage_mode | `database` / `file`（默认 database，元数据写 DB） |
| scan_interval | 定时扫描间隔 |
| last_scanned_at / last_scan_errors | 最近扫描状态 |
| track_count / duration | 冗余统计（扫描后更新） |

## 8. 图片存储

### `images` — 泛化封面/图片存储
| 字段 | 说明 |
|------|------|
| owner_type / owner_id | 多态归属：`track` / `album` / `artist` |
| source | 来源（embedded 等） |
| path / format / width / height / size / hash | 文件元信息 |
| variants | JSONB，缩略图变体（如 64px） |

物理文件存于 `{data_dir}/images/{library_id}/`，DB 仅记录元数据。

## 9. 播放列表

### `playlists`
| 字段 | 说明 |
|------|------|
| id / name / owner_id | 基本信息 |
| is_public | 是否公开 |
| **track_ids** | **JSONB 数组**，直接存 track ID 列表（非关联表） |

设计说明：使用 JSONB 数组而非多对多关联表，换取写入简单（无需事务批量插入），代价是：
- 无法用外键约束保证引用完整性
- 删除 track 时需扫描所有 playlist 的 JSONB 做清理（`library.go:166` 已有清理逻辑）
- 播放列表可能引用非默认版本 track（保留原 ID，不自动转换）

## 10. 用户行为数据

### `favorites` — 收藏
| 字段 | 说明 |
|------|------|
| user_id / item_type / item_id | 联合主键，`item_type` = `track` / `album` / `artist` |
| library_id | 冗余，用于库级清理 |

**多版本联动**：收藏/取消收藏 track 时，通过 `expandTrackVersions` 扩展到同 MBID 的全部版本（仅限用户有权限的库）；列表展示时 `DISTINCT ON (mbid)` 去重，只显示默认版本。

### `play_history` — 播放历史
| 字段 | 说明 |
|------|------|
| id / user_id / track_id | 每次播放记录一条 |
| library_id | 冗余 |
| played_at | 默认 NOW()，索引 `(user_id, played_at DESC)` |

### `user_settings` — 用户设置 KV
| 字段 | 说明 |
|------|------|
| user_id / key | 联合主键 |
| value | 文本值 |

用途：播放器状态同步（`player_state` key 存 JSON）、队列持久化（`player_queue` key）等。

### `user_metadata` — 用户自定义元数据
| 字段 | 说明 |
|------|------|
| user_id / file_hash | 联合主键 |
| track_mbid / title / artist / album / album_artist / track_number / disc_number / year / genre | 用户手工纠正的元数据 |

设计：按**文件哈希**而非 track ID 关联，扫描时以用户元数据覆盖文件标签（`scanner.go:140` 附近），即使 track 被删除重建也能保留用户纠错。

## 11. 任务/作业

### `scan_jobs` — 扫描任务
| 字段 | 说明 |
|------|------|
| library_id | 所属库 |
| type / status | `full` / `overwrite` 等；`pending` / `running` / `done` / `error` |
| total_files / scanned / new_tracks / updated_tracks / deleted_tracks / errors | 进度统计 |
| created_at / completed_at | 生命周期 |

### `download_jobs` — 下载任务（框架预留）
| 字段 | 说明 |
|------|------|
| url / source | 下载源 |
| library_id / format | 落库目标 |
| status / progress / error | 执行状态 |
| metadata | JSONB |

> 注意：download 框架暂无内置源（AGENTS.md 标记为 WIP）。

## 12. Jukebox 硬件播放

### `audio_devices` — 音频设备配置
| 字段 | 说明 |
|------|------|
| name / device_type | `local` / 网络类型 |
| device_id / driver | ffplay 目标设备；`pulseaudio` / `alsa` |
| config | JSONB 设备参数 |

### `jukeboxes` — 服务器端播放器
| 字段 | 说明 |
|------|------|
| device_id / device_name / device_config_id / device_driver | 绑定设备 |
| volume / play_mode | 音量与循环模式 |
| **queue / shuffle_order** | JSONB 数组（track ID 列表） |
| queue_idx / shuffle_idx | 当前播放位置 |
| path_mapping | JSONB，路径映射（本地路径 ↔ 服务器路径） |

设计：与浏览器播放器（前端 zustand store + localStorage）是**两套独立播放体系**：
- 浏览器播放：MSE 流式 + 前端队列（`player.ts`）
- Jukebox：ffplay 通过 PulseAudio/ALSA 播放，服务端持队列（`jukeboxes` 表）

## 13. 系统配置

### `server_settings` — 全局 KV
| 字段 | 说明 |
|------|------|
| key / value | 如 `allow_registration` 是否开放注册 |

---

## 数据模型关系图（简）

```
users 1──N libraries 1──N tracks 1──N track_artists N──1 artists
                     │         │
                     │         ├──N track_albums N──1 albums
                     │         │
                     │         └──N track_version_groups (按 mbid 分组)
                     │
                     ├──N library_members N──N users
                     │
                     └──N images (owner_type/owner_id 多态)

users 1──N favorites (track/album/artist)
users 1──N play_history
users 1──N playlists (track_ids JSONB)
users 1──N user_metadata (file_hash)
users 1──N jukeboxes 1──1 audio_devices
```

## 关键设计取舍总结

| 取舍点 | 选择 | 理由 |
|--------|------|------|
| 元数据存储 | 优先数据库（metadata_storage_mode） | 源文件只读，元数据不写回（AGENTS.md） |
| 播放列表 track 引用 | JSONB 数组 | 写入简单；代价是引用完整性需应用层维护 |
| 多版本分组 | 每文件一条 track + `version` 字段 | 保留各版本独立元数据，播放/收藏灵活 |
| 用户纠错 | 按 file_hash 关联（user_metadata） | 扫描重建后纠错不丢失 |
| 播放器状态 | localStorage + 服务端 user_settings 双写 | 刷新快速恢复 + 跨设备同步 |
| 队列/播放器 | 浏览器播放器与 Jukebox 分离 | 两种使用场景独立，互不干扰 |
