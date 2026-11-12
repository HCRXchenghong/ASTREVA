# 在线客服 Flutter App

> 版本：v1.4.5 · 更新时间：2026-11-10

ASTREVA 客服坐席 App，应用名称为“在线客服”，支持 iOS 和 Android。登录账号由统一后台“客服管理”创建；App 只提供在线客服接待功能，不包含官网内容或系统管理。

## 功能

- 客服账号密码登录；服务器地址编译内置，登录页不显示。
- 在线、忙碌、离线状态切换。
- 会话列表、状态、未读数、历史消息和已读回执。
- WebSocket 心跳与断线重连，轮询补偿会话变化。
- 发送文字和图片、查看图片、结束会话、修改密码。
- 使用官网透明 Logo；iOS App Icon 按平台要求使用无透明通道版本。

App 已接入 FCM/APNs 推送（新会话分配、转接、访客新消息），推送参数由统一后台「系统设置 → 推送设置」下发；未配置 Firebase/APNs 凭证或未授权通知时，退到后台/进程被回收后不能保证收到提醒。正式发布还需配置签名、推送证书、隐私清单和商店资料。

## 运行

```bash
flutter pub get
dart analyze
flutter test          # 当前仓库已有测试；若本机未装依赖，按 CI 结果为准
flutter run --dart-define=SERVICEBRIDGE_URL=http://电脑局域网IP:8080
```

服务器地址必须通过编译期 `--dart-define=SERVICEBRIDGE_URL=...` 内置，登录页只保留账号和密码，不再向客服显示/允许修改服务器地址。Android 模拟器可用 `http://10.0.2.2:8080`；真机必须与开发电脑处于可互通网络，并保证 Go 后端监听 `0.0.0.0:8080`。

## 构建

```bash
flutter build apk --debug --dart-define=SERVICEBRIDGE_URL=https://客服域名
flutter build ios --simulator --debug --dart-define=SERVICEBRIDGE_URL=https://客服域名
```

Android 真机调试：

```bash
adb devices
adb install -r build/app/outputs/flutter-apk/app-debug.apk
```

iOS 真机安装需要在 Xcode 中配置开发团队和签名；模拟器构建不等于已完成 App Store 发布。
