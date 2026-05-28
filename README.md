# OpenLovart 🎨

[![GitHub stars](https://img.shields.io/github/stars/xiaoju111a/OpenLovart?style=social)](https://github.com/xiaoju111a/OpenLovart/stargazers)
[![GitHub forks](https://img.shields.io/github/forks/xiaoju111a/OpenLovart?style=social)](https://github.com/xiaoju111a/OpenLovart/network/members)
[![GitHub issues](https://img.shields.io/github/issues/xiaoju111a/OpenLovart)](https://github.com/xiaoju111a/OpenLovart/issues)
[![GitHub license](https://img.shields.io/github/license/xiaoju111a/OpenLovart)](https://github.com/xiaoju111a/OpenLovart/blob/master/LICENSE)

OpenLovart 是一个基于 AI 的设计平台，让创意设计变得简单而强大。通过 AI 对话和智能画布，快速实现你的设计想法。

## ✨ 主要功能

- 🤖 **AI 设计助手** - 通过自然语言对话生成设计方案
- 🎨 **智能画布** - 可视化编辑器，支持拖拽、缩放、旋转
- 🖼️ **AI 图像生成** - 集成 Google Gemini 与 X.AI Grok
- 💾 **项目管理** - 自托管后端持久化你的项目与画布
- 👤 **自有用户系统** - 邮箱密码 + Google OIDC + 邮箱验证 + 密码重置
- ☁️ **可自部署** - 不依赖任何第三方鉴权 / 数据 BaaS

## 🚀 技术栈

- **前端**: Next.js 16 (App Router) + TypeScript + Tailwind CSS 4
- **后端**: Go (Gin + GORM) — 自有鉴权服务
- **鉴权**: 自研密码鉴权 + Google OIDC，**RS256 JWT + 公开 JWKS**
- **数据库**: PostgreSQL（citext + pgcrypto）
- **邮件**: SMTP（开发期用 Mailpit）
- **AI 服务**:
  - Google Gemini（图像生成）
  - X.AI Grok（设计建议）

## 📦 快速开始

详细步骤见 [SETUP_GUIDE.md](./SETUP_GUIDE.md)。简版：

```bash
git clone git@github.com:xiaoju111a/OpenLovart.git
cd OpenLovart

# 1. 启动 Postgres + Mailpit
docker compose -f docker-compose.dev.yml up -d

# 2. 启动 Go 后端
cp backend/.env.example backend/.env
# 在 backend/.env 中填 AUTH_OIDC_STATE_SECRET（>= 32 字节）；
# JWT RSA 密钥会在 dev 模式下自动生成到 backend/.dev-keys/
cd backend && go run ./cmd/server

# 3. 启动 Next.js 前端
cd ..
cp .env.local.example .env.local
npm install
npm run dev
```

打开 [http://localhost:3000](http://localhost:3000)；邮件收件箱在 [http://localhost:8025](http://localhost:8025)（Mailpit）。

## 🔑 获取 API 密钥

### Google OIDC（用户登录，可选）
1. [Google Cloud Console](https://console.cloud.google.com/) → APIs & Services → Credentials
2. 创建 OAuth client ID（Web application）
3. Authorized redirect URI 设为 `http://localhost:8080/api/auth/oidc/google/callback`

### Google Gemini（AI 图像生成）
1. [Google AI Studio](https://makersuite.google.com/app/apikey) → 创建 API Key

### X.AI Grok（可选）
1. [X.AI Console](https://console.x.ai/) → 创建 API Key

## 📁 项目结构

```
OpenLovart/
├── src/                       # Next.js App Router
│   ├── app/
│   │   ├── api/               # AI 服务 BFF（generate-design / image / video）
│   │   └── lovart/            # 主应用页面
│   ├── components/lovart/     # 核心 UI 组件
│   ├── lib/                   # 前端工具（auth client、API helpers）
│   └── middleware.ts          # 路由守卫
├── backend/                   # Go 后端（Gin + GORM）
│   ├── cmd/server             # 入口
│   ├── internal/auth          # 密码鉴权 + JWT/JWKS + OIDC + cookies
│   ├── internal/business      # projects / canvas-elements / credits
│   ├── internal/email         # SMTP + 模板
│   └── internal/middleware    # CSRF / CORS / RateLimit / RequestID
├── docker-compose.dev.yml     # Postgres + Mailpit
├── openspec/                  # 需求与变更提案
└── README.md
```

## 🛠️ 可用命令

前端：

```bash
npm run dev      # 开发
npm run build    # 生产构建
npm run start    # 运行生产产物
npm run lint     # ESLint
```

后端：

```bash
cd backend
go run ./cmd/server          # 启动
go vet ./... && go build ./...   # 静态检查 + 编译
go test ./...                # 全部测试
```

## 📚 文档

- [本地开发设置指南](./SETUP_GUIDE.md)
- [后端 README](./backend/README.md)
- [OpenSpec 变更与规格](./openspec/)

## 🚢 部署

生产环境必须离线生成 RSA 密钥对并以环境变量方式挂载，详见 `backend/README.md`。前端可部署到 Vercel 或任何支持 Next.js 的平台，并把 `BACKEND_INTERNAL_URL` 指向你的后端服务。

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 许可证

MIT License

## 🙏 致谢

- [Next.js](https://nextjs.org/)
- [Gin](https://gin-gonic.com/) / [GORM](https://gorm.io/)
- [Google Gemini](https://ai.google.dev/)
- [X.AI](https://x.ai/)

## 📊 Star History

[![Star History Chart](https://api.star-history.com/svg?repos=xiaoju111a/OpenLovart&type=Date)](https://star-history.com/#xiaoju111a/OpenLovart&Date)

---

Made with ❤️ by Xiaoju
