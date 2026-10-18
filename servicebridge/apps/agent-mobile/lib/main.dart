import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';
import 'package:just_audio/just_audio.dart';

import 'models.dart';
import 'push_service.dart';
import 'update_service.dart';
import 'service_bridge.dart';

abstract final class AppColors {
  AppColors._();

  // Brand
  static const Color primary = Color(0xFF0878DC);
  static const Color primaryDark = Color(0xFF07345F);
  static const Color primarySoft = Color(0xFFE8F4FB);

  // Surfaces
  static const Color background = Color(0xFFF3F7FA);
  static const Color surface = Colors.white;
  static const Color border = Color(0xFFE0E8EF);
  static const Color divider = Color(0xFFE6EBF0);

  // Text
  static const Color textPrimary = Color(0xFF142033);
  static const Color textSecondary = Color(0xFF68778C);
  static const Color textMuted = Color(0xFF9AA7B4);
  static const Color textFaint = Color(0xFFB1C0CD);

  // Semantic
  static const Color success = Color(0xFF16835C);
  static const Color successSoft = Color(0xFFE5F6EF);
  static const Color successBorder = Color(0xFFBFE8D3);
  static const Color warning = Color(0xFFA46600);
  static const Color danger = Color(0xFFEF5B4C);
  static const Color neutral = Color(0xFF7A8794);

  // Chat identity (WeChat-style bubbles; intentionally scoped to chat UI)
  static const Color chatBubbleMine = Color(0xFF95EC69);
}

abstract final class AppRadii {
  AppRadii._();

  static const double card = 16;
  static const double input = 14;
  static const double bubble = 12;
  static const double bubbleTail = 4;
  static const double chip = 9;
  static const double pill = 999;
}

abstract final class AppTheme {
  AppTheme._();

  static ThemeData light() {
    const scheme = ColorScheme.light(
      primary: AppColors.primary,
      onPrimary: Colors.white,
      secondary: AppColors.primaryDark,
      onSecondary: Colors.white,
      surface: AppColors.surface,
      onSurface: AppColors.textPrimary,
      error: AppColors.danger,
      onError: Colors.white,
    );

    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      scaffoldBackgroundColor: AppColors.background,
      fontFamilyFallback: const ['PingFang SC', 'Microsoft YaHei'],
      splashFactory: InkSparkle.splashFactory,
      appBarTheme: const AppBarTheme(
        backgroundColor: AppColors.surface,
        foregroundColor: AppColors.textPrimary,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        titleTextStyle: TextStyle(
          color: AppColors.textPrimary,
          fontSize: 18,
          fontWeight: FontWeight.w700,
          fontFamilyFallback: ['PingFang SC', 'Microsoft YaHei'],
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: AppColors.surface,
        labelStyle: const TextStyle(color: AppColors.textSecondary),
        hintStyle: const TextStyle(color: AppColors.textMuted),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadii.input),
          borderSide: const BorderSide(color: AppColors.border),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadii.input),
          borderSide: const BorderSide(color: AppColors.border),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadii.input),
          borderSide: const BorderSide(color: AppColors.primary, width: 1.5),
        ),
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 16,
          vertical: 15,
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: AppColors.primary,
          foregroundColor: Colors.white,
          minimumSize: const Size(0, 48),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(AppRadii.input),
          ),
          textStyle: const TextStyle(
            fontSize: 16,
            fontWeight: FontWeight.w700,
          ),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          foregroundColor: AppColors.primary,
          textStyle: const TextStyle(fontWeight: FontWeight.w600),
        ),
      ),
      dividerTheme: const DividerThemeData(
        color: AppColors.divider,
        thickness: 1,
        space: 1,
      ),
      listTileTheme: const ListTileThemeData(
        iconColor: AppColors.textPrimary,
        textColor: AppColors.textPrimary,
      ),
      snackBarTheme: SnackBarThemeData(
        backgroundColor: AppColors.textPrimary,
        contentTextStyle: const TextStyle(color: Colors.white),
        behavior: SnackBarBehavior.floating,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadii.input),
        ),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: AppColors.surface,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadii.card),
        ),
      ),
    );
  }

  static BoxDecoration cardDecoration({Color? color}) => BoxDecoration(
    color: color ?? AppColors.surface,
    borderRadius: BorderRadius.circular(AppRadii.card),
    border: Border.all(color: AppColors.border),
  );
}

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  PushService.instance.init();
  runApp(const CustomerServiceApp());
}

class CustomerServiceApp extends StatelessWidget {
  const CustomerServiceApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: '在线客服',
      debugShowCheckedModeBanner: false,
      navigatorKey: PushService.instance.navigatorKey,
      theme: AppTheme.light(),
      home: const LoginPage(),
    );
  }
}

class LoginPage extends StatefulWidget {
  const LoginPage({super.key});

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  final _formKey = GlobalKey<FormState>();
  final _accountController = TextEditingController();
  final _passwordController = TextEditingController();
  final _serverController = TextEditingController();
  bool _busy = false;
  bool _obscurePassword = true;

  @override
  void initState() {
    super.initState();
    ServiceBridgeClient.savedServerUrl().then((url) {
      if (mounted) setState(() => _serverController.text = url);
      _checkAppUpdate(url);
      // B1：拿到服务器地址后先用服务端下发的参数初始化推送。
      PushService.instance.configureFromServer(url);
    });
  }

  Future<void> _checkAppUpdate(String serverUrl) async {
    if (serverUrl.trim().isEmpty) return;
    try {
      final baseUrl = ServiceBridgeClient.normalizeBaseUrl(serverUrl);
      final info = await AppUpdater.checkForUpdate(baseUrl);
      if (!mounted || !info.updateAvailable) return;
      await _showUpdateDialog(info, baseUrl);
    } catch (_) {
      // Update check is best-effort; login must never be blocked by it.
    }
  }

