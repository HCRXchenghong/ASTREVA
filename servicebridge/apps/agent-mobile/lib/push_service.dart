import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import 'service_bridge.dart';

class PushService {
  PushService._();

  static final PushService instance = PushService._();

  final GlobalKey<NavigatorState> navigatorKey = GlobalKey<NavigatorState>();

  // B1：构建期 dart-define 只作为可选覆盖——非空时优先于服务端下发的参数。
  static const _apiKey = String.fromEnvironment('FIREBASE_API_KEY');
  static const _appId = String.fromEnvironment('FIREBASE_APP_ID');
  static const _messagingSenderId = String.fromEnvironment(
    'FIREBASE_MESSAGING_SENDER_ID',
  );
  static const _projectId = String.fromEnvironment('FIREBASE_PROJECT_ID');
  static const _storageBucket = String.fromEnvironment(
    'FIREBASE_STORAGE_BUCKET',
  );
  static const _vapidKey = String.fromEnvironment('FIREBASE_VAPID_KEY');

  /// 构建期是否提供了完整的 Firebase 参数。
  static bool get dartDefineConfigured =>
      _apiKey.isNotEmpty && _appId.isNotEmpty && _projectId.isNotEmpty;

  FlutterLocalNotificationsPlugin? _localNotifications;
  FirebaseMessaging? _messaging;
  ServiceBridgeClient? _client;
  Future<void>? _initFuture;
  bool _firebaseReady = false;
  bool _messagingConfigured = false;
  bool _configInFlight = false;
  bool _serverPushDisabled = false;
  String? _deviceToken;
  String? _pendingConversationId;

  Future<void> Function(String conversationId)? openHandler;

  String? get deviceToken => _deviceToken;

  /// 推送通道是否已就绪（本地通知 + Firebase）。
  bool get configured => _firebaseReady;

  String? consumePendingConversation() {
    final pending = _pendingConversationId;
    _pendingConversationId = null;
    return pending;
  }

  Future<void> init() {
    return _initFuture ??= _doInit();
  }

  Future<void> _doInit() async {
    await _initLocalNotifications();
    if (dartDefineConfigured) {
      // 显式构建配置优先，无需等待服务端。
      await _initializeFirebase(
        FirebaseOptions(
          apiKey: _apiKey,
          appId: _appId,
          messagingSenderId: _messagingSenderId,
          projectId: _projectId,
          storageBucket: _storageBucket.isEmpty ? null : _storageBucket,
        ),
      );
    }
  }

  /// B1：启动（或登录）后用服务端下发的参数初始化 Firebase。
  /// 参数为空、服务端关闭推送或请求失败时静默跳过，绝不阻塞登录。
  Future<void> configureFromServer(String baseUrl) async {
    await init();
    if (_firebaseReady || _serverPushDisabled || _configInFlight) return;
    final normalized = baseUrl.trim();
    if (normalized.isEmpty) return;
    _configInFlight = true;
    try {
      final config = await ServiceBridgeClient(
        baseUrl: ServiceBridgeClient.normalizeBaseUrl(normalized),
      ).appConfig();
      if (!config.pushEnabled && !dartDefineConfigured) {
        _serverPushDisabled = true;
        debugPrint('[push] 服务端未启用推送，跳过初始化');
        return;
      }
      final firebase = config.firebase;
      // dart-define 非空字段优先覆盖服务端参数。
      final apiKey = _apiKey.isNotEmpty ? _apiKey : firebase.apiKey;
      final appId = _appId.isNotEmpty ? _appId : firebase.appId;
      final projectId = _projectId.isNotEmpty ? _projectId : firebase.projectId;
      final senderId = _messagingSenderId.isNotEmpty
          ? _messagingSenderId
          : firebase.messagingSenderId;
      final bucket = _storageBucket.isNotEmpty
          ? _storageBucket
          : firebase.storageBucket;
      if (apiKey.isEmpty || appId.isEmpty || projectId.isEmpty) {
        debugPrint('[push] 服务端 Firebase 参数不完整，跳过推送初始化');
        return;
      }
      await _initializeFirebase(
        FirebaseOptions(
          apiKey: apiKey,
          appId: appId,
          messagingSenderId: senderId,
          projectId: projectId,
          storageBucket: bucket.isEmpty ? null : bucket,
        ),
      );
    } catch (error) {
      debugPrint('[push] 读取 /api/app-config 失败，跳过推送初始化：$error');
    } finally {
      _configInFlight = false;
    }
  }

  Future<void> _initializeFirebase(FirebaseOptions options) async {
    if (_firebaseReady) return;
    try {
      await Firebase.initializeApp(options: options);
      _firebaseReady = true;
    } catch (error) {
      debugPrint('[push] Firebase 初始化失败，聊天功能不受影响：$error');
      return;
    }
    await _setupMessaging();
  }

