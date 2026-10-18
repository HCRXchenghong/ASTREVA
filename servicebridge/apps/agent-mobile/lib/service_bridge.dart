import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'alert_models.dart';
import 'models.dart';
import 'profile_models.dart';

class LoginResult {
  const LoginResult({required this.token, required this.agent});

  final String token;
  final AgentProfile agent;
}

/// 一次分页历史消息（`GET /api/conversations/{id}/messages`）。
class MessagePage {
  const MessagePage({
    required this.messages,
    required this.hasMore,
    required this.nextBefore,
  });

  final List<ChatMessage> messages;
  final bool hasMore;
  final String nextBefore;

  static const empty = MessagePage(
    messages: <ChatMessage>[],
    hasMore: false,
    nextBefore: '',
  );
}

/// 服务端下发的 Firebase 客户端参数（`GET /api/app-config`）。
class FirebaseClientConfig {
  const FirebaseClientConfig({
    this.apiKey = '',
    this.appId = '',
    this.messagingSenderId = '',
    this.projectId = '',
    this.storageBucket = '',
    this.vapidKey = '',
  });

  factory FirebaseClientConfig.fromJson(Map<String, dynamic> json) =>
      FirebaseClientConfig(
        apiKey: '${json['api_key'] ?? ''}'.trim(),
        appId: '${json['app_id'] ?? ''}'.trim(),
        messagingSenderId: '${json['messaging_sender_id'] ?? ''}'.trim(),
        projectId: '${json['project_id'] ?? ''}'.trim(),
        storageBucket: '${json['storage_bucket'] ?? ''}'.trim(),
        vapidKey: '${json['vapid_key'] ?? ''}'.trim(),
      );

  final String apiKey;
  final String appId;
  final String messagingSenderId;
  final String projectId;
  final String storageBucket;
  final String vapidKey;
}

class AppConfig {
  const AppConfig({
    required this.firebase,
    required this.pushEnabled,
    required this.apnsEnabled,
  });

  factory AppConfig.fromJson(Map<String, dynamic> json) => AppConfig(
    firebase: FirebaseClientConfig.fromJson(
      Map<String, dynamic>.from(json['firebase'] as Map? ?? const {}),
    ),
    pushEnabled: json['push_enabled'] == true,
    apnsEnabled: json['apns_enabled'] == true,
  );

  final FirebaseClientConfig firebase;
  final bool pushEnabled;
  final bool apnsEnabled;
}

class ServiceBridgeException implements Exception {
  const ServiceBridgeException(
    this.message, {
    this.unauthorized = false,
    this.statusCode = 0,
  });

  final String message;

  /// 401/登录态失效时为 true，调用方据此清理本地状态并跳回登录页。
  final bool unauthorized;

  /// HTTP 状态码；0 表示非 HTTP 层面的错误（如响应缺字段）。
  /// 画像接口据此区分 403（越权）与 404（未开启画像/无画像）。
  final int statusCode;

  @override
  String toString() => message;
}

/// 便捷判定：任意异常是否为登录态失效。
bool isUnauthorizedError(Object error) =>
    error is ServiceBridgeException && error.unauthorized;

class ServiceBridgeClient {
  ServiceBridgeClient({required this.baseUrl});

  String baseUrl;
  String token = '';
  AgentProfile? agent;
  WebSocketChannel? _channel;
  StreamSubscription<dynamic>? _socketSubscription;
  Timer? _heartbeat;
  Timer? _reconnectTimer;
  bool _closedByUser = false;

  /// 指数退避重连（1s → 2s → 4s … 上限 30s），连接成功后归零。
  static const int _maxReconnectDelaySeconds = 30;
  int _reconnectAttempts = 0;

  /// 收到 `session.revoked` 后停止一切重连。
  bool _sessionRevoked = false;
  bool get sessionRevoked => _sessionRevoked;

  /// 任何请求返回 401 时回调（B2），由 HomePage 注册以清理状态并跳回登录页。
  void Function()? onUnauthorized;