  Future<void> _showUpdateDialog(AppUpdateInfo info, String baseUrl) async {
    final notes = info.releaseNotes.trim().isEmpty
        ? '本次更新包含功能优化与问题修复。'
        : info.releaseNotes;
    await showDialog<void>(
      context: context,
      barrierDismissible: !info.forceUpdate,
      builder: (dialogContext) => PopScope(
        // B8：强制更新时 Android 返回键不能关闭弹窗。
        canPop: !info.forceUpdate,
        child: AlertDialog(
          title: Text(info.forceUpdate ? '必须更新到新版本' : '发现新版本'),
          content: Text(
            '当前版本过低，请升级到 v${info.latestVersion} 后继续使用。\n\n更新内容：\n$notes',
          ),
          actions: [
            if (!info.forceUpdate)
              TextButton(
                onPressed: () => Navigator.of(dialogContext).pop(),
                child: const Text('稍后再说'),
              ),
            FilledButton(
              onPressed: () async {
                if (info.forceUpdate && AppUpdater.isIOS) {
                  // B8：iOS 打开真实 App Store 页面，弹窗保持阻断但不再递归重弹。
                  await _openIOSStore(info);
                  return;
                }
                Navigator.of(dialogContext).pop();
                await _performUpdate(info, baseUrl);
                if (info.forceUpdate && mounted && !AppUpdater.isIOS) {
                  await _showUpdateDialog(info, baseUrl);
                }
              },
              child: const Text('立即更新'),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _openIOSStore(AppUpdateInfo info) async {
    try {
      await AppUpdater.openExternalUrl(info.iosStoreUrl);
      if (mounted) {
        _showMessage(context, '已打开 App Store，请更新到 v${info.latestVersion} 后继续使用。');
      }
    } catch (error) {
      if (mounted) _showMessage(context, _readableError(error));
    }
  }

  Future<void> _performUpdate(AppUpdateInfo info, String baseUrl) async {
    if (AppUpdater.isIOS) {
      await _openIOSStore(info);
      return;
    }
    if (!mounted) return;
    showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (_) => const AlertDialog(
        content: Row(
          children: [
            CircularProgressIndicator(),
            SizedBox(width: 18),
            Expanded(child: Text('正在下载更新包…')),
          ],
        ),
      ),
    );
    try {
      final allowed = await AppUpdater.canInstallUnknownApps();
      if (!allowed) {
        if (mounted) {
          Navigator.of(context, rootNavigator: true).pop();
          _showMessage(context, '请允许"在线客服"安装未知来源应用，然后重新点击更新。');
        }
        await AppUpdater.openInstallSettings();
        return;
      }
      // B9：下载后按服务端下发的 sha256 校验更新包。
      final path = await AppUpdater.downloadApk(
        info.downloadUrl,
        expectedChecksum: info.checksum,
      );
      if (mounted) Navigator.of(context, rootNavigator: true).pop();
      await AppUpdater.installApk(path);
    } catch (error) {
      if (mounted) {
        Navigator.of(context, rootNavigator: true).pop();
        _showMessage(context, '更新失败：${_readableError(error)}');
      }
    }
  }

  @override
  void dispose() {
    _accountController.dispose();
    _passwordController.dispose();
    _serverController.dispose();
    super.dispose();
  }

  Future<void> _login() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() => _busy = true);
    try {
      final client = ServiceBridgeClient(
        baseUrl: ServiceBridgeClient.normalizeBaseUrl(_serverController.text),
      );
      final result = await client.login(
        _accountController.text,
        _passwordController.text,
      );
      await client.saveServerUrl(_serverController.text);
      await PushService.instance.registerToBackend(client);
      if (!mounted) return;
      await Navigator.of(context).pushReplacement(
        MaterialPageRoute(
          builder: (_) => HomePage(client: client, profile: result.agent),
        ),
      );
    } catch (error) {
      if (mounted) _showMessage(context, _readableError(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Stack(
        children: [
          const Positioned.fill(
            child: DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                  colors: [Color(0xFFF5FAFD), Color(0xFFE8F2F7)],
                ),
              ),
            ),
          ),
          Positioned(
            top: -110,
            right: -90,
            child: Container(
              width: 280,
              height: 280,
              decoration: BoxDecoration(
                shape: BoxShape.circle,
                border: Border.all(color: const Color(0x2218B7D8), width: 34),
              ),
            ),
          ),
          SafeArea(
            child: Center(
              child: SingleChildScrollView(
                padding: const EdgeInsets.all(24),
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 440),
                  child: Form(
                    key: _formKey,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Center(
                          child: Image.asset(
                            'assets/astreva-logo-transparent.png',
                            width: 112,
                            height: 112,
                          ),
                        ),
                        const SizedBox(height: 12),
                        const Text(
                          '在线客服',
                          textAlign: TextAlign.center,
                          style: TextStyle(
                            fontSize: 32,
                            fontWeight: FontWeight.w800,
                            color: AppColors.primaryDark,
                          ),
                        ),
                        const SizedBox(height: 6),
                        const Text(
                          'ASTREVA 客服坐席工作台',
                          textAlign: TextAlign.center,
                          style: TextStyle(
                            color: AppColors.textSecondary,
                            letterSpacing: 1.2,
                          ),
                        ),
                        const SizedBox(height: 34),
                        TextFormField(
                          controller: _accountController,
                          textInputAction: TextInputAction.next,
                          autofillHints: const [AutofillHints.username],
                          decoration: const InputDecoration(
                            labelText: '客服账号',
                            prefixIcon: Icon(Icons.badge_outlined),
                          ),
                          validator: _required,
                        ),
                        const SizedBox(height: 14),
                        TextFormField(
                          controller: _passwordController,
                          obscureText: _obscurePassword,
                          autofillHints: const [AutofillHints.password],
                          onFieldSubmitted: (_) => _login(),
                          decoration: InputDecoration(
                            labelText: '登录密码',
                            prefixIcon: const Icon(Icons.lock_outline),
                            suffixIcon: IconButton(
                              icon: Icon(
                                _obscurePassword
                                    ? Icons.visibility_outlined
                                    : Icons.visibility_off_outlined,
                              ),
                              onPressed: () => setState(
                                () => _obscurePassword = !_obscurePassword,
                              ),
                            ),
                          ),
                          validator: _required,
                        ),
                        const SizedBox(height: 14),
                        TextFormField(
                          controller: _serverController,
                          keyboardType: TextInputType.url,
                          decoration: const InputDecoration(
                            labelText: '服务器地址',
                            hintText: 'http://电脑局域网IP:8080',
                            prefixIcon: Icon(Icons.dns_outlined),
                          ),
                          validator: _required,
                        ),
                        const SizedBox(height: 9),
                        const Text(
                          '真机请输入电脑和手机同一 Wi-Fi 下的局域网地址。',
                          style: TextStyle(
                            fontSize: 12,
                            color: AppColors.textSecondary,
                          ),
                        ),
                        const SizedBox(height: 24),
                        FilledButton.icon(
                          onPressed: _busy ? null : _login,
                          icon: _busy
                              ? const SizedBox(
                                  width: 18,
                                  height: 18,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                    color: Colors.white,
                                  ),
                                )
                              : const Icon(Icons.login),
                          label: const Padding(
                            padding: EdgeInsets.symmetric(vertical: 15),
                            child: Text(
                              '登录工作台',
                              style: TextStyle(
                                fontSize: 16,
                                fontWeight: FontWeight.w800,
                              ),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class HomePage extends StatefulWidget {
  const HomePage({super.key, required this.client, required this.profile});
  final ServiceBridgeClient client;
  final AgentProfile profile;
  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  late AgentProfile _profile;

  /// T1/T2/T3：偏好由 HomePage 统一持有，会话页与偏好页共享同一份状态，
  /// 因此在偏好页改完之后，已打开的会话页会立即刷新（例如 T3 的常驻提示）。
  late final AgentPreferencesController _preferences;
  List<Conversation> _conversations = const [];
  StreamSubscription<Map<String, dynamic>>? _events;
  Timer? _pollTimer;
  RatingSummary? _rating;
  bool _loading = true;
  int _tab = 0;
  bool _sessionExpired = false;

  @override
  void initState() {
    super.initState();
    _profile = widget.profile;
    _preferences = AgentPreferencesController(widget.client);
    // 登录后立刻拉一次偏好；失败时保留默认值（5 项均为 true / standard），
    // 偏好页里还有一次 GET 回填与失败提示。
    unawaited(_preferences.load());
    // B2：任何请求命中 401 时统一清理本地状态并跳回登录页。
    widget.client.onUnauthorized = _handleUnauthorized;
    _loadRating();
    _events = widget.client.events.listen(_handleEvent);
    widget.client.connect();
    _setStatus('online');
    _refresh();
    _pollTimer = Timer.periodic(
      const Duration(seconds: 15),
      (_) => _refresh(silent: true),
    );
    PushService.instance.openHandler = _openConversationFromPush;
    final pending = PushService.instance.consumePendingConversation();
    if (pending != null && pending.isNotEmpty) {
      _openConversationFromPush(pending);
    }
  }

  /// B2：登录态失效后停止轮询与重连，清空路由并回到登录页。
  Future<void> _handleUnauthorized() async {
    if (_sessionExpired) return;
    _sessionExpired = true;
    _pollTimer?.cancel();
    _pollTimer = null;
    PushService.instance.openHandler = null;
    await _events?.cancel();
    _events = null;
    try {
      await widget.client.close();
    } catch (_) {
      // ignore: 清理失败不阻塞回到登录页。
    }
    if (!mounted) return;
    final messenger = ScaffoldMessenger.maybeOf(context);
    Navigator.of(context).pushAndRemoveUntil(
      MaterialPageRoute(builder: (_) => const LoginPage()),
      (_) => false,
    );
    messenger?.showSnackBar(
      const SnackBar(content: Text('登录已失效，请重新登录')),
    );
  }

  Future<void> _loadRating() async {
    try {
      final rating = await widget.client.ratingSummary();
      if (mounted) setState(() => _rating = rating);
    } catch (error) {
      if (isUnauthorizedError(error)) {
        await _handleUnauthorized();
        return;
      }
      // 好评率展示失败不影响主流程，刷新时会重试。
    }
  }

  Future<void> _pickAvatar() async {
    final picker = ImagePicker();
    final image = await picker.pickImage(
      source: ImageSource.gallery,
      imageQuality: 82,
      maxWidth: 640,
      maxHeight: 640,
    );
    if (image == null) return;
    try {
      final url = await widget.client.upload(File(image.path));
      final profile = await widget.client.updateAvatar(url);
      if (mounted) {
        setState(() => _profile = profile);
        _showMessage(context, '头像已更新');
      }
    } catch (error) {
      if (isUnauthorizedError(error)) {
        await _handleUnauthorized();
        return;
      }
      if (mounted) _showMessage(context, _readableError(error));
    }
  }

  Future<void> _openConversationFromPush(String conversationId) async {
    await _refresh(silent: true);
    if (!mounted) return;
    Conversation? target;
    for (final c in _conversations) {
      if (c.id == conversationId) {
        target = c;
        break;
      }
    }
    if (target == null) {
      _showMessage(context, '会话不存在或已被移除');
      return;
    }
    await _openConversation(target);
  }

  Future<void> _openConversation(Conversation conversation) async {
    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => ChatPage(
          client: widget.client,
          conversation: conversation,
          myAvatarUrl: _profile.avatarUrl,
          preferences: _preferences,
        ),
      ),
    );
    if (mounted && !_sessionExpired) _refresh(silent: true);
  }

  /// T1：打开「我的偏好 / 翻译设置」。
  Future<void> _openPreferences() async {
    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => PreferencesPage(controller: _preferences),
      ),
    );
    // 返回后无需重启 App：_preferences 是同一份状态，会话页在监听它。
  }

  void _handleEvent(Map<String, dynamic> event) {
    final type = '${event['event'] ?? ''}';
    if (type == 'session.revoked') {
      _logout(message: '账号已退出，请重新登录');
      return;
    }
    final data = Map<String, dynamic>.from(event['data'] as Map? ?? {});
    if (type == 'agent.notification') {
      final title = '${data['title'] ?? ''}'.trim();
      final body = '${data['body'] ?? ''}'.trim();
      final conversationId = '${data['conversation_id'] ?? ''}'.trim();
      PushService.instance.showLocalNotification(
        title: title.isEmpty ? '新客服消息' : title,
        body: body.isEmpty ? '您有新的客服消息' : body,
        conversationId: conversationId.isEmpty ? null : conversationId,
      );
    }
    if (type == 'message.receive' ||
        type == 'conversation.assigned' ||
        type == 'conversation.status_changed' ||
        type == 'agent.notification') {
      _refresh(silent: true);
    }
  }

  Future<void> _refresh({bool silent = false}) async {
    if (_sessionExpired) return;
    if (!silent && mounted) setState(() => _loading = true);
    try {
      final items = await widget.client.conversations();
      items.sort(
        (a, b) =>
            (b.updatedAt ?? DateTime(0)).compareTo(a.updatedAt ?? DateTime(0)),
      );
      if (mounted) setState(() => _conversations = items);
    } catch (error) {
      if (isUnauthorizedError(error)) {
        await _handleUnauthorized();
        return;
      }
      if (!silent && mounted) _showMessage(context, _readableError(error));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _setStatus(String status) async {
    try {
      final profile = await widget.client.setStatus(status);
      if (mounted) setState(() => _profile = profile);
    } catch (error) {
      if (isUnauthorizedError(error)) {
        await _handleUnauthorized();
        return;
      }
      if (mounted) _showMessage(context, _readableError(error));
    }
  }

  Future<void> _deleteConversation(Conversation conversation) async {
    if (conversation.status != 'closed') {
      return;
    }
    if (!await _confirm(
      context,
      '删除会话',
      '确定删除与 ${conversation.visitorLabel} 的已结束会话？删除后不可恢复。',
    )) {
      return;
    }
    try {
      await widget.client.deleteConversation(conversation.id);
      await _refresh(silent: true);
      if (mounted) _showMessage(context, '会话已删除');
    } catch (error) {
      if (isUnauthorizedError(error)) {
        await _handleUnauthorized();
        return;
      }
      if (mounted) _showMessage(context, _readableError(error));
    }
  }

  Future<void> _logout({String? message}) async {
    _pollTimer?.cancel();
    PushService.instance.openHandler = null;
    await PushService.instance.unregisterFromBackend();
    await widget.client.close();
    if (!mounted) return;
    Navigator.of(context).pushAndRemoveUntil(
      MaterialPageRoute(builder: (_) => const LoginPage()),
      (_) => false,
    );
    if (message != null) _showMessage(context, message);
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    _events?.cancel();
    _preferences.dispose();
    widget.client.onUnauthorized = null;
    if (!_sessionExpired) {
      widget.client.setStatus('offline').catchError((_) => _profile);
    }
    widget.client.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final unread = _conversations.fold<int>(0, (sum, c) => sum + c.unread);
    final now = DateTime.now();
    final todayServed = _conversations.where((c) {
      final t = c.updatedAt;
      return t != null &&
          t.year == now.year &&
          t.month == now.month &&
          t.day == now.day;
    }).length;

    return Scaffold(
      body: IndexedStack(
        index: _tab,
        children: [
          ChatListView(
            conversations: _conversations,
            loading: _loading,
            onRefresh: _refresh,
            onOpen: _openConversation,
            onDelete: _deleteConversation,
          ),
          MePage(
            profile: _profile,
            unread: unread,
            todayServed: todayServed,
            avatarUrl: _profile.avatarUrl,
            baseUrl: widget.client.baseUrl,
            rating: _rating,
            onPickAvatar: _pickAvatar,
            onStatusChanged: _setStatus,
            onChangePassword: () => showDialog<void>(
              context: context,
              builder: (_) => ChangePasswordDialog(client: widget.client),
            ),
            onOpenPreferences: _openPreferences,
            onLogout: _logout,
          ),
        ],
      ),
      bottomNavigationBar: _CustomBottomNav(
        currentIndex: _tab,
        onChanged: (value) {
          setState(() => _tab = value);
          if (value == 1) _loadRating();
        },
      ),
    );
  }
}

class ChatListView extends StatelessWidget {
  const ChatListView({
    super.key,
    required this.conversations,
    required this.loading,
    required this.onRefresh,
    required this.onOpen,
    required this.onDelete,
  });
  final List<Conversation> conversations;
  final bool loading;
  final Future<void> Function() onRefresh;
  final ValueChanged<Conversation> onOpen;
  final ValueChanged<Conversation> onDelete;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text(
          '在线客服',
          style: TextStyle(fontWeight: FontWeight.w700),
        ),
      ),
      body: RefreshIndicator(
        onRefresh: onRefresh,
        child: loading && conversations.isEmpty
            ? const Center(child: CircularProgressIndicator())
            : conversations.isEmpty
                ? ListView(
                    physics: const AlwaysScrollableScrollPhysics(),
                    children: const [
                      SizedBox(height: 180),
                      _EmptyInbox(),
                    ],
                  )
                : ListView.separated(
                    physics: const AlwaysScrollableScrollPhysics(),
                    padding: const EdgeInsets.only(top: 4, bottom: 24),
                    itemCount: conversations.length,
                    separatorBuilder: (_, _) =>
                        const Divider(height: 1, indent: 76),
                    itemBuilder: (context, index) {
                      final conversation = conversations[index];
                      return _ConversationRow(
                        conversation: conversation,
                        onTap: () => onOpen(conversation),
                        onLongPress: conversation.status == 'closed'
                            ? () => onDelete(conversation)
                            : null,
                      );
                    },
                  ),
      ),
    );
  }
}

class _ConversationRow extends StatelessWidget {
  const _ConversationRow({
    required this.conversation,
    required this.onTap,
    this.onLongPress,
  });
  final Conversation conversation;
  final VoidCallback onTap;
  final VoidCallback? onLongPress;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      onLongPress: onLongPress,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        child: Row(
          children: [
            const _VisitorAvatar(),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          conversation.visitorLabel,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 16,
                            fontWeight: FontWeight.w700,
                            color: AppColors.textPrimary,
                          ),
                        ),
                      ),
                      _StatusBadge(status: conversation.status),
                      const SizedBox(width: 6),
                      Text(
                        _formatTime(conversation.updatedAt),
                        style: const TextStyle(
                          fontSize: 11,
                          color: AppColors.textMuted,
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 5),
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          conversation.lastMessage.isEmpty
                              ? '暂无消息'
                              : conversation.lastMessage,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 13,
                            color: AppColors.textSecondary,
                          ),
                        ),
                      ),
                      if (conversation.unread > 0)
                        Container(
                          margin: const EdgeInsets.only(left: 8),
                          constraints: const BoxConstraints(minWidth: 20),
                          padding: const EdgeInsets.symmetric(
                            horizontal: 6,
                            vertical: 2,
                          ),
                          decoration: const BoxDecoration(
                            color: AppColors.danger,
                            borderRadius: BorderRadius.all(Radius.circular(AppRadii.chip)),
                          ),
                          child: Text(
                            '${conversation.unread}',
                            textAlign: TextAlign.center,
                            style: const TextStyle(
                              color: Colors.white,
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _VisitorAvatar extends StatelessWidget {
  const _VisitorAvatar();
  @override
  Widget build(BuildContext context) => const CircleAvatar(
    radius: 23,
    backgroundColor: AppColors.primarySoft,
    foregroundColor: AppColors.primary,
    child: Icon(Icons.person_outline),
  );
}

class _AgentAvatar extends StatelessWidget {
  const _AgentAvatar({
    required this.radius,
    required this.name,
    this.imageUrl,
    this.baseUrl,
  });
  final double radius;
  final String? imageUrl;
  final String? baseUrl;
  final String name;

  @override
  Widget build(BuildContext context) {
    final initials = Text(
      name.isEmpty ? '客' : name.substring(0, 1),
      style: TextStyle(
        color: Colors.white,
        fontSize: radius * 0.78,
        fontWeight: FontWeight.w700,
      ),
    );
    final url = imageUrl ?? '';
    if (url.isNotEmpty) {
      final resolved = url.startsWith('/') && baseUrl != null
          ? '$baseUrl$url'
          : url;
      return CircleAvatar(
        radius: radius,
        backgroundColor: AppColors.primary,
        child: ClipOval(
          child: Image.network(
            resolved,
            width: radius * 2,
            height: radius * 2,
            fit: BoxFit.cover,
            errorBuilder: (_, _, _) => Center(child: initials),
          ),
        ),
      );
    }
    return CircleAvatar(
      radius: radius,
      backgroundColor: AppColors.primary,
      child: initials,
    );
  }
}

class _StatusBadge extends StatelessWidget {
  const _StatusBadge({required this.status});
  final String status;
  @override
  Widget build(BuildContext context) {
    final waiting = status == 'waiting' || status == 'human_requested';
    final closed = status == 'closed';
    final color = closed
        ? AppColors.neutral
        : waiting
        ? AppColors.warning
        : AppColors.success;
    final label = closed
        ? '已结束'
        : waiting
        ? '待接入'
        : status == 'ai_serving'
        ? 'AI接待'
        : '服务中';
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
      decoration: BoxDecoration(
        color: color.withValues(alpha: .12),
        borderRadius: BorderRadius.circular(AppRadii.chip),
      ),
      child: Text(
        label,
        style: TextStyle(
          color: color,
          fontSize: 10,
          fontWeight: FontWeight.w700,
        ),
      ),
    );
  }
}

class _EmptyInbox extends StatelessWidget {
  const _EmptyInbox();
  @override
  Widget build(BuildContext context) => const Center(
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(Icons.forum_outlined, size: 66, color: AppColors.textFaint),
        SizedBox(height: 15),
        Text('暂时没有分配给你的会话', style: TextStyle(fontWeight: FontWeight.w700)),
        SizedBox(height: 6),
        Text('保持在线，新咨询会自动出现在这里', style: TextStyle(color: AppColors.textMuted)),
      ],
    ),
  );
}

class MePage extends StatelessWidget {
  const MePage({
    super.key,
    required this.profile,
    required this.unread,
    required this.todayServed,
    required this.avatarUrl,
    required this.baseUrl,
    required this.rating,
    required this.onPickAvatar,
    required this.onStatusChanged,
    required this.onChangePassword,
    required this.onOpenPreferences,
    required this.onLogout,
  });
  final AgentProfile profile;
  final int unread;
  final int todayServed;
  final String avatarUrl;
  final String baseUrl;
  final RatingSummary? rating;
  final VoidCallback onPickAvatar;
  final ValueChanged<String> onStatusChanged;
  final VoidCallback onChangePassword;
  final VoidCallback onOpenPreferences;
  final VoidCallback onLogout;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('我的', style: TextStyle(fontWeight: FontWeight.w700)),
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 18, 16, 28),
        children: [
          Row(
            children: [
              Stack(
                children: [
                  _AgentAvatar(
                    radius: 36,
                    imageUrl: avatarUrl,
                    baseUrl: baseUrl,
                    name: profile.name,
                  ),
                  Positioned(
                    right: 0,
                    bottom: 0,
                    child: GestureDetector(
                      onTap: onPickAvatar,
                      child: Container(
                        padding: const EdgeInsets.all(5),
                        decoration: const BoxDecoration(
                          color: AppColors.primary,
                          shape: BoxShape.circle,
                        ),
                        child: const Icon(
                          Icons.camera_alt,
                          size: 14,
                          color: Colors.white,
                        ),
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(width: 16),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      profile.name,
                      style: const TextStyle(
                        fontSize: 21,
                        fontWeight: FontWeight.w800,
                        color: AppColors.textPrimary,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      '${profile.group} · ${profile.account}',
                      style: const TextStyle(
                        fontSize: 13,
                        color: AppColors.textSecondary,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 24),
          Row(
            children: [
              _MeMetric(label: '未读消息', value: '$unread'),
              const SizedBox(width: 12),
              _MeMetric(label: '今日接待', value: '$todayServed'),
              const SizedBox(width: 12),
              _MeMetric(
                label: '好评率',
                value: rating == null ? '--' : '${rating!.percent}%',
              ),
            ],
          ),
          const SizedBox(height: 22),
          Container(
            decoration: AppTheme.cardDecoration(),
            child: Column(
              children: [
                _MeMenuTile(
                  icon: Icons.account_circle_outlined,
                  title: '头像修改',
                  onTap: onPickAvatar,
                ),
                const Divider(height: 1, indent: 52),
                _MeMenuTile(
                  icon: Icons.toggle_on_outlined,
                  title: '在线状态',
                  trailing: DropdownButton<String>(
                    value: profile.status,
                    underline: const SizedBox.shrink(),
                    items: const [
                      DropdownMenuItem(value: 'online', child: Text('在线')),
                      DropdownMenuItem(value: 'busy', child: Text('忙碌')),
                      DropdownMenuItem(value: 'offline', child: Text('离线')),
                    ],
                    onChanged: (value) {
                      if (value != null) onStatusChanged(value);
                    },
                  ),
                ),
                const Divider(height: 1, indent: 52),
                _MeMenuTile(
                  icon: Icons.lock_outline,
                  title: '修改密码',
                  onTap: onChangePassword,
                ),
                const Divider(height: 1, indent: 52),
                // T1：翻译 / AI 偏好的入口，和"修改密码"同属账号级设置。
                _MeMenuTile(
                  icon: Icons.translate_outlined,
                  title: '我的偏好',
                  onTap: onOpenPreferences,
                ),
              ],
            ),
          ),
          const SizedBox(height: 22),
          Container(
            decoration: AppTheme.cardDecoration(),
            child: _MeMenuTile(
              icon: Icons.logout,
              title: '退出登录',
              onTap: onLogout,
              danger: true,
            ),
          ),
        ],
      ),
    );
  }
}

class _MeMetric extends StatelessWidget {
  const _MeMetric({required this.label, required this.value});
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) => Expanded(
    child: Container(
      padding: const EdgeInsets.symmetric(vertical: 16),
      decoration: AppTheme.cardDecoration(),
      child: Column(
        children: [
          Text(
            value,
            style: const TextStyle(
              fontSize: 22,
              fontWeight: FontWeight.w800,
              color: AppColors.primary,
            ),
          ),
          const SizedBox(height: 2),
          Text(
            label,
            style: const TextStyle(fontSize: 12, color: AppColors.textSecondary),
          ),
        ],
      ),
    ),
  );
}

class _CustomBottomNav extends StatelessWidget {
  const _CustomBottomNav({
    required this.currentIndex,
    required this.onChanged,
  });
  final int currentIndex;
  final ValueChanged<int> onChanged;

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: AppColors.surface,
        border: const Border(top: BorderSide(color: AppColors.divider)),
        boxShadow: const [
          BoxShadow(
            color: Color(0x12000000),
            blurRadius: 12,
            offset: Offset(0, -2),
          ),
        ],
      ),
      child: SafeArea(
        top: false,
        child: Row(
          children: [
            _BottomNavItem(
              icon: Icons.chat_bubble_rounded,
              label: '聊天',
              active: currentIndex == 0,
              onTap: () => onChanged(0),
            ),
            _BottomNavItem(
              icon: Icons.person_rounded,
              label: '我的',
              active: currentIndex == 1,
              onTap: () => onChanged(1),
            ),
          ],
        ),
      ),
    );
  }
}

class _BottomNavItem extends StatelessWidget {
  const _BottomNavItem({
    required this.icon,
    required this.label,
    required this.active,
    required this.onTap,
  });
  final IconData icon;
  final String label;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final color = active ? AppColors.primary : AppColors.textMuted;
    return Expanded(
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              AnimatedContainer(
                duration: const Duration(milliseconds: 180),
                width: active ? 46 : 30,
                height: 28,
                decoration: BoxDecoration(
                  color: active
                      ? AppColors.primary.withValues(alpha: .12)
                      : Colors.transparent,
                  borderRadius: BorderRadius.circular(AppRadii.input),
                ),
                child: Icon(icon, size: 22, color: color),
              ),
              const SizedBox(height: 3),
              Text(
                label,
                style: TextStyle(
                  fontSize: 11,
                  fontWeight: active ? FontWeight.w700 : FontWeight.w500,
                  color: color,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _MeMenuTile extends StatelessWidget {
  const _MeMenuTile({
    required this.icon,
    required this.title,
    this.trailing,
    this.onTap,
    this.danger = false,
  });
  final IconData icon;
  final String title;
  final Widget? trailing;
  final VoidCallback? onTap;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final color = danger ? AppColors.danger : AppColors.textPrimary;
    return ListTile(
      onTap: onTap,
      leading: Icon(icon, color: color),
      title: Text(
        title,
        style: TextStyle(color: color, fontWeight: FontWeight.w600),
      ),
      trailing: trailing ??
          const Icon(Icons.chevron_right, color: AppColors.textMuted),
    );
  }
}
class ChatPage extends StatefulWidget {
  const ChatPage({
    super.key,
    required this.client,
    required this.conversation,
    required this.preferences,
    this.myAvatarUrl,
  });
  final ServiceBridgeClient client;
  final Conversation conversation;
  final AgentPreferencesController preferences;
  final String? myAvatarUrl;
  @override
  State<ChatPage> createState() => _ChatPageState();
}

class _ChatPageState extends State<ChatPage> {
  /// B7：单页消息数量，向上滚动时用 before 继续加载。
  static const int _pageSize = 30;

  final _messageController = TextEditingController();
  final _scrollController = ScrollController();
  final _picker = ImagePicker();
  StreamSubscription<Map<String, dynamic>>? _events;
  final List<ChatMessage> _messages = <ChatMessage>[];
  bool _loading = true;
  bool _sendingImage = false;
  bool _loadingOlder = false;
  bool _preparingSend = false;
  bool _hasMore = false;
  String _nextBefore = '';
  String _status = 'waiting';
  String _visitorLabel = '';
  DateTime _lastTypingSent = DateTime.fromMillisecondsSinceEpoch(0);

  bool get _closed => _status == 'closed';

  AgentPreferences get _preferences => widget.preferences.preferences;

  @override
  void initState() {
    super.initState();
    _status = widget.conversation.status;
    _visitorLabel = widget.conversation.visitorLabel;
    _events = widget.client.events.listen(_handleEvent);
    // T3：偏好在偏好页被改动后立刻反映到本页的输入区提示。
    widget.preferences.addListener(_onPreferencesChanged);
    unawaited(widget.preferences.load());
    _messageController.addListener(_onTypingChanged);
    _scrollController.addListener(_onScroll);
    _loadMessages();
    widget.client.markRead(widget.conversation.id).catchError((_) {});
  }

  void _onTypingChanged() {
    // 输入中每 2.5 秒最多上报一次，访客端据此显示"客服正在回复"。
    if (_messageController.text.trim().isEmpty) return;
    final now = DateTime.now();
    if (now.difference(_lastTypingSent).inMilliseconds < 2500) return;
    _lastTypingSent = now;
    widget.client.sendTyping(widget.conversation.id);
  }

  void _onScroll() {
    if (!_scrollController.hasClients) return;
    if (_scrollController.position.pixels <= 60) {
      _loadOlder();
    }
  }

  void _handleEvent(Map<String, dynamic> event) {
    final type = '${event['event'] ?? ''}';
    final data = Map<String, dynamic>.from(event['data'] as Map? ?? {});
    if ((type == 'message.receive' ||
            type == 'message.ack' ||
            type == 'message.revoked') &&
        data['conversation_id'] == widget.conversation.id) {
      _loadMessages(silent: true);
      widget.client.markRead(widget.conversation.id).catchError((_) {});
    }
    if (type == 'conversation.status_changed' &&
        data['id'] == widget.conversation.id) {
      // B10：会话一结束就立刻禁用输入，不必等重新进入页面。
      final status = '${data['status'] ?? ''}'.trim();
      if (status.isEmpty || status == _status) return;
      setState(() => _status = status);
      if (status == 'closed' && mounted) {
        _showMessage(context, '该会话已结束');
      }
    }
  }

  Future<void> _loadMessages({bool silent = false}) async {
    if (!silent && mounted) setState(() => _loading = true);
    try {
      final page = await widget.client.messagePage(
        widget.conversation.id,
        limit: _pageSize,
      );
      if (!mounted) return;
      final jumpToBottom = !silent || _messages.isEmpty;
      setState(() {
        if (silent) {
          _mergeNewest(page.messages);
        } else {
          _messages
            ..clear()
            ..addAll(page.messages);
        }
        _hasMore = page.hasMore;
        _nextBefore = page.nextBefore;
      });
      if (jumpToBottom) {
        WidgetsBinding.instance.addPostFrameCallback((_) => _scrollToBottom());
      }
    } catch (error) {
      if (isUnauthorizedError(error)) return;
      if (mounted) _showMessage(context, _readableError(error));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  /// 静默刷新时按 server_msg_id 合并，保留已加载的更早历史。
  void _mergeNewest(List<ChatMessage> incoming) {
    for (final message in incoming) {
      if (message.id.isEmpty) {
        _messages.add(message);
        continue;
      }
      final index = _messages.indexWhere((item) => item.id == message.id);
      if (index >= 0) {
        _messages[index] = message;
      } else {
        _messages.add(message);
      }
    }
  }

  /// B7：向上滚动加载更早消息，并保持当前阅读位置。
  Future<void> _loadOlder() async {
    if (_loadingOlder || !_hasMore || _nextBefore.isEmpty) return;
    if (_status == 'closed' && _messages.isEmpty) return;
    _loadingOlder = true;
    final before = _nextBefore;
    final distanceToBottom = _scrollController.hasClients
        ? _scrollController.position.maxScrollExtent -
              _scrollController.position.pixels
        : 0.0;
    try {
      final page = await widget.client.messagePage(
        widget.conversation.id,
        limit: _pageSize,
        before: before,
      );
      if (!mounted) return;
      setState(() {
        final known = _messages
            .map((item) => item.id)
            .where((id) => id.isNotEmpty)
            .toSet();
        final older = page.messages
            .where((item) => item.id.isEmpty || !known.contains(item.id))
            .toList();
        _messages.insertAll(0, older);
        _hasMore = page.hasMore;
        _nextBefore = page.nextBefore;
      });
      if (_scrollController.hasClients) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (!_scrollController.hasClients) return;
          final position = _scrollController.position;
          final target = (position.maxScrollExtent - distanceToBottom).clamp(
            position.minScrollExtent,
            position.maxScrollExtent,
          );
          _scrollController.jumpTo(target);
        });
      }
    } catch (error) {
      if (isUnauthorizedError(error)) return;
      if (mounted) _showMessage(context, _readableError(error));
    } finally {
      _loadingOlder = false;
    }
  }

  void _onPreferencesChanged() {
    if (mounted) setState(() {});
  }

  /// 真正把文本交给实时通道（发送原文 / 发送编辑后的译文都走这里）。
  void _sendContent(String content) {
    try {
      widget.client.sendMessage(widget.conversation.id, content);
      widget.client.sendTyping(widget.conversation.id, typing: false);
      _messageController.clear();
    } catch (error) {
      _showMessage(context, _readableError(error));
    }
  }

  /// T2：开启「发送前确认译文」时先取预览再发送。
  /// 关闭时不调 preview（避免多余上游调用），直接发送原文由服务端翻译。
  Future<void> _sendText() async {
    if (_closed || _preparingSend) return;
    final original = _messageController.text.trim();
    if (original.isEmpty) return;
    if (!_preferences.reviewTranslationBeforeSend) {
      _sendContent(original);
      return;
    }
    setState(() => _preparingSend = true);
    try {
      final preview = await widget.client.translatePreview(
        conversationId: widget.conversation.id,
        content: original,
      );
      if (!mounted) return;
      // 源语言与目标语言一致：无需确认，直接发送。
      if (preview.sameLanguage) {
        _sendContent(original);
        return;
      }
      final text = await _showTranslationPreview(preview, original: original);
      if (text == null || !mounted) return;
      _sendContent(text);
    } catch (error) {
      if (isUnauthorizedError(error)) return;
      if (!mounted) return;
      // 502 等失败不卡住客服：提示后仍可继续发送原文。
      final sendOriginal = await _confirmSendOriginal(_readableError(error));
      if (sendOriginal && mounted) _sendContent(original);
    } finally {
      if (mounted) setState(() => _preparingSend = false);
    }
  }

  /// T2：译文本预览弹窗；返回 null 表示取消，返回文本表示要发送的内容。
  Future<String?> _showTranslationPreview(
    TranslationPreview preview, {
    required String original,
  }) => showModalBottomSheet<String>(
    context: context,
    isScrollControlled: true,
    backgroundColor: AppColors.surface,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(AppRadii.card)),
    ),
    builder: (_) =>
        _TranslationPreviewSheet(preview: preview, original: original),
  );

  Future<bool> _confirmSendOriginal(String detail) async =>
      await showDialog<bool>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: const Text('译文生成失败'),
          content: Text('译文生成失败，可直接发送原文。\n\n$detail'),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.of(dialogContext).pop(true),
              child: const Text('发送原文'),
            ),
          ],
        ),
      ) ??
      false;

  Future<void> _sendImage() async {
    final image = await _picker.pickImage(
      source: ImageSource.gallery,
      imageQuality: 82,
      maxWidth: 1800,
    );
    if (image == null) return;
    setState(() => _sendingImage = true);
    try {
      final url = await widget.client.upload(File(image.path));
      widget.client.sendMessage(
        widget.conversation.id,
        url,
        messageType: 'image',
      );
    } catch (error) {
      if (isUnauthorizedError(error)) return;
      if (mounted) _showMessage(context, _readableError(error));
    } finally {
      if (mounted) setState(() => _sendingImage = false);
    }
  }

  /// B5：长按自己发送的文本/图片消息 → 撤回。
  Future<void> _showMessageActions(ChatMessage message) async {
    if (_closed) return;
    final canRevoke =
        message.senderType == 'agent' &&
        !message.revoked &&
        (message.messageType == 'text' || message.messageType == 'image');
    if (!canRevoke) return;
    await showModalBottomSheet<void>(
      context: context,
      builder: (sheetContext) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.undo),
              title: const Text('撤回'),
              onTap: () {
                Navigator.of(sheetContext).pop();
                _revokeMessage(message);
              },
            ),
            ListTile(
              leading: const Icon(Icons.close),
              title: const Text('取消'),
              onTap: () => Navigator.of(sheetContext).pop(),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _revokeMessage(ChatMessage message) async {
    if (message.id.isEmpty) {
      _showMessage(context, '消息尚未发送成功，暂时不能撤回');
      return;
    }
    if (!await _confirm(context, '撤回消息', '撤回后访客会看到"该消息已撤回"，确定撤回吗？')) {
      return;
    }
    try {
      await widget.client.revokeMessage(widget.conversation.id, message.id);
      if (!mounted) return;
      setState(() {
        final index = _messages.indexWhere((item) => item.id == message.id);
        if (index >= 0) _messages[index] = _messages[index].asRevoked();
      });
    } catch (error) {
      if (isUnauthorizedError(error)) return;
      if (mounted) _showMessage(context, _readableError(error));
    }
  }

  /// B6：编辑访客备注，保存后刷新列表显示。
  Future<void> _editRemark() async {
    final controller = TextEditingController(text: _visitorLabel);
    final remark = await showDialog<String>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('访客备注'),
        content: TextField(
          controller: controller,
          autofocus: true,
          maxLength: 60,
          decoration: const InputDecoration(
            hintText: '例如：VIP 客户 / 待回访',
            counterText: '',
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () =>
                Navigator.of(dialogContext).pop(controller.text.trim()),
            child: const Text('保存'),
          ),
        ],
      ),
    );
    controller.dispose();
    if (remark == null) return;
    try {
      final updated = await widget.client.updateRemark(
        widget.conversation.id,
        remark,
      );
      if (!mounted) return;
      setState(() {
        _visitorLabel = updated?.visitorLabel.trim().isNotEmpty == true
            ? updated!.visitorLabel
            : (remark.isEmpty ? _visitorLabel : remark);
      });
      _showMessage(context, '备注已保存');
    } catch (error) {
      if (isUnauthorizedError(error)) return;
      if (mounted) _showMessage(context, _readableError(error));
    }
  }

