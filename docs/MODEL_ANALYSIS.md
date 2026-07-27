# 音乐数据模型分析

> 基于当前代码实现 (`internal/core/domain/domain.go`) 的现状分析。
> 日期: 2026-07-27

---

## 1. 歌曲封面：单曲是否有独立封面？

### 现状

**封面目前只属于专辑和艺人，Track 没有封面字段。**

- `Track` 结构体 (`domain.go:89-118`) — 无 `CoverImageID`
- `Album` 结构体 (`domain.go:80`) — 有 `CoverImageID *string`
- `Artist` 结构体 (`domain.go:66`) — 有 `CoverImageID *string`
- `images` 表 (`db.go:129-146`) — 泛化 `owner_type`/`owner_id`，可存储任意类型封面，但无代码写入 `owner_type='track'`
- Subsonic `getCoverArt` (`subsonic/handler.go`) — 只解析 album ID 和 artist ID，传 track ID 返回 404

### 需要改进

- Track 加 `CoverImageID` 字段 + 数据库列
- 扫描器调用 `CoverExtractor` 从每个音频文件提取内嵌封面
- API 提供 `/api/.../cover` 端点
- 前端加载并展示封面

---

## 2. 同一首歌出现在不同专辑

### 现状

**Track 只有一个 `AlbumID` 外键，一首歌严格属于一张专辑。**

- 扫描器 key 是 `(library_id, file_path)`，同一音频文件在不同路径下会创建多条 track 记录
- 无多对多关联，无法支持合辑、精选集、原声带等场景

### 需要改进

- 引入多对多关联表：

```sql
CREATE TABLE track_albums (
    track_id VARCHAR(26) NOT NULL REFERENCES tracks(id),
    album_id VARCHAR(26) NOT NULL REFERENCES albums(id),
    track_number INT NOT NULL DEFAULT 0,
    disc_number INT NOT NULL DEFAULT 1,
    PRIMARY KEY (track_id, album_id)
);
```

- 或参考 MusicBrainz 模型：**Recording → Release (Album)**，一首录音可出现在多个发布中

---

## 3. 歌曲不同版本（不同歌手/专辑/版本）

### 现状

**所有版本都是独立 Track，彼此无关联。**

当前可能的重复版本：
- `Bohemian Rhapsody (Album Version).flac`
- `Bohemian Rhapsody (Live).flac`
- `Bohemian Rhapsody (Rock Version).flac`

三条记录互不相干。尽管 `domain.go:132-139` 定义了 `MBMetadata` 包含：
- `RecordingID` — MusicBrainz 录音 ID（唯一标识一段音频）
- `WorkID` — MusicBrainz 作品 ID（标识一首歌的抽象概念）
- `ReleaseGroupID` — 发布组 ID

但扫描器没有填充这些字段，也没有归并逻辑。

### 需要改进

- 引入 `Work` 实体聚合多个 Recording
- 通过 MusicBrainz 的 `RecordingID`/`WorkID` 自动归并
- 前端展示时允许在同一 Work 下切换不同版本

---

## 4. 艺术家角色分类

### 现状

**Artist 模型只有一个 Name，没有角色分类。**

- `Artist` 字段：`ID`, `Name`, `SortName`, `MBID`, `Biography`, `CoverImageID`
- `TrackMetadata` (`domain.go:120-130`) 有 `Composer`、`Conductor` 字段，但扫描器不填充
- 无单独的贡献者/人员表

### 需要改进

引入多对多贡献者关联表：

```sql
CREATE TABLE track_contributors (
    track_id   VARCHAR(26) NOT NULL REFERENCES tracks(id),
    person_id  VARCHAR(26) NOT NULL,
    role       VARCHAR(20) NOT NULL, -- singer, composer, lyricist, arranger, producer, conductor, ...
    PRIMARY KEY (track_id, person_id, role)
);
```

可复用 `artists` 表作为 person（所有音乐人），或者拆分为独立的 `persons` 表。

常见角色枚举：
| 角色 | 说明 |
|------|------|
| `singer` / `vocalist` | 演唱 |
| `composer` | 作曲 |
| `lyricist` | 作词 |
| `arranger` | 编曲 |
| `producer` | 制作人 |
| `conductor` | 指挥 |
| `orchestra` | 管弦乐团 |
| `featured` | 特邀艺人 |

---

## 5. 多格式/多品质文件（FLAC + MP3）

### 现状

**同一首歌的不同格式文件，当前是完全独立的 Track 记录。**

例如 `album/01-song.flac` 和 `album/01-song.mp3`：
- 两条 Track 记录，不同路径、不同哈希
- 不同 `file_format`、`bit_rate`、`sample_rate`
- 指向同一个 `Album`
- 无去重逻辑，无品质优先策略

### 需要改进

引入多文件映射子表，实现逻辑曲目一个、物理文件多个：

```sql
CREATE TABLE track_files (
    id          SERIAL PRIMARY KEY,
    track_id    VARCHAR(26) NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    file_path   TEXT NOT NULL,
    file_format VARCHAR(10) NOT NULL,
    file_size   BIGINT NOT NULL DEFAULT 0,
    bit_rate    INTEGER NOT NULL DEFAULT 0,
    sample_rate INTEGER NOT NULL DEFAULT 0,
    channels    INTEGER NOT NULL DEFAULT 2,
    hash        VARCHAR(64) NOT NULL DEFAULT '',
    is_primary  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

播放时按偏好选择文件：
- 用户偏好 `highest_quality` → 选最高 bit_rate
- 用户偏好 `lossless` → 优先 flac/wav
- 用户偏好 `smallest` → 优先 mp3/ogg

同时需要修改扫描器：**[path + hash] 检测 → 如果是已存在的逻辑曲目的新格式，关联到同一 track 而非新建**。

---

## 综合路线建议

| 优先级 | 项目 | 复杂度 | 状态 |
|--------|------|--------|------|
| P0 | 封面完整链路（提取+存储+API+展示） | 中 | ✅ 已完成 |
| P1 | 多品质文件合并（track_files） | 中 | 📋 待实现 |
| P2 | 多专辑关联（track_albums） | 低 | 📋 待实现 |
| P3 | 多角色贡献者（track_contributors） | 中 | 📋 待实现 |
| P4 | Work/版本归并（MBID 接入扫描器） | 高 | 📋 待实现 |