  void _notifyUnauthorized() {
    final handler = onUnauthorized;
    if (handler == null) return;
    // 避免回调中的异常影响原始错误抛出。
    try {
      handler();
    } catch (_) {}
  }

  final _eventController = StreamController<Map<String, dynamic>>.broadcast();
  Stream<Map<String, dynamic>> get events => _eventController.stream;

  static Future<String> savedServerUrl() async {
    final preferences = await SharedPreferences.getInstance();
    return preferences.getString('servicebridge_url') ??
        const String.fromEnvironment(
          'SERVICEBRIDGE_URL',
          defaultValue: 'http://10.0.2.2:8080',
        );
  }

  Future<void> saveServerUrl(String value) async {
    baseUrl = normalizeBaseUrl(value);
    final preferences = await SharedPreferences.getInstance();
    await preferences.setString('servicebridge_url', baseUrl);
  }

  static String normalizeBaseUrl(String value) {
    var normalized = value.trim();
    if (!normalized.startsWith('http://') &&
        !normalized.startsWith('https://')) {
      normalized = 'http://$normalized';
    }
    final uri = Uri.tryParse(normalized);
    if (uri == null || uri.host.isEmpty) {
      throw const ServiceBridgeException('请输入有效的服务器地址');
    }
    return normalized.replaceFirst(RegExp(r'/+$'), '');
  }

  Future<LoginResult> login(String account, String password) async {
    final payload = await _request(
      'POST',
      '/api/agent/login',
      body: {'account': account.trim(), 'password': password},
      authenticated: false,
    );
    token = '${payload['token'] ?? ''}';
    agent = AgentProfile.fromJson(
      Map<String, dynamic>.from(payload['agent'] as Map? ?? {}),
    );
    if (token.isEmpty || agent!.id.isEmpty) {
      throw const ServiceBridgeException('登录响应不完整');
    }
    return LoginResult(token: token, agent: agent!);
  }

  Future<AgentProfile> setStatus(String status) async {
    final payload = await _request(
      'POST',
      '/api/agent/status',
      body: {'status': status},
    );
    final updated = AgentProfile.fromJson(
      Map<String, dynamic>.from(payload['agent'] as Map? ?? {}),
    );
    agent = updated;
    return updated;
  }

  Future<AgentProfile> updateAvatar(String avatarUrl) async {
    final payload = await _request(
      'POST',
      '/api/agent/avatar',
      body: {'avatar_url': avatarUrl},
    );
    final updated = AgentProfile.fromJson(
      Map<String, dynamic>.from(payload['agent'] as Map? ?? {}),
    );
    agent = updated;
    return updated;
  }

  Future<RatingSummary> ratingSummary() async {
    final payload = await _request('GET', '/api/agent/ratings/summary');
    return RatingSummary.fromJson(Map<String, dynamic>.from(payload));
  }

  /// 读取客服自己的翻译 / AI 偏好（`GET /api/agent/preferences`）。
  Future<AgentPreferences> preferences() async {
    final payload = await _request('GET', '/api/agent/preferences');
    return AgentPreferences.fromJson(
      Map<String, dynamic>.from(payload['preferences'] as Map? ?? {}),
    );
  }

  /// 局部更新偏好：只提交 [patch] 中显式赋值的字段；非法值后端返回 400，
  /// 由 [ServiceBridgeException.message] 原样带出给界面展示。
  Future<AgentPreferences> updatePreferences(
    AgentPreferencesPatch patch,
  ) async {
    final payload = await _request(
      'PATCH',
      '/api/agent/preferences',
      body: patch.toJson(),
    );
    return AgentPreferences.fromJson(
      Map<String, dynamic>.from(payload['preferences'] as Map? ?? {}),
    );
  }