  void _scrollToBottom() {
    if (_scrollController.hasClients) {
      _scrollController.animateTo(
        _scrollController.position.maxScrollExtent,
        duration: const Duration(milliseconds: 220),
        curve: Curves.easeOut,
      );
    }
  }

  Future<void> _closeConversation() async {
    if (!await _confirm(context, '结束会话', '结束后访客将不能继续发送消息，确定继续吗？')) return;
    try {
      await widget.client.closeConversation(widget.conversation.id);
      if (!mounted) return;
      setState(() => _status = 'closed');
      Navigator.pop(context);
    } catch (error) {
      if (isUnauthorizedError(error)) return;
      if (mounted) _showMessage(context, _readableError(error));
    }
  }

  @override
  void dispose() {
    _events?.cancel();
    widget.preferences.removeListener(_onPreferencesChanged);
    _messageController.removeListener(_onTypingChanged);
    _scrollController.removeListener(_onScroll);
    _messageController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final closed = _closed;
    return Scaffold(
      appBar: AppBar(
        centerTitle: true,
        title: Column(
          children: [
            Text(
              _visitorLabel,
              style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
            ),
            Text(
              _statusLabel(_status),
              style: const TextStyle(fontSize: 11, color: AppColors.textMuted),
            ),
          ],
        ),
        actions: [
          IconButton(
            onPressed: _editRemark,
            tooltip: '访客备注',
            icon: const Icon(Icons.edit_note_outlined),
          ),
          if (!closed)
            IconButton(
              onPressed: _closeConversation,
              tooltip: '结束会话',
              icon: const Icon(Icons.check_circle_outline),
            ),
        ],
      ),
      body: Column(
        children: [
          if (closed)
            Container(
              width: double.infinity,
              color: AppColors.border,
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
              child: const Text(
                '会话已结束，仅可查看历史消息',
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 12, color: AppColors.textSecondary),
              ),
            ),
          Expanded(
            child: _loading && _messages.isEmpty
                ? const Center(child: CircularProgressIndicator())
                : ListView.builder(
                    controller: _scrollController,
                    padding: const EdgeInsets.all(16),
                    itemCount: _messages.length + 1,
                    itemBuilder: (context, index) {
                      if (index == 0) {
                        return _HistoryHeader(
                          loading: _loadingOlder,
                          hasMore: _hasMore,
                          onLoad: _loadOlder,
                        );
                      }
                      final message = _messages[index - 1];
                      return _MessageBubble(
                        message: message,
                        baseUrl: widget.client.baseUrl,
                        myAvatarUrl: widget.myAvatarUrl,
                        onLongPress: () => _showMessageActions(message),
                      );
                    },
                  ),
          ),
          SafeArea(
            top: false,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                // T3：关闭出向自动翻译时常驻的浅色提示。
                if (!_preferences.autoTranslateOutgoing)
                  const _NoAutoTranslateHint(),
                Container(
                  padding: const EdgeInsets.fromLTRB(10, 10, 10, 12),
                  decoration: const BoxDecoration(
                    color: AppColors.surface,
                    border: Border(top: BorderSide(color: AppColors.border)),
                  ),
                  child: Row(
                    children: [
                      IconButton(
                        onPressed: closed || _sendingImage ? null : _sendImage,
                        icon: _sendingImage
                            ? const SizedBox(
                                width: 20,
                                height: 20,
                                child: CircularProgressIndicator(strokeWidth: 2),
                              )
                            : const Icon(Icons.add_photo_alternate_outlined),
                      ),
                      Expanded(
                        child: TextField(
                          controller: _messageController,
                          minLines: 1,
                          maxLines: 5,
                          enabled: !closed,
                          onSubmitted: (_) => _sendText(),
                          decoration: InputDecoration(
                            hintText: closed ? '会话已结束' : '输入回复...',
                            contentPadding: const EdgeInsets.symmetric(
                              horizontal: 14,
                              vertical: 11,
                            ),
                          ),
                        ),
                      ),
                      const SizedBox(width: 8),
                      IconButton.filled(
                        onPressed: closed || _preparingSend ? null : _sendText,
                        icon: _preparingSend
                            ? const SizedBox(
                                width: 18,
                                height: 18,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                  color: Colors.white,
                                ),
                              )
                            : const Icon(Icons.send_rounded),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// B7：列表顶部的历史加载入口。
class _HistoryHeader extends StatelessWidget {
  const _HistoryHeader({
    required this.loading,
    required this.hasMore,
    required this.onLoad,
  });

  final bool loading;
  final bool hasMore;
  final VoidCallback onLoad;

  @override
  Widget build(BuildContext context) {
    if (loading) {
      return const Padding(
        padding: EdgeInsets.symmetric(vertical: 12),
        child: Center(
          child: SizedBox(
            width: 18,
            height: 18,
            child: CircularProgressIndicator(strokeWidth: 2),
          ),
        ),
      );
    }
    if (hasMore) {
      return Center(
        child: TextButton(
          onPressed: onLoad,
          child: const Text('加载更早的消息'),
        ),
      );
    }
    return const Padding(
      padding: EdgeInsets.symmetric(vertical: 10),
      child: Center(
        child: Text(
          '没有更早的消息了',
          style: TextStyle(fontSize: 11, color: AppColors.textMuted),
        ),
      ),
    );
  }
}

class _MessageBubble extends StatefulWidget {
  const _MessageBubble({
    required this.message,
    required this.baseUrl,
    this.myAvatarUrl,
    this.onLongPress,
  });
  final ChatMessage message;
  final String baseUrl;
  final String? myAvatarUrl;
  final VoidCallback? onLongPress;

  @override
  State<_MessageBubble> createState() => _MessageBubbleState();
}

class _MessageBubbleState extends State<_MessageBubble> {
  bool _showOriginal = false;
  bool _playingAudio = false;

  @override
  void dispose() {
    if (_playingAudio) {
      _VoicePlaybackController.instance.stop();
    }
    super.dispose();
  }

  /// B4：播放远端语音消息（just_audio，单实例播放，避免多条语音叠加）。
  Future<void> _toggleAudio() async {
    final controller = _VoicePlaybackController.instance;
    if (_playingAudio) {
      await controller.stop();
      if (mounted) setState(() => _playingAudio = false);
      return;
    }
    final raw = widget.message.content.trim();
    if (raw.isEmpty) return;
    final url = raw.startsWith('/') ? '${widget.baseUrl}$raw' : raw;
    setState(() => _playingAudio = true);
    try {
      await controller.play(url);
    } catch (_) {
      if (mounted) _showMessage(context, '语音播放失败');
    } finally {
      if (mounted) setState(() => _playingAudio = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final message = widget.message;
    final myAvatarUrl = widget.myAvatarUrl;
    final isMine = message.senderType == 'agent';
    final isAI = message.senderType == 'ai';
    final isSystem = message.senderType == 'system' ||
        message.messageType == 'system';

    if (isSystem) {
      return Center(
        child: Container(
          margin: const EdgeInsets.symmetric(vertical: 8),
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 5),
          decoration: BoxDecoration(
            color: AppColors.border,
            borderRadius: BorderRadius.circular(AppRadii.bubble),
          ),
          child: Text(
            message.content,
            style: const TextStyle(fontSize: 12, color: AppColors.textSecondary),
          ),
        ),
      );
    }

    final Widget avatar;
    if (isAI) {
      avatar = const CircleAvatar(
        radius: 18,
        backgroundColor: AppColors.successSoft,
        foregroundColor: AppColors.success,
        child: Icon(Icons.smart_toy_outlined, size: 20),
      );
    } else if (isMine) {
      avatar = _AgentAvatar(
        radius: 18,
        imageUrl: myAvatarUrl,
        baseUrl: widget.baseUrl,
        name: '我',
      );
    } else {
      avatar = const _VisitorAvatar();
    }

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisAlignment: isMine
            ? MainAxisAlignment.end
            : MainAxisAlignment.start,
        children: [
          if (!isMine) ...[
            avatar,
            const SizedBox(width: 8),
          ],
          Flexible(
            child: Column(
              crossAxisAlignment: isMine
                  ? CrossAxisAlignment.end
                  : CrossAxisAlignment.start,
              children: [
                if (isAI)
                  Container(
                    margin: const EdgeInsets.only(bottom: 4),
                    padding: const EdgeInsets.symmetric(
                      horizontal: 7,
                      vertical: 3,
                    ),
                    decoration: BoxDecoration(
                      color: AppColors.success.withValues(alpha: .12),
                      borderRadius: BorderRadius.circular(AppRadii.chip),
                    ),
                    child: const Text(
                      'AI客服',
                      style: TextStyle(
                        color: AppColors.success,
                        fontSize: 10,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                if (widget.onLongPress != null)
                  GestureDetector(
                    onLongPress: widget.onLongPress,
                    child: _buildBubbleBody(context, isMine, isAI),
                  )
                else
                  _buildBubbleBody(context, isMine, isAI),
                const SizedBox(height: 3),
                Text(
                  _formatTime(message.createdAt),
                  style: const TextStyle(
                    fontSize: 10,
                    color: AppColors.textMuted,
                  ),
                ),
              ],
            ),
          ),
          if (isMine) ...[
            const SizedBox(width: 8),
            avatar,
          ],
        ],
      ),
    );
  }

  Widget _buildBubbleBody(BuildContext context, bool isMine, bool isAI) {
    final message = widget.message;
    final baseUrl = widget.baseUrl;
    final bubbleColor = isMine ? AppColors.chatBubbleMine : AppColors.surface;
    final textColor = isMine
        ? AppColors.textPrimary
        : AppColors.textPrimary;

    final Widget content;
    if (message.revoked) {
      content = Text(
        '该消息已撤回',
        style: TextStyle(color: textColor.withValues(alpha: .6), fontSize: 13),
      );
    } else if (message.messageType == 'image') {
      final imageUrl = message.content.startsWith('/')
          ? '$baseUrl${message.content}'
          : message.content;
      content = ClipRRect(
        borderRadius: BorderRadius.circular(AppRadii.chip),
        child: Image.network(
          imageUrl,
          width: 200,
          fit: BoxFit.cover,
          errorBuilder: (_, _, _) => Text(message.content),
        ),
      );
    } else if (message.messageType == 'audio') {
      // B4：语音消息气泡，点击播放远端 URL。
      content = InkWell(
        onTap: _toggleAudio,
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              _playingAudio
                  ? Icons.pause_circle_filled
                  : Icons.play_circle_fill,
              size: 28,
              color: AppColors.primary,
            ),
            const SizedBox(width: 8),
            Text(
              _playingAudio ? '播放中…' : '语音消息',
              style: TextStyle(color: textColor, fontSize: 14),
            ),
          ],
        ),
      );
    } else {
      content = Text(
        message.content,
        style: TextStyle(color: textColor, fontSize: 15, height: 1.4),
      );
    }

    // 客服端默认展示中文译文，点击「查看原文」可展开访客/客服输入的原文。
    final original = message.originalContent.trim();
    final hasOriginal =
        original.isNotEmpty && original != message.content.trim();
    // 翻译状态与语种标签：让客服清楚对方原文、译文是否就绪。
    final translationHint = _translationHintFor(message, isMine: isMine);

    return Container(
      constraints: BoxConstraints(
        maxWidth: MediaQuery.sizeOf(context).width * .62,
      ),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
      decoration: BoxDecoration(
        color: bubbleColor,
        borderRadius: BorderRadius.only(
          topLeft: const Radius.circular(AppRadii.bubble),
          topRight: const Radius.circular(AppRadii.bubble),
          bottomLeft: Radius.circular(
            isMine ? AppRadii.bubble : AppRadii.bubbleTail,
          ),
          bottomRight: Radius.circular(
            isMine ? AppRadii.bubbleTail : AppRadii.bubble,
          ),
        ),
        border: isMine
            ? null
            : Border.all(
                color: isAI ? AppColors.successBorder : AppColors.border,
              ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          content,
          if (hasOriginal) ...[
            const SizedBox(height: 6),
            GestureDetector(
              onTap: () => setState(() => _showOriginal = !_showOriginal),
              child: Text(
                _showOriginal ? '收起原文' : '查看原文',
                style: const TextStyle(fontSize: 11, color: AppColors.primary),
              ),
            ),
            if (_showOriginal) ...[
              const SizedBox(height: 4),
              Text(
                original,
                style: TextStyle(
                  color: textColor.withValues(alpha: .55),
                  fontSize: 12,
                  height: 1.4,
                ),
              ),
            ],
          ],
          if (translationHint != null) ...[
            const SizedBox(height: 4),
            Text(
              translationHint,
              style: TextStyle(
                fontSize: 11,
                color: textColor.withValues(alpha: .6),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// 语言代码 → 中文名（与后台的语种标签保持一致）。
const Map<String, String> _languageNames = {
  'zh': '中文',
  'en': '英语',
  'ar': '阿拉伯语',
  'es': '西班牙语',
  'fr': '法语',
  'de': '德语',
  'pt': '葡萄牙语',
  'it': '意大利语',
  'nl': '荷兰语',
  'id': '印尼语',
  'vi': '越南语',
  'tr': '土耳其语',
  'pl': '波兰语',
  'ro': '罗马尼亚语',
  'ru': '俄语',
  'ja': '日语',
  'ko': '韩语',
  'th': '泰语',
  'he': '希伯来语',
  'hi': '印地语',
  'el': '希腊语',
};

String languageLabelFor(String code) {
  final key = code.trim().toLowerCase().split('-').first;
  if (key.isEmpty) return '';
  return _languageNames[key] ?? key.toUpperCase();
}

/// 翻译状态提示：翻译中 / 翻译失败 / 已翻译 / 语种标签；无需展示时返回 null。
String? _translationHintFor(ChatMessage message, {required bool isMine}) {
  final status = message.translationStatus.trim();
  final parts = <String>[];
  if (status == 'pending') {
    parts.add(isMine ? '翻译中…' : '对方原文，翻译中…');
  } else if (status == 'failed') {
    parts.add(isMine ? '翻译失败，已发送原文' : '翻译失败，已显示原文');
  } else if (status == 'done' && isMine) {
    parts.add('已翻译');
  }
  if (!isMine) {
    final label = languageLabelFor(message.detectedLang);
    if (label.isNotEmpty) parts.add('语种：$label');
  }
  if (parts.isEmpty) return null;
  return parts.join(' · ');
}

/// T3：自动翻译已关闭时，输入框上方的常驻提示。
class _NoAutoTranslateHint extends StatelessWidget {
  const _NoAutoTranslateHint();

  @override
  Widget build(BuildContext context) => Container(
    width: double.infinity,
    color: AppColors.warning.withValues(alpha: .10),
    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 7),
    child: const Row(
      children: [
        Icon(Icons.info_outline, size: 14, color: AppColors.warning),
        SizedBox(width: 6),
        Expanded(
          child: Text(
            '自动翻译已关闭 · 对方将看到你的原文',
            style: TextStyle(fontSize: 12, color: AppColors.warning),
          ),
        ),
      ],
    ),
  );
}

/// T2：发送前确认译文的底部弹窗。
/// 返回 `发送译文` 的文本、`发送原文` 的原文，取消时返回 null。
class _TranslationPreviewSheet extends StatefulWidget {
  const _TranslationPreviewSheet({
    required this.preview,
    required this.original,
  });

  final TranslationPreview preview;
  final String original;

  @override
  State<_TranslationPreviewSheet> createState() =>
      _TranslationPreviewSheetState();
}

class _TranslationPreviewSheetState extends State<_TranslationPreviewSheet> {
  late final TextEditingController _controller;

  @override
  void initState() {
    super.initState();
    _controller = TextEditingController(text: widget.preview.translatedText);
    // 译文可编辑，按钮可用状态跟随输入内容。
    _controller.addListener(_onChanged);
  }

  void _onChanged() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    _controller.removeListener(_onChanged);
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final label = languageLabelFor(widget.preview.targetLang);
    final dialect = widget.preview.dialect;
    final text = _controller.text.trim();
    return SafeArea(
      child: Padding(
        padding: EdgeInsets.only(
          bottom: MediaQuery.viewInsetsOf(context).bottom,
        ),
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(18, 16, 18, 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Icon(
                    Icons.translate,
                    size: 20,
                    color: AppColors.primary,
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      label.isEmpty ? '发送前确认译文' : '翻译成$label',
                      style: const TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w700,
                        color: AppColors.textPrimary,
                      ),
                    ),
                  ),
                ],
              ),
              if (dialect.isNotEmpty) ...[
                const SizedBox(height: 4),
                Text(
                  '方言：$dialect',
                  style: const TextStyle(
                    fontSize: 12,
                    color: AppColors.textSecondary,
                  ),
                ),
              ],
              const SizedBox(height: 12),
              TextField(
                controller: _controller,
                minLines: 3,
                maxLines: 6,
                decoration: const InputDecoration(labelText: '译文（可修改）'),
              ),
              const SizedBox(height: 12),
              Container(
                width: double.infinity,
                padding: const EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 9,
                ),
                decoration: BoxDecoration(
                  color: AppColors.background,
                  borderRadius: BorderRadius.circular(AppRadii.input),
                  border: Border.all(color: AppColors.border),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      '原文',
                      style: TextStyle(
                        fontSize: 11,
                        color: AppColors.textMuted,
                      ),
                    ),
                    const SizedBox(height: 3),
                    Text(
                      widget.original,
                      style: const TextStyle(
                        fontSize: 13,
                        height: 1.4,
                        color: AppColors.textSecondary,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 16),
              Row(
                children: [
                  TextButton(
                    onPressed: () => Navigator.of(context).pop(),
                    child: const Text('取消'),
                  ),
                  const Spacer(),
                  TextButton(
                    onPressed: () => Navigator.of(context).pop(widget.original),
                    child: const Text('发送原文'),
                  ),
                  const SizedBox(width: 8),
                  FilledButton(
                    onPressed: text.isEmpty
                        ? null
                        : () => Navigator.of(context).pop(text),
                    child: const Text('发送译文'),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// T1/T2/T3 的偏好单一数据源：一处修改，全 App 生效。
class AgentPreferencesController extends ChangeNotifier {
  AgentPreferencesController(this._client);

  final ServiceBridgeClient _client;
  AgentPreferences _preferences = AgentPreferences.defaults;
  bool _loaded = false;
  bool _loading = false;
  bool _saving = false;
  String? _error;
  bool _disposed = false;

  AgentPreferences get preferences => _preferences;
  bool get loaded => _loaded;
  bool get loading => _loading;
  bool get saving => _saving;

  /// 最近一次读写失败的可展示文案（后端 message 原样带出）。
  String? get error => _error;

  void _notify() {
    if (_disposed) return;
    notifyListeners();
  }

  /// 拉取偏好。[force] 为 true 时即使已有缓存也重新 GET。
  /// 返回是否成功；失败时保留上一次可用值，错误见 [error]。
  Future<bool> load({bool force = false}) async {
    if (_loading) return _loaded;
    if (_loaded && !force) return true;
    _loading = true;
    _error = null;
    _notify();
    try {
      _preferences = await _client.preferences();
      _loaded = true;
      return true;
    } catch (error) {
      _error = _readableError(error);
      return false;
    } finally {
      _loading = false;
      _notify();
    }
  }

  /// 乐观更新：先按新值刷 UI，PATCH 成功后用服务端返回值覆盖，
  /// 失败（如非法 dialect 的 400）则回滚并返回错误文案。返回 null 表示成功。
  Future<String?> update({
    bool? autoTranslateIncoming,
    bool? autoTranslateOutgoing,
    bool? reviewTranslationBeforeSend,
    String? translationDialectMode,
    bool? aiSuggestReplies,
  }) async {
    if (_saving) return null;
    final patch = AgentPreferencesPatch(
      autoTranslateIncoming: autoTranslateIncoming,
      autoTranslateOutgoing: autoTranslateOutgoing,
      reviewTranslationBeforeSend: reviewTranslationBeforeSend,
      translationDialectMode: translationDialectMode,
      aiSuggestReplies: aiSuggestReplies,
    );
    if (patch.isEmpty) return null;
    final previous = _preferences;
    _preferences = previous.copyWith(
      autoTranslateIncoming: autoTranslateIncoming,
      autoTranslateOutgoing: autoTranslateOutgoing,
      reviewTranslationBeforeSend: reviewTranslationBeforeSend,
      translationDialectMode: translationDialectMode,
      aiSuggestReplies: aiSuggestReplies,
    );
    _saving = true;
    _error = null;
    _notify();
    try {
      _preferences = await _client.updatePreferences(patch);
      _loaded = true;
      return null;
    } catch (error) {
      _preferences = previous;
      _error = _readableError(error);
      return _error;
    } finally {
      _saving = false;
      _notify();
    }
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}

/// T1：「我的偏好 / 翻译设置」界面。打开时 GET 回填，改动即时 PATCH 保存。
class PreferencesPage extends StatefulWidget {
  const PreferencesPage({super.key, required this.controller});

  final AgentPreferencesController controller;

  @override
  State<PreferencesPage> createState() => _PreferencesPageState();
}

class _PreferencesPageState extends State<PreferencesPage> {
  @override
  void initState() {
    super.initState();
    // 打开界面时重新 GET 一次，保证回填的是服务端最新值。
    unawaited(_reload());
  }

  Future<void> _reload() async {
    final ok = await widget.controller.load(force: true);
    if (!mounted || ok) return;
    final message = widget.controller.error;
    if (message != null && message.isNotEmpty) {
      _showMessage(context, '偏好加载失败：$message');
    }
  }

  Future<void> _save({
    bool? autoTranslateIncoming,
    bool? autoTranslateOutgoing,
    bool? reviewTranslationBeforeSend,
    String? translationDialectMode,
    bool? aiSuggestReplies,
  }) async {
    final error = await widget.controller.update(
      autoTranslateIncoming: autoTranslateIncoming,
      autoTranslateOutgoing: autoTranslateOutgoing,
      reviewTranslationBeforeSend: reviewTranslationBeforeSend,
      translationDialectMode: translationDialectMode,
      aiSuggestReplies: aiSuggestReplies,
    );
    if (!mounted) return;
    if (error == null) {
      _showMessage(context, '偏好已保存');
    } else {
      // 不静默失败：400 等错误直接展示后端 message，UI 已由 controller 回滚。
      _showMessage(context, '保存失败：$error');
    }
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: widget.controller,
      builder: (context, _) {
        final controller = widget.controller;
        final preferences = controller.preferences;
        final enabled = !controller.saving;
        return Scaffold(
          appBar: AppBar(
            title: const Text(
              '我的偏好',
              style: TextStyle(fontWeight: FontWeight.w700),
            ),
            actions: [
              if (controller.saving)
                const Padding(
                  padding: EdgeInsets.only(right: 16),
                  child: Center(
                    child: SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  ),
                ),
            ],
          ),
          body: !controller.loaded
              ? controller.loading
                    ? const Center(child: CircularProgressIndicator())
                    : _PreferencesErrorState(
                        message: controller.error ?? '偏好加载失败',
                        onRetry: () => unawaited(_reload()),
                      )
              : ListView(
                  padding: const EdgeInsets.fromLTRB(16, 16, 16, 28),
                  children: [
                    const _PreferencesSectionLabel('翻译设置'),
                    Container(
                      decoration: AppTheme.cardDecoration(),
                      child: Column(
                        children: [
                          _PreferenceSwitchTile(
                            icon: Icons.translate,
                            title: '访客消息自动翻译成中文',
                            description: '开启后访客消息先翻译成中文再展示；关闭则只看到对方原文。',
                            value: preferences.autoTranslateIncoming,
                            onChanged: enabled
                                ? (value) => _save(autoTranslateIncoming: value)
                                : null,
                          ),
                          const Divider(height: 1, indent: 52),
                          _PreferenceSwitchTile(
                            icon: Icons.send_outlined,
                            title: '我的消息自动翻译成访客语言',
                            description: '关闭后对方将看到你输入的原文，请自行确认对方能看懂。',
                            value: preferences.autoTranslateOutgoing,
                            onChanged: enabled
                                ? (value) => _save(autoTranslateOutgoing: value)
                                : null,
                          ),
                          const Divider(height: 1, indent: 52),
                          _PreferenceSwitchTile(
                            icon: Icons.fact_check_outlined,
                            title: '发送前确认译文',
                            description: '发送前先展示译文供你确认和修改；关闭则直接发送原文，由服务端自动翻译。',
                            value: preferences.reviewTranslationBeforeSend,
                            onChanged: enabled
                                ? (value) =>
                                      _save(reviewTranslationBeforeSend: value)
                                : null,
                          ),
                          const Divider(height: 1, indent: 52),
                          _PreferenceBlock(
                            icon: Icons.record_voice_over_outlined,
                            title: '翻译风格',
                            description: '含方言时按访客所在地的用词习惯翻译，例如拉丁美洲西班牙语。',
                            child: _DialectModeSelector(
                              mode: preferences.translationDialectMode,
                              onChanged: enabled
                                  ? (value) =>
                                        _save(translationDialectMode: value)
                                  : null,
                            ),
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 18),
                    const _PreferencesSectionLabel('AI 助手'),
                    Container(
                      decoration: AppTheme.cardDecoration(),
                      child: _PreferenceSwitchTile(
                        icon: Icons.smart_toy_outlined,
                        title: 'AI 推荐回复',
                        description: '开启后由 AI 生成可直接发送的推荐回复内容，供你参考和引用。',
                        value: preferences.aiSuggestReplies,
                        onChanged: enabled
                            ? (value) => _save(aiSuggestReplies: value)
                            : null,
                      ),
                    ),
                    if (controller.error != null) ...[
                      const SizedBox(height: 14),
                      _PreferencesInlineError(message: controller.error!),
                    ],
                    const SizedBox(height: 18),
                    const Text(
                      '偏好只对当前客服账号生效，修改后立即同步到服务端。',
                      style: TextStyle(
                        fontSize: 12,
                        color: AppColors.textMuted,
                      ),
                    ),
                  ],
                ),
        );
      },
    );
  }
}

class _PreferencesSectionLabel extends StatelessWidget {
  const _PreferencesSectionLabel(this.text);

  final String text;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(left: 4, bottom: 8),
    child: Text(
      text,
      style: const TextStyle(
        fontSize: 13,
        fontWeight: FontWeight.w700,
        color: AppColors.textSecondary,
      ),
    ),
  );
}

class _PreferenceSwitchTile extends StatelessWidget {
  const _PreferenceSwitchTile({
    required this.icon,
    required this.title,
    required this.description,
    required this.value,
    required this.onChanged,
  });

  final IconData icon;
  final String title;
  final String description;
  final bool value;
  final ValueChanged<bool>? onChanged;

  @override
  Widget build(BuildContext context) => SwitchListTile(
    value: value,
    onChanged: onChanged,
    activeThumbColor: AppColors.primary,
    secondary: Icon(icon, color: AppColors.textSecondary),
    contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
    title: Text(
      title,
      style: const TextStyle(
        fontSize: 15,
        fontWeight: FontWeight.w600,
        color: AppColors.textPrimary,
      ),
    ),
    subtitle: Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Text(
        description,
        style: const TextStyle(
          fontSize: 12,
          height: 1.3,
          color: AppColors.textSecondary,
        ),
      ),
    ),
  );
}

class _PreferenceBlock extends StatelessWidget {
  const _PreferenceBlock({
    required this.icon,
    required this.title,
    required this.description,
    required this.child,
  });

  final IconData icon;
  final String title;
  final String description;
  final Widget child;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.fromLTRB(16, 14, 16, 16),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(icon, size: 22, color: AppColors.textSecondary),
            const SizedBox(width: 14),
            Expanded(
              child: Text(
                title,
                style: const TextStyle(
                  fontSize: 15,
                  fontWeight: FontWeight.w600,
                  color: AppColors.textPrimary,
                ),
              ),
            ),
          ],
        ),
        const SizedBox(height: 6),
        Text(
          description,
          style: const TextStyle(
            fontSize: 12,
            height: 1.3,
            color: AppColors.textSecondary,
          ),
        ),
        const SizedBox(height: 12),
        child,
      ],
    ),
  );
}

/// 翻译风格二选一：标准语 / 含方言。
class _DialectModeSelector extends StatelessWidget {
  const _DialectModeSelector({required this.mode, required this.onChanged});

  final String mode;

  /// 为 null 时禁用点选（保存中）。
  final ValueChanged<String>? onChanged;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.all(3),
    decoration: BoxDecoration(
      color: AppColors.primarySoft,
      borderRadius: BorderRadius.circular(AppRadii.input),
    ),
    child: Row(
      children: [
        _option(AgentPreferences.dialectModeStandard, '标准语'),
        _option(AgentPreferences.dialectModeDialect, '含方言'),
      ],
    ),
  );

  Widget _option(String value, String label) {
    final selected = mode == value;
    return Expanded(
      child: InkWell(
        onTap: onChanged == null || selected ? null : () => onChanged!(value),
        borderRadius: BorderRadius.circular(AppRadii.input - 2),
        child: Container(
          padding: const EdgeInsets.symmetric(vertical: 9),
          decoration: BoxDecoration(
            color: selected ? AppColors.surface : Colors.transparent,
            borderRadius: BorderRadius.circular(AppRadii.input - 2),
          ),
          child: Text(
            label,
            textAlign: TextAlign.center,
            style: TextStyle(
              fontSize: 13,
              fontWeight: selected ? FontWeight.w700 : FontWeight.w500,
              color: selected ? AppColors.primary : AppColors.textSecondary,
            ),
          ),
        ),
      ),
    );
  }
}

class _PreferencesInlineError extends StatelessWidget {
  const _PreferencesInlineError({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
    decoration: BoxDecoration(
      color: AppColors.danger.withValues(alpha: .08),
      borderRadius: BorderRadius.circular(AppRadii.input),
      border: Border.all(color: AppColors.danger.withValues(alpha: .3)),
    ),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Icon(Icons.error_outline, size: 16, color: AppColors.danger),
        const SizedBox(width: 8),
        Expanded(
          child: Text(
            '部分设置未同步：$message',
            style: const TextStyle(fontSize: 12, color: AppColors.danger),
          ),
        ),
      ],
    ),
  );
}

class _PreferencesErrorState extends StatelessWidget {
  const _PreferencesErrorState({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(28),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(
            Icons.cloud_off_outlined,
            size: 56,
            color: AppColors.textFaint,
          ),
          const SizedBox(height: 14),
          const Text('偏好加载失败', style: TextStyle(fontWeight: FontWeight.w700)),
          const SizedBox(height: 6),
          Text(
            message,
            textAlign: TextAlign.center,
            style: const TextStyle(
              fontSize: 12,
              color: AppColors.textSecondary,
            ),
          ),
          const SizedBox(height: 18),
          FilledButton(onPressed: onRetry, child: const Text('重试')),
        ],
      ),
    ),
  );
}

class ChangePasswordDialog extends StatefulWidget {
  const ChangePasswordDialog({super.key, required this.client});
  final ServiceBridgeClient client;
  @override
  State<ChangePasswordDialog> createState() => _ChangePasswordDialogState();
}

class _ChangePasswordDialogState extends State<ChangePasswordDialog> {
  final _formKey = GlobalKey<FormState>();
  final _current = TextEditingController();
  final _next = TextEditingController();
  bool _busy = false;
  @override
  void dispose() {
    _current.dispose();
    _next.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() => _busy = true);
    try {
      await widget.client.changePassword(_current.text, _next.text);
      if (mounted) {
        Navigator.pop(context);
        _showMessage(context, '密码已修改，请重新登录');
      }
    } catch (error) {
      if (mounted) _showMessage(context, _readableError(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('修改密码'),
    content: Form(
      key: _formKey,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextFormField(
            controller: _current,
            obscureText: true,
            decoration: const InputDecoration(labelText: '当前密码'),
            validator: _required,
          ),
          const SizedBox(height: 12),
          TextFormField(
            controller: _next,
            obscureText: true,
            decoration: const InputDecoration(labelText: '新密码（至少 10 位）'),
            validator: (value) =>
                (value ?? '').length < 10 ? '新密码至少 10 位' : null,
          ),
        ],
      ),
    ),
    actions: [
      TextButton(
        onPressed: _busy ? null : () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      FilledButton(onPressed: _busy ? null : _save, child: const Text('保存')),
    ],
  );
}

String? _required(String? value) =>
    (value ?? '').trim().isEmpty ? '此项不能为空' : null;
String _readableError(Object error) => error is ServiceBridgeException
    ? error.message
    : error.toString().replaceFirst('Exception: ', '');
String _formatTime(DateTime? value) {
  if (value == null) return '';
  final now = DateTime.now();
  if (now.difference(value).inDays == 0) {
    return '${value.hour.toString().padLeft(2, '0')}:${value.minute.toString().padLeft(2, '0')}';
  }
  return '${value.month}/${value.day} ${value.hour.toString().padLeft(2, '0')}:${value.minute.toString().padLeft(2, '0')}';
}

String _statusLabel(String status) {
  switch (status) {
    case 'waiting':
    case 'human_requested':
      return '待接入';
    case 'ai_serving':
      return 'AI接待中';
    case 'closed':
      return '已结束';
    case 'assigned':
      return '服务中';
    default:
      return status;
  }
}

void _showMessage(BuildContext context, String message) {
  ScaffoldMessenger.maybeOf(
    context,
  )?.showSnackBar(SnackBar(content: Text(message)));
}

Future<bool> _confirm(
  BuildContext context,
  String title,
  String message,
) async =>
    await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title),
        content: Text(message),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确定'),
          ),
        ],
      ),
    ) ??
    false;

/// B4：全局单实例语音播放器，保证同一时刻只有一条语音在播放。
class _VoicePlaybackController {
  _VoicePlaybackController._();

  static final _VoicePlaybackController instance = _VoicePlaybackController._();

  final AudioPlayer _player = AudioPlayer();
  String? _currentUrl;

  /// 播放 [url]；Future 在播放结束、暂停或被 stop 后完成。
  Future<void> play(String url) async {
    if (_currentUrl != url) {
      await _player.stop();
      _currentUrl = url;
      await _player.setUrl(url);
    } else {
      await _player.seek(Duration.zero);
    }
    await _player.play();
  }

  Future<void> stop() async {
    try {
      await _player.stop();
    } catch (_) {
      // 停止失败无需向上抛出。
    }
  }
}
