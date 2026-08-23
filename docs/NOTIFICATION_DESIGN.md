# 通知渠道架构

## 1. 设计原则

1. **业务与实现分离** — 通知服务层负责"何时发、发给谁、发什么"，渠道实现只负责"发送"
2. **渠道可插拔** — 新增渠道只需实现 `Notifier` 接口，注册即可，零改动业务层
3. **模板驱动** — 消息内容通过 `//go:embed` 内嵌模板渲染，新增通知类型只需加模板文件
4. **配置即覆盖** — 运行时从 DB 设置表读取配置，配置文件为兜底，管理后台保存后立即生效

## 2. 架构分层

```
┌──────────────────────────────────────────────────────────┐
│  core/service/notification_service.go                    │
│  ┌─ 业务逻辑层 ──────────────────────────────────────┐  │
│  │  • 何时发送（扫描完成、扫描失败等事件）             │  │
│  │  • 查找管理员邮箱（UserRepository.FindByRole）      │  │
│  │  • 构建消息 Metadata（注入模板数据）                │  │
│  │  • 路由到对应渠道                                   │  │
│  │  • 共享 sender 实例，配置变更时重建                 │  │
│  └───────────────────────────────────────────────────┘  │
├──────────────────────────────────────────────────────────┤
│  core/port/notification.go                               │
│  ┌─ 接口抽象层 ──────────────────────────────────────┐  │
│  │  Notifier interface       ← 发送通知               │  │
│  │  MessageReceiver interface ← 接收消息（预留）       │  │
│  │  NotificationMessage      ← 领域消息结构体         │  │
│  └───────────────────────────────────────────────────┘  │
├──────────────────────────────────────────────────────────┤
│  infrastructure/notification/                            │
│  ┌─ 具体实现层（不关心业务逻辑）────────────────────┐  │
│  │  email/sender.go     SMTP 发送（STARTTLS/SMTPS）    │  │
│  │  email/receiver.go   IMAP 收信骨架（预留）          │  │
│  │  email/templates/    内嵌 HTML/文本模板             │  │
│  └───────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────┘
```

## 3. 核心类型

### 通知类型

```go
// internal/core/domain/notification.go
type NotificationType string

const (
    NotifScanComplete NotificationType = "scan_complete"
    NotifScanFailed   NotificationType = "scan_failed"
    NotifSystemError  NotificationType = "system_error"
)
```

### 渠道类型

```go
type ChannelType string

const (
    ChannelEmail ChannelType = "email"
)
```

### 消息结构

```go
type NotificationMessage struct {
    ID        string
    Type      NotificationType   // 决定使用哪个模板
    Channel   ChannelType        // 决定走哪个渠道
    To        []string           // 收件人列表
    Subject   string
    TextBody  string             // 预渲染文本（为空则用模板）
    HTMLBody  string             // 预渲染 HTML（为空则用模板）
    Metadata  map[string]any     // 模板数据
    CreatedAt time.Time
}
```

## 4. 接口定义

```go
// internal/core/port/notification.go

// Notifier 发送通知。底层实现只关心"发送"本身，不关心业务。
type Notifier interface {
    ChannelType() ChannelType
    Name() string
    Enabled() bool
    Send(ctx context.Context, msg *NotificationMessage) error
}

// MessageReceiver 接收消息（将来扩展：邮件回复、指令等）。
type MessageReceiver interface {
    ChannelType() ChannelType
    Start(ctx context.Context, handler ReceivedMessageHandler) error
    Stop(ctx context.Context) error
}

type ReceivedMessageHandler func(ctx context.Context, msg *NotificationMessage) error
```

## 5. 数据流

### 扫描完成通知