  /// 发送前译文预览：只翻译，不发送任何消息。
  /// 翻译上游失败时后端返回 502，这里按普通请求错误抛出。
  Future<TranslationPreview> translatePreview({
    required String conversationId,
    required String content,
  }) async {
    final payload = await _request(
      'POST',
      '/api/agent/translate-preview',
      body: {'conversation_id': conversationId, 'content': content},
    );
    return TranslationPreview.fromJson(payload);
  }

  /// AI 推荐发送（`POST /api/agent/suggest-reply`）：只生成 1–3 条候选回复，
  /// **不发送任何消息**，客服需自行点选/编辑后再发送。
  /// 403 表示该会话不属于当前客服；上游不可用时后端返回 502，
  /// 均按普通请求错误抛出，由界面展示后端 message。
  Future<SuggestReplyResult> suggestReply({
    required String conversationId,
  }) async {
    final payload = await _request(
      'POST',
      '/api/agent/suggest-reply',
      body: {'conversation_id': conversationId},
    );
    return SuggestReplyResult.fromJson(payload);
  }

  /// 会话侧访客画像（`GET /api/agent/conversations/{id}/profile`）。
  ///
  /// 403：会话不属于当前客服；404：未开启画像或访客无画像。
  /// 两者都以 [ServiceBridgeException.statusCode] 带出，由界面给中文文案。
  Future<VisitorProfile> conversationProfile(String conversationId) async {
    final payload = await _request(
      'GET',
      '/api/agent/conversations/${Uri.encodeComponent(conversationId)}'
          '/profile',
    );
    final raw = payload['profile'];
    if (raw is! Map) {
      throw const ServiceBridgeException('画像响应不完整');
    }
    return VisitorProfile.fromJson(Map<String, dynamic>.from(raw));
  }

  /// 提醒渠道列表（`GET /api/agent/alert-channels`）。
  /// 服务端只回传掩码，不回传明文密钥。
  Future<List<AlertChannel>> alertChannels() async {
    final payload = await _request('GET', '/api/agent/alert-channels');
    return (payload['channels'] as List? ?? [])
        .map(
          (item) => AlertChannel.fromJson(
            Map<String, dynamic>.from(item as Map),
          ),
        )
        .toList();
  }

  /// 新增提醒渠道（`POST /api/agent/alert-channels`）。
  /// 超过 3 个时后端返回 400，错误 message 由 [ServiceBridgeException] 带出。
  Future<AlertChannel> createAlertChannel({
    required String kind,
    required Map<String, dynamic> config,
    bool? enabled,
  }) async {
    final payload = await _request(
      'POST',
      '/api/agent/alert-channels',
      body: <String, dynamic>{
        'kind': kind,
        'config': config,
        'enabled': ?enabled,
      },
    );
    return _alertChannelFrom(payload);
  }

  /// 启停 / 修改渠道（`PATCH /api/agent/alert-channels/{id}`）。
  /// 只提交显式提供的字段。
  Future<AlertChannel> updateAlertChannel(
    String id, {
    bool? enabled,
    Map<String, dynamic>? config,
  }) async {
    final payload = await _request(
      'PATCH',
      '/api/agent/alert-channels/${Uri.encodeComponent(id)}',
      body: <String, dynamic>{
        'enabled': ?enabled,
        'config': ?config,
      },
    );
    return _alertChannelFrom(payload);
  }

  /// 删除渠道（`DELETE /api/agent/alert-channels/{id}`）。
  Future<void> deleteAlertChannel(String id) async {
    await _request(
      'DELETE',
      '/api/agent/alert-channels/${Uri.encodeComponent(id)}',
    );
  }

  /// 立即发一条测试消息（`POST /api/agent/alert-channels/test`）。
  /// 未配置 SMTP 时邮件渠道会失败，message 原样抛给界面展示。
  Future<AlertChannelTestResult> testAlertChannel({
    required String kind,
    required Map<String, dynamic> config,
  }) async {
    final payload = await _request(
      'POST',
      '/api/agent/alert-channels/test',
      body: <String, dynamic>{'kind': kind, 'config': config},
    );
    return AlertChannelTestResult.fromJson(payload);
  }

