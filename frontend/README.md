# frontend

ASTREVA 官网前端（唯一的一套）。

- 技术栈：Astro 7 + Tailwind CSS **v4** + Preline UI **v5** + React islands
- 端口：`4321`
- 构建产物：`frontend/dist`，由发布流程写入静态卷；`frontend/Dockerfile` 只负责起 nginx

## 视觉语言：精密工程感

设计方向来自品牌本身（不锈钢排水系统 / 市政工程），用"技术图纸"的语汇替代通用的企业蓝模板：

- **蓝图网格**：深色区块叠一层 1px 网格 + 径向遮罩（`.blueprint-grid`），暗示图纸与精度；浅色区块用更淡的版本。
- **金色只做索引**：`accent-500` 仅用于眉题短横线、强调点，不做大面积色块。
- **排版分层**：标题统一 `display-tight`（`text-wrap: balance` + 收紧字距）；字号梯度拉到 `text-8xl`；正文行高 1.75–2。
- **结构替代装饰**：优势区是"细线分隔 + 描边序号 01–04"；产品卡用分类胶囊 + 图片缓推。
- **动效克制**：`.reveal` 滚动揭示（位移 18px，700ms），**只在 JS 可用时生效**（`html.js` 守卫），并提供 `prefers-reduced-motion` 降级。
- **移动端**：导航品牌区单行化，Hero 字号与留白单独压缩，底部数据条在小屏隐藏以避免与 Cookie 横幅重叠。

## 图标系统

CMS 里存的是字符（`✓ ▶ 工 滴 盾 ? ☎ ✉ ← 层 ↓ ◇`），直接渲染会随访客字体变化、粗细不一。现在统一映射到内联 SVG（`components/ui/Icon.astro`，24×24、1.7 描边、currentColor），映射表在 `lib/icons.ts`。**内容字段不变**，编辑仍然填原来的字符。

解析顺序：

1. **上下文映射**：同一个字符在不同区块含义不同——`工` 在优势区是"承重"（卡车），在支持页是"安装"（扳手），在产品说明是"文档"。
2. **全局字符映射**：`✓ → check`、`▶ → play`、`滴 → droplet`、`盾 → shieldCheck`、`☎ → phone`、`✉ → mail`、`← → arrowLeft`、`↓ → download`、`层 → layers`、`◇ → box`。
3. **语义名**：`file / book / search / arrow` 这些导航里已经用的名字。
4. **兜底**：未识别的一律渲染 `circle`，不会退回字体字符。

新增图标：在 `lib/icons.ts` 的 `IconName` 联合类型和 `Icon.astro` 的 `paths` 里各加一条即可。

## 目录结构

```
src/
  components/          业务组件（Navbar / ChatWidget / CookieConsent / ContactForm / VideoModal / InnerPageHero）
  components/ui/       版式与基础组件（Container / SectionHeading / Button / Accordion / Icon）
  layouts/BaseLayout   全站骨架：SEO、hreflang、JSON-LD、埋点、页脚、Preline 运行时
  lib/                 内容契约（types / content / i18n / seo / consent / tracking / video / icons）
  styles/global.css    Tailwind v4 入口 + Preline 三件套 + 富文本兜底
  styles/themes/       astreva.css —— 品牌令牌唯一来源
  pages/               路由（[lang] 多语言、products、solutions、support、legal、robots、llms.txt）
  views/               页面级视图
```

## 约定

1. **不写死色值。** 只用 `brand-*` / `ink-*` / `line-*` / `surface-*` / `navy-*` / `accent-*` 这些语义类；新增颜色先进 `themes/astreva.css`。
2. **不写死文案。** `pages/`、`components/`、`layouts/` 里出现中文或 http(s) 链接，`npm run cms:contract` 会直接失败。所有文案来自内容接口。
3. **版式用组件。** 区块用 `Section` + `Container` + `SectionHeading`，按钮用 `Button`，图标用 `Icon`，不要重新拼 class 串。
4. **整站圆角统一 10px**，由 `--radius-*` 统一覆写。
5. **Preline 用法**：`global.css` 导入 `@preline/accordion` 的 `variants.css` + `theme.css`；`BaseLayout` 里 `import '@preline/accordion'` 做自动初始化。新增其他组件时，按同样方式引入对应的 `@preline/<plugin>` 包。

## 运行与构建

