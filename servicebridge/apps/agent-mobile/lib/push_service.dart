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

  static const _apiKey = String.fromEnvironment('FIREBASE_API_KEY');
  static const _appId = String.fromEnvironment('FIREBASE_APP_ID');
  static const _messagingSenderId = String.fromEnvironment(
    'FIREBASE_MESSAGING_SENDER_ID',
  );
  static const _projectId = String.fromEnvironment('FIREBASE_PROJECT_ID');
  static const _storageBucket = String.fromEnvironment(
    'FIREBASE_STORAGE_BUCKET',
  );

  static bool get configured =>
      _apiKey.isNotEmpty && _appId.isNotEmpty && _projectId.isNotEmpty;

  FlutterLocalNotificationsPlugin? _localNotifications;
  FirebaseMessaging? _messaging;
  ServiceBridgeClient? _client;
  Future<void>? _initFuture;
  String? _deviceToken;
  String? _pendingConversationId;

  Future<void> Function(String conversationId)? openHandler;

  String? get deviceToken => _deviceToken;

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
    if (!configured) {
      debugPrint('[push] Firebase 未配置，跳过推送初始化');
      return;
    }
    try {
      await Firebase.initializeApp(
        options: FirebaseOptions(
          apiKey: _apiKey,
          appId: _appId,
          messagingSenderId: _messagingSenderId,
          projectId: _projectId,
          storageBucket: _storageBucket.isEmpty ? null : _storageBucket,
        ),
      );
      _messaging = FirebaseMessaging.instance;

      await _messaging!.requestPermission(
        alert: true,
        badge: true,
        sound: true,
        provisional: false,
      );
      // iOS 前台由系统横幅展示（本地通知仅用于 Android 前台，
      // 避免 iOS 上出现重复横幅）。
      await _messaging!.setForegroundNotificationPresentationOptions(
        alert: true,
        badge: true,
        sound: true,
      );

      _deviceToken = await _messaging!.getToken();
      debugPrint('[push] FCM token: $_deviceToken');

      FirebaseMessaging.onMessage.listen(_showForegroundNotification);
      FirebaseMessaging.onMessageOpenedApp.listen((message) {
        _handleConversationTap(message.data['conversation_id']);
      });
      _messaging!.onTokenRefresh.listen((token) async {
        _deviceToken = token;
        final client = _client;
        if (client != null) {
          await _reportDeviceToken(client, token);
        }
      });

      final initial = await _messaging!.getInitialMessage();
      if (initial != null) {
        _pendingConversationId = initial.data['conversation_id'];
      }
    } catch (error) {
      debugPrint('[push] 初始化失败，聊天功能不受影响：$error');
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
    if (!configured || _deviceToken == null) return;
    await _reportDeviceToken(client, _deviceToken!);
  }

  Future<void> unregisterFromBackend() async {
    final client = _client;
    _client = null;
    if (!configured) return;
    if (client != null) {
      try {
        await client.registerDeviceToken('');
      } catch (_) {
        // Best effort: token cleanup must never break logout.
      }
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