  AlertChannel _alertChannelFrom(Map<String, dynamic> payload) {
    final raw = payload['channel'];
    if (raw is! Map) {
      throw const ServiceBridgeException('渠道响应不完整');
    }
    return AlertChannel.fromJson(Map<String, dynamic>.from(raw));
  }

  /// 客服接管会话（`POST /api/agent/conversations/{id}/takeover`）。
  /// 后端为下一波实现，失败按普通请求错误抛出（界面展示可读文案）。
  Future<void> takeoverConversation(String conversationId) async {
    await _request(
      'POST',
      '/api/agent/conversations/${Uri.encodeComponent(conversationId)}'
          '/takeover',
      body: const {},
    );
  }

  /// 只读围观会话（`POST /api/agent/conversations/{id}/watch`）：
  /// 围观时不显示已读、不能发送消息。后端为下一波实现。
  Future<void> watchConversation(String conversationId) async {
    await _request(
      'POST',
      '/api/agent/conversations/${Uri.encodeComponent(conversationId)}/watch',
      body: const {},
    );
  }

  /// 公开接口：拉取服务端下发的 Firebase / 推送开关配置（B1）。
  /// 网络失败或响应异常时抛出 [ServiceBridgeException]，由调用方决定是否忽略。
  Future<AppConfig> appConfig() async {
    final payload = await _request(
      'GET',
      '/api/app-config',
      authenticated: false,
    );
    return AppConfig.fromJson(payload);
  }

  Future<void> registerDeviceToken(String deviceToken) async {
    await _request(
      'POST',
      '/api/agent/device-token',
      body: {
        'device_token': deviceToken,
        'platform': Platform.isIOS ? 'ios' : 'android',
      },
      authenticated: true,
    );
  }

  Future<List<Conversation>> conversations() async {
    final payload = await _request('GET', '/api/agent/conversations');
    return (payload['conversations'] as List? ?? [])
        .map(
          (item) =>
              Conversation.fromJson(Map<String, dynamic>.from(item as Map)),
        )
        .toList();
  }

  /// 历史分页：默认只取一页，向上滚动时用 [before] 取更早的消息（B7）。
  Future<MessagePage> messagePage(
    String conversationId, {
    int limit = 30,
    String? before,
  }) async {
    final query = StringBuffer('?limit=$limit');
    if (before != null && before.trim().isNotEmpty) {
      query.write('&before=${Uri.encodeQueryComponent(before.trim())}');
    }
    final payload = await _request(
      'GET',
      '/api/conversations/${Uri.encodeComponent(conversationId)}/messages$query',
    );
    final messages = (payload['messages'] as List? ?? [])
        .map(
          (item) =>
              ChatMessage.fromJson(Map<String, dynamic>.from(item as Map)),
        )
        .toList();
    return MessagePage(
      messages: messages,
      hasMore: payload['has_more'] == true,
      nextBefore: '${payload['next_before'] ?? ''}'.trim(),
    );
  }

  /// 兼容旧调用：只返回消息列表。
  Future<List<ChatMessage>> messages(String conversationId) async =>
      (await messagePage(conversationId)).messages;

  /// 撤回消息（B5）。仅对自己发送的文本/图片消息生效。
  Future<ChatMessage?> revokeMessage(
    String conversationId,
    String messageId,
  ) async {
    final payload = await _request(
      'POST',
      '/api/conversations/${Uri.encodeComponent(conversationId)}'
          '/messages/${Uri.encodeComponent(messageId)}/revoke',
      body: const {},
    );
    final raw = payload['message'];
    if (raw is! Map) return null;
    return ChatMessage.fromJson(Map<String, dynamic>.from(raw));
  }

  /// 更新访客备注（B6）。
  Future<Conversation?> updateRemark(
    String conversationId,
    String remark,
  ) async {
    final payload = await _request(
      'PATCH',
      '/api/conversations/${Uri.encodeComponent(conversationId)}/remark',
      body: {'remark': remark},
    );
    final raw = payload['conversation'];
    if (raw is! Map) return null;
    return Conversation.fromJson(Map<String, dynamic>.from(raw));
  }

