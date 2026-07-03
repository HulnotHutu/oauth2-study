# OAuth 2.0 授权模式实战

一个基于 Go + Echo 的 OAuth 2.0 学习项目，完整实现了四种授权模式，帮助开发者理解 OAuth 2.0 的核心概念与每种模式的区别。

## 项目结构

```
cmd/
├── Authorization-Code/              # 授权码模式（标准版）
│   ├── auth-server/                 # 授权服务器 (:8081)
│   ├── client/                      # 客户端应用 (:8080)
│   ├── resource-server/             # 资源服务器 (:8082)
│   ├── types/                       # 共享类型定义
│   └── views/                       # HTML 模板
│
├── Authorization-Code-PKCE/         # 授权码 + PKCE（S256）
│   ├── auth-server/                 # 授权服务器 (:8081)
│   ├── client/                      # 客户端应用 (:8080)
│   ├── resource-server/             # 资源服务器 (:8082)
│   ├── types/                       # 共享类型定义
│   └── views/                       # HTML 模板
│
├── Authorization-Code-PKCE-github/  # PKCE 对接 GitHub 的 OAuth2.0 的 demos
│   ├── client/                      # 客户端应用 (:8080)
│   ├── types/                       # 共享类型定义
│   └── views/                       # HTML 模板 + Dashboard
│
├── Implicit/                        # 隐式模式（已弃用）
│   ├── auth-server/                 # 授权服务器 (:8081)
│   ├── client/                      # 客户端应用 (:8080)
│   ├── resource-server/             # 资源服务器 (:8082)
│   ├── types/                       # 共享类型定义
│   └── views/                       # HTML 模板
│
├── Client-Credentials/              # 客户端凭证模式
│   ├── auth-server/                 # 授权服务器 (:8081)
│   ├── client/                      # 客户端应用 (:8080)
│   ├── resource-server/             # 资源服务器 (:8082)
│   ├── types/                       # 共享类型定义
│   └── views/                       # HTML 模板
│
└── Resource-Owner-Password-Credentials/  # 密码模式（已弃用）
    ├── auth-server/                 # 授权服务器 (:8081)
    ├── client/                      # 客户端应用 (:8080)
    ├── resource-server/             # 资源服务器 (:8082)
    ├── types/                       # 共享类型定义
    └── views/                       # HTML 模板
```

## 四种授权模式一览

| 模式 | grant_type | 是否需要授权码 | 是否需要 client_secret | 推荐场景 |
|------|-----------|---------------|----------------------|---------|
| **授权码** | authorization_code | ✅ | ✅ | 服务端 Web 应用 |
| **授权码 + PKCE** | authorization_code | ✅ | 可不需要 | 原生 App / SPA |
| **隐式** （已弃用） | 无/token | ❌ | ❌ | 浏览器 SPA（已被 PKCE 替代） |
| **客户端凭证** | client_credentials | ❌ | ✅ | 服务间调用（M2M） |
| **密码** （已弃用） | password | ❌ | ✅ | 高度信任的第一方应用 |

## 快速开始

```bash
# 安装依赖
go mod tidy

# 启动任意模式（三组件模式需要三个终端）
# 例如：授权码模式
go run ./cmd/Authorization-Code/auth-server/    # 终端 1
go run ./cmd/Authorization-Code/resource-server/ # 终端 2
go run ./cmd/Authorization-Code/client/          # 终端 3

# 或者运行 GitHub PKCE 单组件模式
go run ./cmd/Authorization-Code-PKCE-github/client/
```

打开 http://localhost:8080 访问。

## 端口约定

| 组件 | 端口 | 说明 |
| --- | --- | --- |
| Client Application | `:8080` | 第三方客户端，用户交互入口 |
| Authorization Server | `:8081` | 用户认证、授权、令牌签发 |
| Resource Server | `:8082` | 受保护资源 |

## 内置用户

| 用户名 | 密码 | 说明 |
| --- | --- | --- |
| `alice` | `password123` | 演示用户 |
| `bob` | `secret456` | 演示用户 |

> 个别的需要

## 共性安全特性

| 特性 | 说明 |
| --- | --- |
| State 参数 (CSRF) | 每次授权生成随机 state，回调验证后消费 |
| 一次性授权码 | 10 分钟过期，使用后立即标记，重用则撤销令牌 |
| Refresh Token 轮换 | 每次刷新签发新 token，旧 token 标记为已轮换 |
| 重放检测 | 已轮换的 token 被重用 → 撤销所有相关令牌 |
| Token Introspection | 资源服务器通过 introspection 端点验证令牌 |
| 自动刷新 | 客户端检测 401 后自动刷新并重试 |

## 授权码 + PKCE 对比

| | 标准授权码 | 授权码 + PKCE |
|---|---|---|
| 授权码泄露防护 | client_secret（需安全存储） | code_verifier（加密绑定，无需存储） |
| Public Client | ❌ 不安全 | ✅ 安全 |
| 额外参数 | 无 | code_challenge + code_verifier |
| 实现复杂度 | 低 | 低 |

## 技术栈

- **Go 1.26** — 语言
- **Echo v5** — Web 框架
- **Go 标准库** — `crypto/rand`, `crypto/sha256`, `encoding/base64`, `net/http`, `log/slog`
- **HTML 模板** — 无前后端分离，Go `html/template` 渲染

## 提醒

这些只是演示的demo，参考文档进行实现的，用于理解 OAuth2.0 的四种流程机制。