  Future<void> _setupMessaging() async {
    if (_messagingConfigured) return;
    _messagingConfigured = true;
    final messaging = FirebaseMessaging.instance;
    _messaging = messaging;
    try {
      await messaging.requestPermission(
        alert: true,
        badge: true,
        sound: true,
        provisional: false,
      );
      // iOS 前台由系统横幅展示（本地通知仅用于 Android 前台，
      // 避免 iOS 上出现重复横幅）。
      await messaging.setForegroundNotificationPresentationOptions(
        alert: true,
        badge: true,
        sound: true,
      );

      _deviceToken = await messaging.getToken();
      debugPrint('[push] FCM token: $_deviceToken');

      FirebaseMessaging.onMessage.listen(_showForegroundNotification);
      FirebaseMessaging.onMessageOpenedApp.listen((message) {
        _handleConversationTap(message.data['conversation_id']);
      });
      messaging.onTokenRefresh.listen((token) async {
        _deviceToken = token;
        final client = _client;
        if (client != null) {
          await _reportDeviceToken(client, token);
        }
      });

      final initial = await messaging.getInitialMessage();
      if (initial != null) {
        _pendingConversationId = initial.data['conversation_id'];
      }
    } catch (error) {
      debugPrint('[push] 消息通道初始化失败，聊天功能不受影响：$error');
    }
  }

  Future<void> _initLocalNotifications() async {
    final plugin = FlutterLocalNotificationsPlugin();
    const settings = InitializationSettings(
      android: AndroidInitializationSettings('@mipmap/ic_launcher'),
      iOS: DarwinInitializationSettings(),
    );
    await plugin.initialize(
      settings: settings,
      onDidReceiveNotificationResponse: (response) {
        _handleConversationTap(response.payload);
      },
    );
    await plugin
        .resolvePlatformSpecificImplementation<
          AndroidFlutterLocalNotificationsPlugin
        >()
        ?.requestNotificationsPermission();
    _localNotifications = plugin;
  }

  Future<void> _showForegroundNotification(RemoteMessage message) async {
    if (defaultTargetPlatform != TargetPlatform.android) return;
    final plugin = _localNotifications;
    if (plugin == null) return;
    final title = message.notification?.title ?? '在线客服';
    final body = message.notification?.body ?? '您有新的客服消息';
    const details = NotificationDetails(
      android: AndroidNotificationDetails(
        'agent_messages',
        '客服消息',
        channelDescription: '新会话与新消息提醒',
        importance: Importance.high,
        priority: Priority.high,
      ),
      iOS: DarwinNotificationDetails(),
    );
    await plugin.show(
      id: message.hashCode & 0x7fffffff,
      title: title,
      body: body,
      notificationDetails: details,
      payload: message.data['conversation_id'],
    );
  }

  Future<void> showLocalNotification({
    required String title,
    required String body,
    String? conversationId,
  }) async {
    try {
      await init();
      final plugin = _localNotifications;
      if (plugin == null) return;
      const details = NotificationDetails(
        android: AndroidNotificationDetails(
          'agent_messages',
          '客服消息',
          channelDescription: '新会话与新消息提醒',
          importance: Importance.high,
          priority: Priority.high,
        ),
        iOS: DarwinNotificationDetails(),
      );
      await plugin.show(
        id: DateTime.now().millisecondsSinceEpoch & 0x7fffffff,
        title: title,
        body: body,
        notificationDetails: details,
        payload: conversationId,
      );
    } catch (error) {
      debugPrint('[push] 本地通知失败：$error');
    }
  }

  Future<void> registerToBackend(ServiceBridgeClient client) async {
    _client = client;
    await init();
    await configureFromServer(client.baseUrl);
    final token = _deviceToken;
    if (token == null || token.isEmpty) return;
    await _reportDeviceToken(client, token);
  }

  Future<void> unregisterFromBackend() async {
    final client = _client;
    _client = null;
    if (client == null) return;
    // B1：退出登录时仍然上报空 token，清掉后端记录。
    try {
      await client.registerDeviceToken('');
    } catch (_) {
      // Best effort: token cleanup must never break logout.
    }
  }

  Future<void> _reportDeviceToken(
    ServiceBridgeClient client,
    String token,
  ) async {
    try {
      await client.registerDeviceToken(token);
    } catch (error) {
      debugPrint('[push] 设备 token 上报失败：$error');
    }
  }

  void _handleConversationTap(String? conversationId) {
    final id = conversationId?.trim() ?? '';
    if (id.isEmpty) return;
    _pendingConversationId = id;
    final handler = openHandler;
    if (handler != null) {
      handler(id);
    }
  }
}
