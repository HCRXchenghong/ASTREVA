export function createOpenApiDocument(serverUrl) {
  const auth = [{ cookieAuth: [] }];
  const ref = (name) => ({ $ref: `#/components/schemas/${name}` });
  const jsonBody = (schema) => ({ required: true, content: { 'application/json': { schema } } });
  const jsonResponse = (schema, description = '成功') => ({ description, content: { 'application/json': { schema } } });
  const idParameter = { name: 'id', in: 'path', required: true, schema: { type: 'string', minLength: 1 } };
  return {
    openapi: '3.1.0',
    info: {
      title: 'ASTREVA 官网后台 API',
      version: '2.7.0',
      description: '官网内容、中英阿翻译、AI SEO、咨询单、媒体、R2/AI 生图运行配置、客服系统代理、降级版本、发布队列和审计日志接口。除公开内容、语言推荐、降级快照和咨询提交外，均需要后台会话 Cookie。'
    },
    servers: [{ url: serverUrl }],
    components: {
      securitySchemes: {
        cookieAuth: { type: 'apiKey', in: 'cookie', name: 'astreva_admin_session', description: '2FA 登录成功后由服务器写入的 HttpOnly 会话 Cookie。' }
      },
      schemas: {
        Error: {
          type: 'object', required: ['error'], properties: {
            error: { type: 'object', required: ['message'], properties: { message: { type: 'string' } } }
          }
        },
        SiteContent: {
          type: 'object',
          required: ['site', 'home', 'productCenter', 'support', 'categories', 'products', 'faqs', 'staticPages'],
          properties: {
            site: { type: 'object', additionalProperties: true },
            home: { type: 'object', additionalProperties: true },
            productCenter: { type: 'object', additionalProperties: true },
            support: { type: 'object', additionalProperties: true },
            categories: { type: 'array', items: { type: 'object', required: ['id', 'name', 'slug'], additionalProperties: true } },
            products: { type: 'array', items: { type: 'object', required: ['id', 'name', 'slug', 'categorySlug'], additionalProperties: true } },
            faqs: { type: 'array', items: { type: 'object', required: ['id', 'question', 'answer'], additionalProperties: true } },
            staticPages: { type: 'array', items: { type: 'object', required: ['slug', 'title'], additionalProperties: true } }
          },
          additionalProperties: false
        },
        LeadSubmission: {
          type: 'object', required: ['name'], properties: {
            name: { type: 'string', minLength: 1, maxLength: 120 },
            companyName: { type: 'string', maxLength: 160 },
            phone: { type: 'string', maxLength: 32 },
            email: { type: 'string', format: 'email', maxLength: 254 },
            message: { type: 'string', maxLength: 5000 },
            website: { type: 'string', description: '反机器人蜜罐，真实用户必须留空。' },
            sourcePage: { type: 'string', maxLength: 300 },
            sourceUrl: { type: 'string', format: 'uri', maxLength: 1000 },
            referrer: { type: 'string', format: 'uri', maxLength: 1000 },
            utm: { type: 'object', properties: Object.fromEntries(['source', 'medium', 'campaign', 'term', 'content'].map((key) => [key, { type: 'string', maxLength: 200 }])) }
          },
          anyOf: [{ required: ['phone'] }, { required: ['email'] }],
          additionalProperties: false
        },
        Lead: {
          allOf: [ref('LeadSubmission'), {
            type: 'object', required: ['id', 'status', 'priority', 'submittedAt'], properties: {
              id: { type: 'string' }, status: { enum: ['新咨询', '处理中', '已联系', '已关闭'] },
              priority: { enum: ['低', '普通', '高', '紧急'] }, internalNotes: { type: 'string' },
              submittedAt: { type: 'string', format: 'date-time' }, updatedAt: { type: 'string', format: 'date-time' }
            }
          }]
        },
        Media: {
          type: 'object', required: ['id', 'name', 'key', 'url', 'type', 'size'], properties: {
            id: { type: 'string' }, name: { type: 'string' }, key: { type: 'string' }, url: { type: 'string', format: 'uri' },
            mime: { type: 'string' }, type: { enum: ['image', 'video', 'file'] }, size: { type: 'integer', minimum: 0 },
            storageDriver: { enum: ['local', 's3'] }, category: { type: 'string' }, sourceType: { enum: ['upload', 'direct', 'ai', 'replacement', 'migration'] },
            originalKey: { type: 'string' }, width: { type: ['integer', 'null'] }, height: { type: ['integer', 'null'] },
            checksumSha256: { type: 'string' }, variants: { type: 'object', additionalProperties: true }, metadata: { type: 'object', additionalProperties: true },
            createdAt: { type: 'string', format: 'date-time' }, updatedAt: { type: 'string', format: 'date-time' }
          }
        },
        MediaRuntimeSettings: {
          type: 'object',
          description: 'R2/S3 与 AI 生图运行配置。所有密钥字段均为空，只返回 configured 与掩码状态。',
          properties: {
            config: { type: 'object', additionalProperties: true },
            storageHealth: { type: 'object', additionalProperties: true },
            aiImage: { type: 'object', additionalProperties: true }
          }
        },
        FallbackVersion: {
          type: 'object', required: ['id', 'name', 'active'], properties: {
            id: { type: 'string' }, name: { type: 'string' }, active: { type: 'boolean' },
            createdAt: { type: 'string', format: 'date-time' }, updatedAt: { type: 'string', format: 'date-time' }
          }
        }
      },
      responses: {
        Unauthorized: { description: '未登录或会话失效', content: { 'application/json': { schema: ref('Error') } } },
        ValidationError: { description: '请求参数无效', content: { 'application/json': { schema: ref('Error') } } },
        NotFound: { description: '资源不存在', content: { 'application/json': { schema: ref('Error') } } }
      }
    },
    paths: {
      '/healthz': { get: { summary: '后台健康检查', responses: { 200: jsonResponse({ type: 'object', required: ['ok', 'service'], properties: { ok: { type: 'boolean' }, service: { type: 'string' } } }) } } },
      '/site-admin-auth/status': { get: { summary: '读取后台登录状态', responses: { 200: jsonResponse({ type: 'object', required: ['hasAdmin', 'authenticated'], properties: { hasAdmin: { type: 'boolean' }, authenticated: { type: 'boolean' } } }) } } },
      '/site-admin-auth/logout': { post: { summary: '退出后台', security: auth, responses: { 200: jsonResponse({ type: 'object', properties: { ok: { type: 'boolean' } } }) } } },
      '/site-admin-api/public-content': { get: { summary: '读取官网公开内容', parameters: [{ name: 'locale', in: 'query', schema: { enum: ['zh-CN', 'en', 'ar'], default: 'zh-CN' } }], responses: { 200: jsonResponse({ type: 'object', properties: { data: ref('SiteContent') } }), 409: { description: '译文未就绪、过期或翻译失败' } } } },
      '/site-admin-api/public-locale': { get: { summary: '根据可信 IP 国家头或 Accept-Language 推荐语言', responses: { 200: jsonResponse({ type: 'object', properties: { data: { type: 'object', required: ['locale', 'source'], properties: { locale: { enum: ['zh-CN', 'en', 'ar'] }, source: { type: 'string' }, country: { type: 'string' } } } } }) } } },
      '/site-admin-api/public-fallback-content': { get: { summary: '读取当前启用的降级版本', responses: { 200: jsonResponse({ type: 'object', properties: { data: { type: 'object', properties: { content: ref('SiteContent'), version: ref('FallbackVersion') } } } }) } } },
      '/site-admin-api/leads': {
        options: { summary: '咨询单跨域预检', responses: { 204: { description: '允许的官网来源' }, 403: { $ref: '#/components/responses/ValidationError' } } },
        post: { summary: '提交官网咨询单', requestBody: jsonBody({ type: 'object', required: ['data'], properties: { data: ref('LeadSubmission') } }), responses: { 201: jsonResponse({ type: 'object', properties: { data: ref('Lead') } }), 200: jsonResponse({ type: 'object', description: '重复提交或蜜罐静默成功' }), 400: { $ref: '#/components/responses/ValidationError' }, 429: { description: '提交过于频繁' } } }
      },
      '/site-admin-api/leads/{id}': { patch: { summary: '更新咨询单状态与内部备注', security: auth, parameters: [idParameter], requestBody: jsonBody({ type: 'object', properties: { status: { enum: ['新咨询', '处理中', '已联系', '已关闭'] }, priority: { enum: ['低', '普通', '高', '紧急'] }, internalNotes: { type: 'string', maxLength: 5000 } }, additionalProperties: false }), responses: { 200: jsonResponse({ type: 'object', properties: { data: ref('Lead') } }), 400: { $ref: '#/components/responses/ValidationError' }, 401: { $ref: '#/components/responses/Unauthorized' } } } },
      '/site-admin-api/bootstrap': { get: { summary: '读取后台工作台数据', security: auth, responses: { 200: jsonResponse({ type: 'object', properties: { data: { type: 'object', additionalProperties: true } } }), 401: { $ref: '#/components/responses/Unauthorized' } } } },
      '/site-admin-api/save': { post: { summary: '保存官网内容模块并排队翻译', security: auth, requestBody: jsonBody({ type: 'object', required: ['section', 'data'], properties: { section: { enum: ['content', 'site', 'home', 'productCenter', 'support', 'category', 'product', 'faq', 'staticPage', 'lead'] }, data: { type: 'object' }, revision: { type: 'integer', minimum: 1, description: '客户端读取到的内容修订号，用于防止多页面覆盖。' } }, additionalProperties: false }), responses: { 200: jsonResponse({ type: 'object', properties: { data: { type: 'object', additionalProperties: true } } }), 400: { $ref: '#/components/responses/ValidationError' }, 409: { description: '内容修订冲突' }, 401: { $ref: '#/components/responses/Unauthorized' } } } },
      '/site-admin-api/translations': { get: { summary: '读取中英阿版本和上游配置状态', security: auth, responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }), 401: { $ref: '#/components/responses/Unauthorized' } } } },
      '/site-admin-api/translations/settings': { put: { summary: '加密保存网站翻译上游配置', security: auth, requestBody: jsonBody({ type: 'object', properties: { enabled: { type: 'boolean' }, apiType: { enum: ['chat_completions', 'responses', 'anthropic_messages'] }, baseUrl: { type: 'string', format: 'uri' }, apiKey: { type: 'string', format: 'password' }, clearApiKey: { type: 'boolean' }, model: { type: 'string' }, configuredModels: { type: 'array', items: { type: 'string' } }, batchSize: { type: 'integer', minimum: 5, maximum: 100 }, timeoutMs: { type: 'integer', minimum: 10000, maximum: 600000 } } }), responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }), 400: { $ref: '#/components/responses/ValidationError' } } } },
      '/site-admin-api/translations/models': { post: { summary: '使用当前或新 Key 实时获取翻译上游模型', security: auth, requestBody: jsonBody({ type: 'object', additionalProperties: true }), responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }), 502: { description: '上游模型接口不可用' } } } },
      '/site-admin-api/translations/retranslate': { post: { summary: '按当前中文修订版重新翻译', security: auth, requestBody: jsonBody({ type: 'object', properties: { locale: { enum: ['en', 'ar'] } }, additionalProperties: false }), responses: { 202: jsonResponse({ type: 'object', additionalProperties: true }), 400: { $ref: '#/components/responses/ValidationError' } } } },
      '/site-admin-api/seo/ai-generate': { post: { summary: '根据全站现有内容生成并保存中英阿三语 SEO', description: '使用网站翻译模块已加密保存的 AI 上游配置生成中文 SEO，保存后自动排队生成英文和阿拉伯语译文。覆盖全站默认、首页、产品中心、服务页、品类、产品和静态页；保留已有分享图。', security: auth, requestBody: jsonBody({ type: 'object', properties: { revision: { type: 'integer', minimum: 1, description: '客户端读取到的内容修订号，用于避免生成期间覆盖其他管理员的修改。' } }, additionalProperties: false }), responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }), 400: { $ref: '#/components/responses/ValidationError' }, 401: { $ref: '#/components/responses/Unauthorized' }, 409: { description: '内容修订冲突' }, 502: { description: 'AI 上游生成失败' }, 503: { description: '网站 AI 尚未配置' } } } },
      '/site-admin-api/upload': { post: { summary: '上传媒体（兼容入口）', security: auth, requestBody: { required: true, content: { 'multipart/form-data': { schema: { type: 'object', required: ['file'], properties: { file: { type: 'string', format: 'binary' }, mode: { enum: ['smart', 'original', 'auto'] }, category: { type: 'string' }, preserveOriginal: { type: 'boolean' } } } } } }, responses: { 201: jsonResponse({ type: 'object', properties: { data: ref('Media') } }), 400: { $ref: '#/components/responses/ValidationError' }, 401: { $ref: '#/components/responses/Unauthorized' } } } },
      '/site-admin-api/media/upload': { post: { summary: '智能或原图模式上传媒体', security: auth, requestBody: { required: true, content: { 'multipart/form-data': { schema: { type: 'object', required: ['file'], properties: { file: { type: 'string', format: 'binary' }, mode: { enum: ['smart', 'original'] }, category: { type: 'string' }, preserveOriginal: { type: 'boolean' } } } } } }, responses: { 201: jsonResponse({ type: 'object', properties: { data: ref('Media') } }), 400: { $ref: '#/components/responses/ValidationError' } } } },
      '/site-admin-api/media': { get: { summary: '搜索与分页列出媒体库', security: auth, parameters: [
        { name: 'q', in: 'query', schema: { type: 'string', maxLength: 200 } }, { name: 'category', in: 'query', schema: { type: 'string' } },
        { name: 'type', in: 'query', schema: { enum: ['image', 'video', 'file'] } }, { name: 'page', in: 'query', schema: { type: 'integer', minimum: 1 } },
        { name: 'pageSize', in: 'query', schema: { type: 'integer', minimum: 1, maximum: 100 } }
      ], responses: { 200: jsonResponse({ type: 'object', properties: { data: { type: 'array', items: ref('Media') }, meta: { type: 'object', additionalProperties: true } } }), 401: { $ref: '#/components/responses/Unauthorized' } } } },
      '/site-admin-api/media/stats': { get: { summary: '媒体与对象存储统计', security: auth, responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/media/runtime': { get: { summary: '读取媒体服务、PostgreSQL、CPU、内存与磁盘运行状态', security: auth, responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }), 401: { $ref: '#/components/responses/Unauthorized' } } } },
      '/site-admin-api/media/categories': {
        get: { summary: '列出媒体分类', security: auth, responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } },
        post: { summary: '新建媒体分类', security: auth, requestBody: jsonBody({ type: 'object', required: ['name'], properties: { name: { type: 'string', minLength: 1, maxLength: 80 } } }), responses: { 201: jsonResponse({ type: 'object', additionalProperties: true }) } }
      },
      '/site-admin-api/media/categories/{id}': { delete: { summary: '删除分类并将其素材移入未分类', security: auth, parameters: [idParameter], responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/media/batch-delete': { post: { summary: '批量删除未被引用的媒体', security: auth, requestBody: jsonBody({ type: 'object', required: ['ids'], properties: { ids: { type: 'array', maxItems: 100, items: { type: 'string' } } } }), responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/media/presign': { post: { summary: '申请 S3/R2 预签名直传', security: auth, requestBody: jsonBody({ type: 'object', required: ['name', 'mime', 'size'], properties: { name: { type: 'string' }, mime: { type: 'string' }, size: { type: 'integer', minimum: 1 }, category: { type: 'string' } } }), responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/media/confirm': { post: { summary: '校验并确认预签名直传入库', security: auth, requestBody: jsonBody({ type: 'object', required: ['token'], properties: { token: { type: 'string' } } }), responses: { 201: jsonResponse({ type: 'object', properties: { data: ref('Media') } }) } } },
      '/site-admin-api/media/ai-models': { get: { summary: '读取或同步 OpenAI 图片模型', security: auth, responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/media/ai-generate': { post: { summary: '调用 OpenAI Images API 生图并直接入库', security: auth, requestBody: jsonBody({ type: 'object', required: ['prompt'], properties: { prompt: { type: 'string', minLength: 1, maxLength: 4000 }, model: { type: 'string' }, size: { type: 'string' }, quality: { type: 'string' }, category: { type: 'string' }, preserveOriginal: { type: 'boolean' } } }), responses: { 201: jsonResponse({ type: 'object', properties: { data: ref('Media') } }), 503: { description: 'AI 凭据未配置' } } } },
      '/site-admin-api/media/ai-preview': { post: { summary: '调用 OpenAI Images API 生成临时预览，不立即入库', description: '预览只在服务器内存保留 15 分钟；管理员确认后再提交入库。', security: auth, requestBody: jsonBody({ type: 'object', required: ['prompt'], properties: { prompt: { type: 'string', minLength: 1, maxLength: 4000 }, model: { type: 'string' }, size: { type: 'string' }, quality: { type: 'string' } } }), responses: { 201: jsonResponse({ type: 'object', additionalProperties: true }), 503: { description: 'AI 凭据未配置' } } } },
      '/site-admin-api/media/ai-preview/{id}/commit': { post: { summary: '将 AI 临时结果智能处理并入库', security: auth, parameters: [idParameter], requestBody: jsonBody({ type: 'object', properties: { category: { type: 'string' }, preserveOriginal: { type: 'boolean' } } }), responses: { 201: jsonResponse({ type: 'object', properties: { data: ref('Media') } }), 404: { $ref: '#/components/responses/NotFound' } } } },
      '/site-admin-api/media/ai-preview/{id}': { delete: { summary: '放弃并删除 AI 临时结果', security: auth, parameters: [idParameter], responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }), 404: { $ref: '#/components/responses/NotFound' } } } },
      '/site-admin-api/system/media-settings': { get: { summary: '读取 R2/S3 与 AI 生图配置（密钥仅返回掩码）', security: auth, responses: { 200: jsonResponse({ type: 'object', properties: { data: ref('MediaRuntimeSettings') } }), 401: { $ref: '#/components/responses/Unauthorized' } } } },
      '/site-admin-api/system/media-settings/storage': { patch: { summary: '验证并保存 R2/S3 配置', description: '保存前执行 HeadBucket；存在媒体记录时禁止直接切换存储身份。', security: auth, requestBody: jsonBody({ type: 'object', properties: { endpoint: { type: 'string', format: 'uri' }, region: { type: 'string' }, bucket: { type: 'string' }, publicBaseUrl: { type: 'string', format: 'uri' }, prefix: { type: 'string' }, forcePathStyle: { type: 'boolean' }, serverSideEncryption: { type: 'string' }, accessKeyId: { type: 'string', format: 'password' }, secretAccessKey: { type: 'string', format: 'password' }, sessionToken: { type: 'string', format: 'password' }, clearSessionToken: { type: 'boolean' } } }), responses: { 200: jsonResponse({ type: 'object', properties: { data: ref('MediaRuntimeSettings') } }), 400: { $ref: '#/components/responses/ValidationError' }, 409: { description: '已有素材，禁止切换存储身份' }, 503: { description: 'Bucket 连通验证失败' } } } },
      '/site-admin-api/system/media-settings/image': { patch: { summary: '保存图片处理与上传限制并立即生效', security: auth, requestBody: jsonBody({ type: 'object', properties: { variantWidths: { type: 'string', example: '400,800,1200,1600' }, webpQuality: { type: 'integer', minimum: 40, maximum: 100 }, keepOriginal: { type: 'boolean' }, maxUploadMb: { type: 'number', minimum: 1, maximum: 2048 }, allowedMimeTypes: { type: 'string' } } }), responses: { 200: jsonResponse({ type: 'object', properties: { data: ref('MediaRuntimeSettings') } }), 400: { $ref: '#/components/responses/ValidationError' }, 503: { description: '对象存储验证失败' } } } },
      '/site-admin-api/system/media-settings/ai-image': { patch: { summary: '验证并保存 AI 生图配置', security: auth, requestBody: jsonBody({ type: 'object', properties: { baseUrl: { type: 'string', format: 'uri' }, defaultModel: { type: 'string' }, apiKey: { type: 'string', format: 'password' }, allowedHosts: { type: 'string' }, downloadHosts: { type: 'string' }, configuredModels: { type: 'string' }, requestTimeoutMs: { type: 'integer', minimum: 10000, maximum: 600000 }, clearApiKey: { type: 'boolean' } } }), responses: { 200: jsonResponse({ type: 'object', properties: { data: ref('MediaRuntimeSettings') } }), 400: { $ref: '#/components/responses/ValidationError' }, 503: { description: 'AI 接口验证失败' } } } },
      '/site-admin-api/system/media-settings/ai-image/test': { post: { summary: '测试 AI 生图配置但不保存', security: auth, requestBody: jsonBody({ type: 'object', additionalProperties: true }), responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }), 400: { $ref: '#/components/responses/ValidationError' }, 503: { description: 'AI 接口验证失败' } } } },
      '/site-admin-api/media/{id}/references': { get: { summary: '查看媒体的内容引用', security: auth, parameters: [idParameter], responses: { 200: jsonResponse({ type: 'object', properties: { data: { type: 'object', properties: { media: ref('Media'), references: { type: 'array', items: { type: 'string' } } } } } }), 404: { $ref: '#/components/responses/NotFound' } } } },
      '/site-admin-api/media/{id}/replace': { put: { summary: '保持主 URL 不变替换媒体及变体', security: auth, parameters: [idParameter], requestBody: { required: true, content: { 'multipart/form-data': { schema: { type: 'object', required: ['file'], properties: { file: { type: 'string', format: 'binary' }, mode: { enum: ['smart', 'original'] }, category: { type: 'string' }, preserveOriginal: { type: 'boolean' } } } } } }, responses: { 200: jsonResponse({ type: 'object', properties: { data: ref('Media') } }) } } },
      '/site-admin-api/media/{id}': {
        get: { summary: '读取媒体详情', security: auth, parameters: [idParameter], responses: { 200: jsonResponse({ type: 'object', properties: { data: ref('Media') } }), 404: { $ref: '#/components/responses/NotFound' } } },
        delete: { summary: '删除未被引用的媒体及全部对象存储变体', security: auth, parameters: [idParameter], responses: { 200: jsonResponse({ type: 'object', properties: { data: ref('Media') } }), 409: { description: '媒体仍被内容引用' }, 404: { $ref: '#/components/responses/NotFound' } } }
      },
      '/site-admin-api/fallback-versions': {
        get: { summary: '列出降级版本', security: auth, responses: { 200: jsonResponse({ type: 'object', properties: { data: { type: 'object', properties: { activeId: { type: 'string' }, versions: { type: 'array', items: ref('FallbackVersion') } } } } }) } },
        post: { summary: '从当前内容创建降级版本', security: auth, requestBody: jsonBody({ type: 'object', properties: { name: { type: 'string', maxLength: 80 } } }), responses: { 201: jsonResponse({ type: 'object', additionalProperties: true }) } }
      },
      '/site-admin-api/fallback-versions/{id}/activate': { post: { summary: '启用降级版本', security: auth, parameters: [idParameter], responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }), 404: { $ref: '#/components/responses/NotFound' } } } },
      '/site-admin-api/fallback-versions/{id}': { delete: { summary: '删除降级版本', security: auth, parameters: [idParameter], responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }), 400: { $ref: '#/components/responses/ValidationError' } } } },
      '/site-admin-api/servicebridge/api/admin/agents': {
        get: { summary: '列出客服账号与分组', security: auth, responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } },
        post: { summary: '创建客服账号', security: auth, requestBody: jsonBody({ type: 'object', required: ['account', 'password', 'name'], properties: { account: { type: 'string' }, password: { type: 'string', format: 'password', minLength: 10 }, name: { type: 'string' }, group: { type: 'string' }, max_conversations: { type: 'integer', minimum: 1 } }, additionalProperties: false }), responses: { 201: jsonResponse({ type: 'object', additionalProperties: true }), 400: { $ref: '#/components/responses/ValidationError' } } }
      },
      '/site-admin-api/servicebridge/api/admin/conversations': { get: { summary: '列出客服会话', security: auth, responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/servicebridge/api/conversations/{id}/messages': { get: { summary: '读取会话消息时间线', security: auth, parameters: [idParameter, { name: 'limit', in: 'query', schema: { type: 'integer', minimum: 1, maximum: 200 } }], responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/servicebridge/api/admin/contact-settings': { patch: { summary: '保存 Telephone、WhatsApp、Telegram 与欢迎语', security: auth, requestBody: jsonBody({ type: 'object', properties: { telephone: { type: 'string' }, whatsapp: { type: 'string' }, telegram: { type: 'string' }, entry_reply: { type: 'string' } }, additionalProperties: false }), responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/servicebridge/api/admin/business-hours': { patch: { summary: '保存 IANA 时区和营业时间', security: auth, requestBody: jsonBody({ type: 'object', required: ['timezone', 'start', 'end'], properties: { timezone: { type: 'string', example: 'Asia/Shanghai' }, start: { type: 'string', pattern: '^([01]\\d|2[0-3]):[0-5]\\d$' }, end: { type: 'string', pattern: '^([01]\\d|2[0-3]):[0-5]\\d$' }, enabled: { type: 'boolean' } }, additionalProperties: false }), responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/servicebridge/api/admin/ai-settings': {
        get: { summary: '读取客服 AI 配置（密钥只返回掩码）', security: auth, responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } },
        patch: { summary: '加密保存客服 AI 配置', security: auth, requestBody: jsonBody({ type: 'object', properties: { enabled: { type: 'boolean' }, mode: { enum: ['human_first', 'always_ai', 'manual_only'] }, base_url: { type: 'string', format: 'uri' }, api_key: { type: 'string', format: 'password' }, model: { type: 'string' }, api_type: { enum: ['chat_completions', 'responses', 'anthropic_messages'] }, temperature: { type: 'number', minimum: 0, maximum: 2 }, max_output_tokens: { type: 'integer', minimum: 1 }, timeout_seconds: { type: 'integer', minimum: 1, maximum: 120 }, system_prompt: { type: 'string' } }, additionalProperties: false }), responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } }
      },
      '/site-admin-api/servicebridge/api/admin/ai-settings/models': { post: { summary: '使用当前或新密钥实时获取上游模型', security: auth, requestBody: jsonBody({ type: 'object', properties: { base_url: { type: 'string', format: 'uri' }, api_key: { type: 'string', format: 'password' }, api_type: { enum: ['chat_completions', 'responses', 'anthropic_messages'] }, timeout_seconds: { type: 'integer', minimum: 1, maximum: 120 } }, additionalProperties: false }), responses: { 200: jsonResponse({ type: 'object', required: ['models'], properties: { models: { type: 'array', items: { type: 'string' } } } }), 502: { description: '上游模型接口不可用' } } } },
      '/site-admin-api/servicebridge/api/admin/keyword-rules': { get: { summary: '列出关键词自动回复规则', security: auth, responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } }, post: { summary: '创建关键词规则', security: auth, requestBody: jsonBody({ type: 'object', properties: { keyword: { type: 'string' }, match_type: { enum: ['contains', 'exact', 'prefix'] }, action: { enum: ['text', 'telephone', 'whatsapp', 'telegram', 'handoff'] }, reply: { type: 'string' }, enabled: { type: 'boolean' }, priority: { type: 'integer' } }, additionalProperties: true }), responses: { 201: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/servicebridge/api/admin/audit-events': { get: { summary: '读取客服系统审计日志', security: auth, parameters: [{ name: 'limit', in: 'query', schema: { type: 'integer', minimum: 1, maximum: 200 } }], responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/servicebridge/api/admin/api-apps': { get: { summary: '列出第三方平台 API 授权', security: auth, responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } }, post: { summary: '创建 API 授权（完整 Key 仅返回一次）', security: auth, requestBody: jsonBody({ type: 'object', required: ['name'], properties: { name: { type: 'string' }, note: { type: 'string' }, rate_limit: { type: 'integer', minimum: 0 }, enabled: { type: 'boolean' } }, additionalProperties: false }), responses: { 201: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/servicebridge/api/admin/api-apps/{id}': { patch: { summary: '更新 API 授权（名称、备注、限速、启用状态）', security: auth, parameters: [idParameter], requestBody: jsonBody({ type: 'object', properties: { name: { type: 'string' }, note: { type: 'string' }, rate_limit: { type: 'integer', minimum: 0 }, enabled: { type: 'boolean' } }, additionalProperties: false }), responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } }, delete: { summary: '删除 API 授权', security: auth, parameters: [idParameter], responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/servicebridge/api/admin/api-apps/{id}/reset-key': { post: { summary: '重置 API Key（旧 Key 立即失效，新 Key 仅返回一次）', security: auth, parameters: [idParameter], responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/publish': { post: { summary: '创建官网发布任务', description: 'source=current 发布当前草稿；source=fallback 发布当前启用的降级版本。', security: auth, parameters: [{ name: 'source', in: 'query', schema: { enum: ['current', 'fallback'], default: 'current' } }], responses: { 202: jsonResponse({ type: 'object', additionalProperties: true }), 503: { description: '重建服务不可用' } } } },
      '/site-admin-api/status': { get: { summary: '读取官网与构建状态', security: auth, responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/audit-events': { get: { summary: '读取后台审计日志', security: auth, parameters: [{ name: 'limit', in: 'query', schema: { type: 'integer', minimum: 1, maximum: 1000 } }], responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } },
      '/site-admin-api/publish-jobs': { get: { summary: '读取发布队列', security: auth, parameters: [{ name: 'limit', in: 'query', schema: { type: 'integer', minimum: 1, maximum: 1000 } }], responses: { 200: jsonResponse({ type: 'object', additionalProperties: true }) } } }
    }
  };
}