```bash
npm run dev:frontend        # 开发服务器 http://127.0.0.1:4321
npm run build:frontend      # 契约校验 + 构建到 frontend/dist
npm run preview:frontend    # 预览构建产物
npm run typecheck           # astro check（.astro + TS 全量类型检查）
npm run lint                # ESLint（flat config，Astro + TS）
npm run format              # Prettier 写入
npm run format:check        # 只检查格式
npm run check               # typecheck + lint
npm run cms:contract        # 只跑内容契约校验
```

构建需要内容接口可达。没有后台时，用仓库自带的桩内容 API：

```bash
node scripts/verify-content-api-stub.mjs &          # 日志里会打印 STUB_PORT
CONTENT_API_URL="http://127.0.0.1:$STUB_PORT" npm run build:frontend
```

## Tailwind v3 → v4 迁移踩过的坑（重要）

1. **渐变工具改名**：`bg-gradient-to-br` → `bg-linear-to-br`。
2. **`leading-*` 与 `text-*` 的优先级反转**。
   - v3：响应式 `md:text-6xl` 会盖过 `leading-tight`（媒体查询在后面），但**基础** `text-lg` 盖不过 `leading-relaxed`。
   - v4：`leading-*` 一律盖过 `text-*`，包括响应式的。
   - 因此凡是「响应式字号 + 行高」的组合，都要显式补齐断点，例如 `text-5xl leading-tight md:text-6xl md:leading-none`、`text-base leading-8 md:text-lg md:leading-7`。
3. **默认字体栈不同**：v4 的 `font-sans` 首项是 `-apple-system`，v3 是 `ui-sans-serif`。已通过 `--font-sans` 令牌统一。

## 代码规范

- **ESLint**：`eslint.config.js`（flat config），启用 `@eslint/js` + `typescript-eslint` + `eslint-plugin-astro`；`dist/`、`.astro/` 被忽略。
- **Prettier**：`.prettierrc.json` + `prettier-plugin-astro`，规则为单引号、120 列、无尾逗号。
- 提交前建议至少跑 `npm run check`（类型 + lint）；CI/部署前跑根目录的 `npm run verify`。

## 渲染与体积

- **关键 UI 走 SSR**：`Navbar` 与 Hero 的 `VideoModal`（主 CTA 在它里面）用 `client:load`，导航和主按钮是服务端 HTML，爬虫和慢网都能拿到；`ChatWidget` / `CookieConsent` 仍用 `client:only`（它们依赖 `window` 且非关键路径）。
- **Preline 按插件引入**：站点只用了手风琴，因此依赖 `@preline/accordion` 而不是整包 `preline`（后者会把 27 个插件全打进产物）。这个包同时提供 JS、`theme.css` 和 `variants.css`，所以 umbrella 依赖已经移除。
- 构建产物（gzip）：JS 约 **75 KB**（其中 React 运行时占大头），CSS 约 **13 KB**。
- 滚动揭示用原生 `IntersectionObserver`，无第三方动画库。

## 验收记录

| 项目           | 结果                                                        |
| -------------- | ----------------------------------------------------------- |
| 路由           | 67 个 HTML 页面（zh-CN 根路径 / en / ar）                   |
| 布局回归       | 11 条主路由 × 1440 / 390 两档视口，横向溢出 0，控制台报错 0 |
| 类型检查       | `npm run typecheck` → **0 errors / 0 warnings / 0 hints**   |
| 内容契约       | `cms:contract` 通过（页面内零硬编码中文/链接）              |
| 全量验证       | `bash scripts/verify-all.sh` → 11/11 PASS                   |
| 发布链路       | 原子发布 + `current` symlink 切换通过                       |
| 交互           | 导航下拉、语言切换、Cookie、FAQ、表单、客服 iframe 均可用   |
| 多语言         | `zh-CN` 根路径 / `en` / `ar`，`ar` 为 `dir="rtl"`           |
| 无 JS 降级     | 内容默认可见（`html.js` 守卫），不会因脚本失败白屏          |
| reduced-motion | 滚动揭示动画自动关闭                                        |

## 部署

- `frontend/Dockerfile` 只构建 nginx 镜像，并把 `frontend/nginx.conf` 复制进去。
- 静态产物不在镜像里：发布流程把 `frontend/dist` 写进 `frontend-published` 卷的 `releases/`，再把 `current` 指向最新版本（见 `scripts/build-frontend.mjs`）。
- 草稿预览写到同一卷的 `preview/`，通过 `/__preview/` 访问，不影响 `current`。
