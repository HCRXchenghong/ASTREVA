# frontend-next

ASTREVA 官网的新前端。与现有 `frontend/` **并行存在**，互不影响；验收通过后可一键切换、再删除旧目录。

- 技术栈：Astro 7 + Tailwind CSS **v4** + Preline UI **v5** + React islands
- 端口：`4330`（旧前端是 `4321`，两个可以同时跑起来左右对比）
- 状态：**功能与视觉已与旧站对齐**，切换链路已验证，等待最终确认后切换

## 为什么要有这个目录

旧前端的问题不是"不好看"，而是"不好维护"：

| 问题 | 旧前端 | 新前端 |
| --- | --- | --- |
| 品牌色 | 散落 100+ 处硬编码 hex | 全部收敛到 `src/styles/themes/astreva.css` |
| 版式 | `max-w-7xl mx-auto px-6` 重复 15 次、`py-24` 13 次 | `ui/Container`、`ui/Section`、`ui/SectionHeading`、`ui/Button` |
| 交互无障碍 | 手写 `<details>`、手写下拉 | Preline 组件提供 ARIA + 键盘行为 + 焦点管理 |
| Tailwind | v3.4.17 | v4.3.3（新目录无历史包袱） |

## 运行与构建

```bash
npm run dev:frontend-next        # 开发服务器 http://127.0.0.1:4330
npm run build:frontend-next      # 契约校验 + 构建到 frontend-next/dist
npm run preview:frontend-next    # 预览构建产物
npm run cms:contract:next        # 只跑新目录的内容契约校验
```

构建需要内容接口可达。没有后台时，用仓库自带的桩内容 API：

```bash
node scripts/verify-content-api-stub.mjs &          # 日志里会打印 STUB_PORT
CONTENT_API_URL="http://127.0.0.1:$STUB_PORT" npm run build:frontend-next
```

## 切换与回滚

`scripts/frontend-target.mjs` 是"当前生效的是哪个官网目录"的**单一事实来源**，
构建、静态服务、内容契约校验、后台 `/assets/` 托管都读它，默认仍是 `frontend`。

```bash
# 切到新前端
ACTIVE_FRONTEND=frontend-next npm run build:frontend

# 回滚（旧目录一个字节都没动）
ACTIVE_FRONTEND=frontend npm run build:frontend
# 或直接去掉 ACTIVE_FRONTEND

# 完整发布链路（原子发布 + symlink 切换）演练
ACTIVE_FRONTEND=frontend-next PUBLISH_DIR=/tmp/astreva-publish node scripts/build-frontend.mjs

# 以新前端为目标跑全量验证
ACTIVE_FRONTEND=frontend-next bash scripts/verify-all.sh
```

## 目录结构

```
src/
  components/          业务组件（Navbar / ChatWidget / CookieConsent / ContactForm / VideoModal / InnerPageHero）
  components/ui/       版式组件（Container / Section / SectionHeading / Button / Accordion）
  layouts/BaseLayout   全站骨架：SEO、hreflang、JSON-LD、埋点、页脚、Preline 运行时
  lib/                 内容契约（types / content / i18n / seo / consent / tracking / video）
  styles/global.css    Tailwind v4 入口 + Preline 三件套 + 富文本兜底
  styles/themes/       astreva.css —— 品牌令牌唯一来源
  pages/               路由（[lang] 多语言、products、solutions、support、legal、robots、llms.txt）
  views/               页面级视图
```

## 约定

1. **不写死色值。** 只用 `brand-*` / `ink-*` / `line-*` / `surface-*` / `navy-*` / `accent-*` 这些语义类；新增颜色先进 `themes/astreva.css`。
2. **不写死文案。** `pages/`、`components/`、`layouts/` 里出现中文或 http(s) 链接，`npm run cms:contract:next` 会直接失败。所有文案来自内容接口。
3. **版式用组件。** 区块用 `Section` + `Container` + `SectionHeading`，按钮用 `Button`，不要重新拼 class 串。
4. **整站圆角统一 10px**，由 `--radius-*` 统一覆写，与旧前端一致。
5. **Preline 用法**：`global.css` 已接入 `variants.css` + `theme.css`；`BaseLayout` 里 `import 'preline'` 做自动初始化。组件按 `hs-accordion` / `hs-dropdown` 等约定写 `data-*` 与 `hs-*` 类即可。

## Tailwind v3 → v4 迁移踩过的坑（重要）

从旧前端搬迁样式时，有三类差异会导致肉眼可见的错位，都已修好，**改样式前请先读**：

1. **渐变工具改名**：`bg-gradient-to-br` → `bg-linear-to-br`。
2. **`leading-*` 与 `text-*` 的优先级反转**。
   - v3：响应式 `md:text-6xl` 会盖过 `leading-tight`（媒体查询在后面），但**基础** `text-lg` 盖不过 `leading-relaxed`。
   - v4：`leading-*` 一律盖过 `text-*`，包括响应式的。
   - 因此凡是「响应式字号 + 行高」的组合，都要显式补齐断点，例如
     `text-5xl leading-tight md:text-6xl md:leading-none`、
     `text-base leading-8 md:text-lg md:leading-7`。
3. **默认字体栈不同**：v4 的 `font-sans` 首项是 `-apple-system`，v3 是 `ui-sans-serif`。已通过 `--font-sans` 令牌统一。

## 与旧前端的**有意差异**

- FAQ 从原生 `<details>` 换成 Preline 手风琴：多出高度动画、`role="region"` 关联和一致的键盘行为；通过 `data-hs-accordion-always-open` 保留了"可同时展开多项"的旧行为。
- 删除了未被任何页面引用的 `FAQAccordion.tsx`（旧目录里的死代码）。

## 验收记录

| 项目 | 结果 |
| --- | --- |
| 路由 | 与旧站**完全一致**（`diff` 逐条比对 67 个 HTML） |
| 视觉 | 逐页像素比对，除 FAQ 手风琴外最大通道差 ≤ 8/255 |
| 内容契约 | `cms:contract:next` 通过 |
| 全量验证 | `ACTIVE_FRONTEND=frontend-next bash scripts/verify-all.sh` → **11/11 PASS** |
| 发布链路 | 原子发布 + `current` symlink 切换演练通过（67 页） |
| 交互 | 导航下拉、语言切换、Cookie、FAQ、表单、客服 iframe 均可用，0 控制台报错 |
| 多语言 | `zh-CN` 根路径 / `en` / `ar`，`ar` 为 `dir="rtl"` |