  Future<void> markRead(String conversationId) async {
    await _request(
      'POST',
      '/api/conversations/${Uri.encodeComponent(conversationId)}/read',
      body: const {},
    );
  }

  Future<void> closeConversation(String conversationId) async {
    await _request(
      'POST',
      '/api/conversations/${Uri.encodeComponent(conversationId)}/close',
      body: const {},
    );
  }

  Future<void> deleteConversation(String conversationId) async {
    await _request(
      'DELETE',
      '/api/conversations/${Uri.encodeComponent(conversationId)}',
    );
  }

  Future<void> changePassword(
    String currentPassword,
    String newPassword,
  ) async {
    await _request(
      'POST',
      '/api/account/password',
      body: {'current_password': currentPassword, 'new_password': newPassword},
    );
  }

  Future<String> upload(File file) async {
    final uri = Uri.parse('$baseUrl/api/uploads');
    final request = http.MultipartRequest('POST', uri)
      ..headers['Authorization'] = 'Bearer $token'
      ..files.add(await http.MultipartFile.fromPath('file', file.path));
    final response = await request.send();
    final text = await response.stream.bytesToString();
    final payload = _decode(text);
    if (response.statusCode == 401) {
      _notifyUnauthorized();
      throw const ServiceBridgeException(
        '登录已失效，请重新登录',
        unauthorized: true,
      );
    }
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw ServiceBridgeException(
        _errorMessage(payload, response.statusCode),
        statusCode: response.statusCode,
      );
    }
    return '${payload['url'] ?? ''}';
  }

  void sendMessage(
    String conversationId,
    String content, {
    String messageType = 'text',
  }) {
    final value = content.trim();
    if (_channel == null) throw const ServiceBridgeException('实时通道尚未连接');
    if (value.isEmpty) throw const ServiceBridgeException('消息不能为空');
    _channel!.sink.add(
      jsonEncode({
        'event': 'message.send',
        'data': {
          'conversation_id': conversationId,
          'client_msg_id': 'app_${DateTime.now().microsecondsSinceEpoch}',
          'message_type': messageType,
          'content': value,
        },
      }),
    );
  }

  /// 客服正在输入指示：访客聊天窗口会显示"客服正在回复"。
  void sendTyping(String conversationId, {bool typing = true}) {
    if (_channel == null) return;
    try {
      _channel!.sink.add(
        jsonEncode({
          'event': 'conversation.typing',
          'data': {
            'conversation_id': conversationId,
            'typing': typing,
          },
        }),
      );
    } catch (_) {}
  }

  void connect() {
    if (token.isEmpty) return;
    _closedByUser = false;
    _sessionRevoked = false;
    _reconnectAttempts = 0;
    _connectNow();
  }

  void _connectNow() {
    if (_closedByUser || _sessionRevoked || token.isEmpty) return;
    _socketSubscription?.cancel();
    _heartbeat?.cancel();
    _reconnectTimer?.cancel();
    try {
      final httpUri = Uri.parse(baseUrl);
      final socketUri = httpUri.replace(
        scheme: httpUri.scheme == 'https' ? 'wss' : 'ws',
        path: '/ws',
        queryParameters: {'role': 'agent', 'token': token},
      );
      _channel = IOWebSocketChannel.connect(
        socketUri,
        pingInterval: const Duration(seconds: 20),
      );
      // 连接建立成功后退避计数归零（B3）。
      _channel!.ready.then((_) {
        _reconnectAttempts = 0;
      }).catchError((Object _) {});
      _socketSubscription = _channel!.stream.listen(
        (raw) {
          final decoded = _decode('$raw');
          if (decoded['event'] == 'session.revoked') {
            // 登录态被后台撤销：停止重连并关闭本地通道（B3）。
            _stopReconnect(revoked: true);
          }
          _eventController.add(Map<String, dynamic>.from(decoded));
        },
        onError: (_) => _scheduleReconnect(),
        onDone: _scheduleReconnect,
      );
      _heartbeat = Timer.periodic(const Duration(seconds: 25), (_) {
        try {
          _channel?.sink.add(
            jsonEncode({
              'event': 'ping',
              'data': {'at': DateTime.now().toUtc().toIso8601String()},
            }),
          );
        } catch (_) {
          _scheduleReconnect();
        }
      });
    } catch (_) {
      _channel = null;
      _scheduleReconnect();
    }
  }

  void _scheduleReconnect() {
    _heartbeat?.cancel();
    _channel = null;
    if (_closedByUser || _sessionRevoked || token.isEmpty) return;
    _reconnectTimer?.cancel();
    final delaySeconds = _nextReconnectDelaySeconds(_reconnectAttempts);
    _reconnectAttempts += 1;
    _reconnectTimer = Timer(Duration(seconds: delaySeconds), _connectNow);
  }

  /// 指数退避序列：1, 2, 4, 8, 16, 30, 30 …（上限 30 秒）。
  static int _nextReconnectDelaySeconds(int attempt) {
    if (attempt <= 0) return 1;
    if (attempt >= 5) return _maxReconnectDelaySeconds;
    final value = 1 << attempt;
    return value > _maxReconnectDelaySeconds
        ? _maxReconnectDelaySeconds
        : value;
  }

  void _stopReconnect({required bool revoked}) {
    if (revoked) _sessionRevoked = true;
    _heartbeat?.cancel();
    _reconnectTimer?.cancel();
    _socketSubscription?.cancel();
    _socketSubscription = null;
    _channel = null;
  }

  Future<Map<String, dynamic>> _request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    bool authenticated = true,
  }) async {
    final uri = Uri.parse('$baseUrl$path');
    final headers = <String, String>{'Accept': 'application/json'};
    if (authenticated) headers['Authorization'] = 'Bearer $token';
    if (body != null) headers['Content-Type'] = 'application/json';
    final request = http.Request(method, uri)
      ..headers.addAll(headers)
      ..body = body == null ? '' : jsonEncode(body);
    final streamed = await request.send().timeout(const Duration(seconds: 15));
    final text = await streamed.stream.bytesToString();
    final payload = _decode(text);
    if (streamed.statusCode == 401) {
      _notifyUnauthorized();
      throw const ServiceBridgeException(
        '登录已失效，请重新登录',
        unauthorized: true,
      );
    }
    if (streamed.statusCode < 200 || streamed.statusCode >= 300) {
      throw ServiceBridgeException(
        _errorMessage(payload, streamed.statusCode),
        statusCode: streamed.statusCode,
      );
    }
    return payload;
  }

  Map<String, dynamic> _decode(String text) {
    if (text.trim().isEmpty) return <String, dynamic>{};
    try {
      return Map<String, dynamic>.from(jsonDecode(text) as Map);
    } catch (_) {
      return {'message': text};
    }
  }

  String _errorMessage(Map<String, dynamic> payload, int status) {
    final error = payload['error'];
    final message = error is Map && error['message'] != null
        ? '${error['message']}'
        : payload['message'] != null
        ? '${payload['message']}'
        : error is String
        ? error
        : '';
    return switch (message.toLowerCase()) {
      'invalid credentials' => '账号或密码错误',
      'unauthorized' => '登录已失效，请重新登录',
      'forbidden' => '当前账号没有操作权限',
      'not found' => '请求的数据不存在',
      'invalid input' => '输入内容不符合要求',
      _ when message.isNotEmpty => message,
      _ => '请求失败（HTTP $status）',
    };
  }

  Future<void> close() async {
    _closedByUser = true;
    _heartbeat?.cancel();
    _reconnectTimer?.cancel();
    await _socketSubscription?.cancel();
    await _channel?.sink.close();
    _channel = null;
  }

  Future<void> dispose() async {
    await close();
    await _eventController.close();
  }
}
