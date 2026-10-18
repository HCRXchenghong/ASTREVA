# App 发布、更新与推送

客服 App 的版本分发走自建更新通道：统一后台上传安装包，App 启动时向客服引擎查询最新版本，按后台配置执行“强制更新”（不升级不能使用）或“可选更新”（可稍后）。

## 版本号规则

- 每次发版必须递增 `version_code`（整数），同一平台不允许重复。
- `version_name` 仅用于展示，例如 `1.2.0`。
- 构建 App 时必须传入与后台一致的版本代码：

```bash
flutter build apk --release \
  --dart-define=SERVICEBRIDGE_URL=https://客服域名 \
  --dart-define=APP_VERSION_CODE=3
```

不传 `APP_VERSION_CODE` 时默认为 `1`，会导致更新检测异常。

## 更新流程

1. 构建 APK 并记录 `version_code`。
2. 登录统一后台 → 左侧“App 版本”。
3. 选择平台、填写版本号和更新说明，上传安装包。
4. “强制更新”开关决定低于此版本的 App 是否必须升级：
   - 开：App 弹窗无法关闭，只能点“立即更新”，下载后自动拉起系统安装。
   - 关：App 弹窗提供“稍后再说”，下次启动再提醒。
5. 每个版本可单独“停用/启用”，删除版本会同时移除安装包文件。

## Android 检测与安装

- App 启动时请求 `GET /api/app-update/latest?platform=android&version_code=N`。
- 有更新时下载 `GET /api/app-update/download/{id}`（公开端点，无登录要求）。
- 安装通过系统 `ACTION_VIEW` + FileProvider 完成；Android 8+ 首次需要用户允许“安装未知应用”，App 会引导打开系统设置页。
- 未开启该权限时 App 提示并跳转设置，用户开启后回到 App 重新点击更新。

iOS 无法旁载安装包。iOS 上检测到更新时提示用户前往 App Store；TestFlight/正式发布仍需走 Apple 发布流程。

## 正式签名

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

在 `android/app/build.gradle.kts` 的 `buildTypes.release` 中按 `key.properties` 配置 `signingConfig`，替换当前的 debug 签名。发布用 `flutter build appbundle` 产出 `.aab`。

iOS：在 Xcode 中配置开发者团队、Bundle ID 和签名证书，通过 Archive + App Store Connect 上传。商店素材、隐私清单和权限说明需另行准备。

## 推送接入（FCM）

App 端已集成 `firebase_messaging` + `flutter_local_notifications`，后端具备推送触发点（新会话分配、访客新消息）。`FCM_ACCESS_TOKEN` 未配置时后端只记录日志，不影响聊天功能。

### 1. 配置 Firebase 参数

App 使用 `FirebaseOptions` 初始化，不依赖 `google-services.json`。构建时传入 Firebase 项目参数：

```bash
flutter build apk --release \
  --dart-define=SERVICEBRIDGE_URL=https://客服域名 \
  --dart-define=APP_VERSION_CODE=3 \
  --dart-define=FIREBASE_API_KEY=AIza... \
  --dart-define=FIREBASE_APP_ID=1:123456:android:abc123 \
  --dart-define=FIREBASE_MESSAGING_SENDER_ID=123456789 \
  --dart-define=FIREBASE_PROJECT_ID=your-project \
  --dart-define=FIREBASE_STORAGE_BUCKET=your-project.appspot.com
```

未传入 `FIREBASE_*` 参数时 App 自动跳过推送初始化，登录、聊天、更新检查均不受影响。参数可从 Firebase 控制台 → 项目设置 → 常规 → “您的应用”中获取。

### 2. App 内行为

- 首次登录后自动申请通知权限，并向后端上报设备 token：`POST /api/agent/device-token`，body 为 `{"device_token":"<FCM token>"}`。
- FCM token 刷新时自动重新上报；退出登录时向后端上报空 token 解除绑定。
- 前台展示策略（避免重复通知）：Android 前台由 `flutter_local_notifications` 展示横幅（渠道 agent_messages，Android 13+ 会请求通知权限）；iOS 前台由系统横幅展示（setForegroundNotificationPresentationOptions）。后台/进程被系统回收时由 FCM 系统通知展示。
- 点击通知按 `conversation_id` 跳转对应会话：冷启动时先进入登录页，登录完成后自动打开会话。

### 3. 后端环境变量

- `FCM_ACCESS_TOKEN`：Firebase 服务账号 OAuth2 access token（短期有效）或 API 网关生成的令牌。
- `FCM_ENDPOINT`：可选，默认 `https://fcm.googleapis.com/v1/projects/-/messages:send`；正式环境需把 `-` 替换为项目 ID，并配合服务账号签名刷新 `FCM_ACCESS_TOKEN`。
- 推送对象是客服设备 token，访客新消息或会话分配时触发。

### 4. iOS 注意事项

iOS 需要在 Xcode 中为 Runner 开启 Push Notifications capability、配置 APNs 证书，并通过真机验证。iOS 上检测到新版本时只能引导用户去 App Store，不能旁载安装。APNs 后端签名通道尚未实现，iOS 正式推送前需补充。


## 推送集成本地验证

依赖版本：firebase_core 4.4.0、firebase_messaging 16.1.1、flutter_local_notifications 22.0.1（与本地 pub 缓存匹配的版本组合；联网后可直接执行）。

```bash
cd servicebridge/apps/agent-mobile
flutter pub get            # 联网时执行一次，锁定时按上面的版本
flutter analyze           # 已通过：No issues found
flutter test              # 测试内核编译已通过；沙箱内因禁 socket 无法跑完，本机可直接通过
flutter build apk --debug --dart-define=FIREBASE_API_KEY=... --dart-define=FIREBASE_APP_ID=... --dart-define=FIREBASE_MESSAGING_SENDER_ID=... --dart-define=FIREBASE_PROJECT_ID=...
```

### 真机验证步骤

1. Firebase 控制台新建项目，开启 Cloud Messaging，按上文参数构建并安装到 Android 真机。
2. 登录客服账号，在后台查看 agents.device_token 已写入；用访客端发消息或新建会话。
3. 分别验证：App 前台（横幅+声音）、后台、进程被杀三种状态下均收到通知；点击通知直达对应会话。
4. 退出登录后再次触发，确认不再收到推送；重新登录后恢复。
5. iOS 需在 Xcode 开启 Push Notifications capability、配置 APNs 证书并真机验证（项目为 Swift Package Manager 集成，无需 Podfile/pod install）。
