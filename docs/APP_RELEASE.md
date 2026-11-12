# App 发布、更新与推送

> 版本：v1.4.5 · 更新时间：2026-11-10

客服 App 的版本分发走自建更新通道：统一控制台上传安装包，App 启动时向客服引擎查询最新版本，按后台配置执行「强制更新」（不升级不能使用）或「可选更新」（可稍后）。推送参数同样在控制台里配置，不再依赖构建期参数。

## 版本号规则

- 每次发版必须递增 `version_code`（整数），同一平台不允许重复。
- `version_name` 仅用于展示，例如 `1.2.0`。
- 构建 App 时必须传入与后台一致的版本代码：

```bash
flutter build apk --release \
  --dart-define=SERVICEBRIDGE_URL=https://客服域名 \
  --dart-define=APP_VERSION_CODE=3
```

不传 `APP_VERSION_CODE` 时默认为 `1`，会导致更新检测异常。客服端服务器地址必须通过 `--dart-define=SERVICEBRIDGE_URL=https://客服域名` 编译内置，登录页不再显示服务器地址输入框。

## 更新流程

1. 构建 APK 并记录 `version_code`。
2. 登录统一控制台 → **应用管理 → App 版本**。
3. 选择平台、填写版本号和更新说明，上传安装包。
4. 勾选「强制更新」（或在版本列表里点「设为强制」）：低于此版本的 App 必须升级才能继续使用；不勾选则可选更新。
5. 每个版本可单独「停用/启用」，删除版本会同时移除安装包文件。

App 启动时请求 `GET /api/app-update/latest?platform=android&version_code=N`，有更新时下载 `GET /api/app-update/download/{id}`（公开端点）。Android 下载完成后会校验后台记录的 SHA-256（`checksum`），校验失败会删除文件并报错。

## Android 检测与安装

- 安装通过系统 `ACTION_VIEW` + FileProvider 完成；Android 8+ 首次需要用户允许「安装未知应用」，App 会引导打开系统设置页。
- 强制更新弹窗会拦截系统返回键，不能绕过。
- 未开启安装权限时 App 提示并跳转设置，用户开启后回到 App 重新点击更新。

iOS 无法旁载安装包。iOS 上检测到更新时提示用户前往 App Store；TestFlight/正式发布仍需走 Apple 发布流程。

## 推送配置（控制台可配）

推送不再需要 `--dart-define=FIREBASE_*`：App 启动时调用 `GET /api/app-config` 获取公开的 Firebase 参数并初始化；参数为空或 `push_enabled=false` 时自动跳过推送初始化，不影响聊天。

配置步骤：

1. Firebase 控制台新建项目并开启 Cloud Messaging。
2. 控制台 → **系统设置 → 推送设置**，填入：
   - Firebase 客户端参数：`api_key`、`app_id`、`messaging_sender_id`、`project_id`、`storage_bucket`（可选 `vapid_key`）；
   - 服务端密钥：Firebase 服务账号 JSON（推荐，服务端会自动换取并缓存 OAuth2 令牌）；
   - iOS 额外填 APNs `.p8` 私钥、Key ID、Apple Team ID、Topic（Bundle ID）与环境（production/sandbox）。
3. 点击「验证凭证」：FCM 会真实换取一次 OAuth2 令牌，APNs 会真实签出一次 JWT；结果会分项展示。
4. 保存后立即生效，无需重启服务或重新构建 App。

安全说明：服务端密钥使用 `DATA_ENCRYPTION_KEY` 做 AES-256-GCM 加密存入 PostgreSQL，接口只返回是否已配置与末四位掩码，永不回显明文；清除密钥有独立按钮。

App 行为：

- 登录后申请通知权限并上报设备 token（`POST /api/agent/device-token`，带 `platform`）。
- token 刷新时自动重新上报；退出登录时上报空 token 解除绑定。
- 前台 Android 用本地通知渠道 `agent_messages` 展示，iOS 由系统横幅展示；后台/进程被回收时由 FCM/APNs 系统通知展示。
- 点击通知按 `conversation_id` 跳转；冷启动时先登录再自动打开会话。

后端触发点：访客新消息、**新会话分配**、**会话转接**都会推送；未配置任何通道时只记录日志，不影响聊天。

## 正式签名（待办）

Android 与 iOS 的正式签名、商店素材、隐私清单仍未完成，需要发布负责人执行：

Android：

```bash
keytool -genkeypair -v -keystore ~/astreva-release.keystore \
  -keyalg RSA -keysize 2048 -validity 10000 -alias astreva

mkdir -p android/key.properties
cat > android/key.properties << 'PROPS'
storePassword=替换为你的密码
keyPassword=替换为你的密码
keyAlias=astreva
storeFile=/绝对路径/astreva-release.keystore
PROPS
```

在 `android/app/build.gradle.kts` 的 `buildTypes.release` 中按 `key.properties` 配置 `signingConfig`，替换当前的 debug 签名；发布用 `flutter build appbundle`。

iOS：在 Xcode 中配置开发者团队、Bundle ID 和签名证书，开启 Push Notifications capability（`Runner.entitlements` 已包含 `aps-environment`，正式发布前改成 `production`），通过 Archive + App Store Connect 上传。

## 本地验证

```bash
cd servicebridge/apps/agent-mobile
PUB_CACHE="$PWD/.pub-cache" flutter pub get    # 依赖下载到仓库内，避免污染系统 pub 缓存
dart analyze
flutter test
flutter build apk --debug \
  --dart-define=SERVICEBRIDGE_URL=https://客服域名 \
  --dart-define=APP_VERSION_CODE=3
```

真机验证步骤：

1. 按上文完成推送配置并点「验证凭证」通过。
2. 登录客服账号，在控制台确认该客服的 `device_platform` 与 `device_token` 已写入。
3. 用访客端发消息、新建会话、后台转接三种方式各触发一次，分别验证 App 前台、后台、进程被杀三种状态都能收到通知，点击直达对应会话。
4. 退出登录后再次触发，确认不再收到推送；重新登录后恢复。
5. iOS 需真机 + APNs 证书验证；模拟器无法验证远程推送。