```
ScannerService.runScan()
  └─→ NotificationService.NotifyScanComplete(ctx, job, libName)
       ├─ users.FindByRole("super_admin") → 获取管理员邮箱
       ├─ 构建 Metadata{LibraryName, NewTracks, UpdatedTracks, ...}
       ├─ 构建 NotificationMessage{Type, Channel, To, Subject, Metadata}
       └─→ Send(ctx, msg)
            └─ notifiers[ChannelEmail].Send(ctx, msg)
                 └─ EmailSender.SendWithConfig(ctx, msg, cfg)
                      ├─ render(msg) → 根据 msg.Type 选择模板渲染
                      │   ├─ textTemplates.Lookup("scan_complete.txt")
                      │   └─ htmlTemplates.Lookup("scan_complete.html")
                      ├─ buildMIMEMessage(...) → 构建邮件
                      ├─ dial → 建立 SMTP 连接
                      │   ├─ cfg.TLS && port 465 → dialSSL (直接 TLS)
                      │   └─ 其他 → dial + STARTTLS (可选)
                      ├─ sendWithClient → 发送
                      └─ client.Quit() → 正常关闭
```

### 测试发送

```
前端 AdminPage → POST /api/notifications/test
  ├─ 请求体含当前表单值（smtp_host, smtp_port, password, ...）
  └─→ NotificationHandler.SendTest()
       ├─ 从已保存配置加载 EmailConfig（EmailConfig()）
       ├─ 用请求中的字段覆盖
       └─→ SendTestWithConfig(ctx, to, cfg)
            └─ email.NewSender(cfg).Send(ctx, msg)  ← 临时 sender，不影响共享实例
```

## 6. 模板系统

### 模板文件位置

```
internal/infrastructure/notification/email/templates/
├── scan_complete.html
├── scan_complete.txt
├── scan_failed.html
├── scan_failed.txt
```

### 模板命名规则

文件名必须与 `NotificationType` 一致：`{type}.html` / `{type}.txt`。

### 模板数据

模板通过 `{{.FieldName}}` 使用 `NotificationMessage.Metadata` 中的字段。

### 渲染流程

```go
func (s *Sender) render(msg *NotificationMessage) (text, html string, err error) {
    data := make(map[string]any, len(msg.Metadata)+1)
    for k, v := range msg.Metadata { data[k] = v }  // 浅拷贝
    data["Timestamp"] = time.Now().Format(time.RFC1123)

    // 优先使用预渲染内容，否则用模板
    if msg.TextBody == "" {
        t := textTemplates.Lookup(string(msg.Type) + ".txt")
        // 执行模板...
    }
    if msg.HTMLBody == "" {
        t := htmlTemplates.Lookup(string(msg.Type) + ".html")
        // 执行模板...
    }
}
```

## 7. 配置

### 配置文件

```toml
[notification.email]
enabled = false
smtp_host = "smtp.example.com"
smtp_port = 587
username = ""
password = ""
from_address = "sonicore@example.com"
from_name = "Sonicore"
tls = true
```

### 运行时覆盖

配置可通过管理后台保存到 `server_settings` 表，key 为 `notification_email_*`。
`NotificationService.ReloadEmailConfig()` 读取 DB 配置重建共享 sender。

## 8. 扩展指南

### 新增通知类型

1. **`domain/notification.go`** — 添加 `NotificationType` 常量
2. **`email/templates/`** — 创建 `{type}.html` 和 `{type}.txt` 模板
3. **`notification_service.go`** — 添加对应的 `NotifyXxx()` 方法
4. 在业务层调用

### 新增渠道（如 Slack）

1. 创建 `infrastructure/notification/slack/sender.go`，实现 `Notifier` 接口
2. 在 `server.go` 中 `RegisterNotifier(slack.NewSender(...))`
3. 零改动业务层

### 启用收消息（如处理邮件回复）

1. 实现 `MessageReceiver` 接口
2. 在 `NotificationService` 中注册
3. 实现 `ReceivedMessageHandler` 处理回复

## 9. 安全措施

| 措施 | 说明 |
|------|------|
| AdminOnly 中间件 | 通知 API 端点仅管理员可访问 |
| STARTTLS 强制 | `TLS=true` 时若服务器不支持 STARTTLS 则报错，不降级明文 |
| Subject MIME 编码 | 非 ASCII 字符通过 `mime.QEncoding` 编码 |
| Content-Transfer-Encoding | 使用 base64 实际编码，而非虚假声明 |
| Header 防注入 | `sanitizeHeader` 过滤 `\r\n` |
| Context 取消传播 | `net.Dialer.DialContext` 传递上下文 |
| 密码区分 | `password` 不在请求中 = 不修改，`""` = 清空，`"xxx"` = 设置 |