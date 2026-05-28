# OpenLovart 本地开发设置指南

本指南帮助你在本地启动完整的 OpenLovart 开发环境（Next.js 前端 + Go 后端 + Postgres + Mailpit）。

## 📋 前置要求

- Node.js 18+
- Go 1.22+
- Docker / Docker Compose（用于 Postgres + Mailpit）
- 一个 Google Cloud 账号（用于 Google OIDC 登录，开发可选）

---

## 🚀 步骤 1：启动依赖服务（Postgres + Mailpit）

仓库根目录下：

```bash
docker compose -f docker-compose.dev.yml up -d
```

这将启动：

- **Postgres** — `localhost:5432`，账户 `openlovart` / `openlovart` / `openlovart`
- **Mailpit** — SMTP `localhost:1025`，Web UI `http://localhost:8025`（开发期所有验证 / 重置邮件都会落到这里）

---

## 🔐 步骤 2：配置后端

```bash
cd backend
cp .env.example .env
```

打开 `backend/.env`：

- 开发环境**不必**填写 `AUTH_JWT_PRIVATE_KEY_PATH` / `AUTH_JWT_PUBLIC_KEY_PATH`：后端首次启动会自动生成 RSA 密钥到 `backend/.dev-keys/`
- **必须**填写 `AUTH_OIDC_STATE_SECRET`（≥ 32 字节随机串）。生成示例：
  ```bash
  openssl rand -base64 48
  ```
- 如果要测试 Google 登录，把以下三项填上：
  - `OIDC_GOOGLE_CLIENT_ID`
  - `OIDC_GOOGLE_CLIENT_SECRET`
  - `OIDC_GOOGLE_REDIRECT_URL=http://localhost:8080/api/auth/oidc/google/callback`

不填则后端启动时会跳过 Google verifier 初始化，仍然可以使用密码登录、注册、邮箱验证、密码重置流程。

启动后端：

```bash
cd backend
go run ./cmd/server
```

后端默认监听 `http://localhost:8080`，关键端点：

- `GET /healthz`、`GET /api/health`
- `GET /api/auth/.well-known/jwks.json` — JWKS（公开，5 分钟缓存）
- `POST /api/auth/register|login|refresh|logout|forgot-password|reset-password|verify-email`
- `GET /api/auth/me` — 当前用户（需要 access cookie + CSRF）
- `GET/POST/PATCH/DELETE /api/projects[/:id]`
- `GET/PUT /api/projects/:id/canvas-elements`
- `GET /api/credits`

---

## 🔑 步骤 3：（可选）配置 Google OIDC

1. 进入 [Google Cloud Console](https://console.cloud.google.com/) → APIs & Services → Credentials
2. 创建 **OAuth client ID**，类型选 **Web application**
3. 在 “Authorized redirect URIs” 添加：
   ```
   http://localhost:8080/api/auth/oidc/google/callback
   ```
4. 拿到 `Client ID` 与 `Client secret`，填进 `backend/.env`

---

## 🌐 步骤 4：配置并启动前端

仓库根目录下：

```bash
cp .env.local.example .env.local
```

`.env.local` 关键变量：

- `BACKEND_INTERNAL_URL=http://localhost:8080` — 服务端组件用它来取 JWKS
- `AUTH_JWKS_CACHE_TTL=300` — JWKS 缓存秒数

> ⚠️ 前端**不需要**任何 JWT 签名密钥。所有签名/验签都在后端，Next.js 仅通过公开的 JWKS 验证。

安装依赖并启动：

```bash
npm install
npm run dev
```

访问 [http://localhost:3000](http://localhost:3000)。

`/api/auth/*`、`/api/projects/*`、`/api/credits/*` 通过 `next.config.ts` 的 dev rewrite 透传到 `http://localhost:8080`。

---

## ✅ 步骤 5：端到端冒烟

1. 注册账号 → 在 [http://localhost:8025](http://localhost:8025) 收验证邮件 → 点击链接验证邮箱
2. 登录 → 创建项目 → 在画布添加元素 → 刷新页面，元素仍在
3. 退出 → "Sign in with Google"（如已配置 OIDC）合并到同一账户
4. 走一遍 Forgot Password：旧 refresh token 应当失效（重放被拒）
5. 直接访问 `/lovart` 在未登录时应被重定向到 `/sign-in?next=/lovart`

---

## 🛠️ 生产部署密钥

生产环境**必须**离线生成 RSA 密钥对，并通过环境变量挂载：

```bash
openssl genrsa -out jwt.key 2048
openssl rsa -in jwt.key -pubout -out jwt.pub
```

把这两个文件挂到容器，再设置：

```env
ENV=production
AUTH_JWT_PRIVATE_KEY_PATH=/run/secrets/jwt.key
AUTH_JWT_PUBLIC_KEY_PATH=/run/secrets/jwt.pub
AUTH_COOKIE_SECURE=true
AUTH_COOKIE_DOMAIN=your.domain
```

详见 `backend/README.md`。

---

## 📚 参考

- [后端 README](./backend/README.md)
- [Next.js docs](https://nextjs.org/docs)
- [Google OIDC docs](https://developers.google.com/identity/openid-connect/openid-connect)
