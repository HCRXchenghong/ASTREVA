import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'service_bridge.dart';
import 'package:http/http.dart' as http;

class AppUpdateInfo {
  const AppUpdateInfo({
    required this.updateAvailable,
    required this.forceUpdate,
    required this.latestVersion,
    required this.latestVersionCode,
    required this.releaseNotes,
    required this.downloadUrl,
    required this.checksum,
  });

  final bool updateAvailable;
  final bool forceUpdate;
  final String latestVersion;
  final int latestVersionCode;
  final String releaseNotes;
  final String downloadUrl;
  final String checksum;

  /// iOS 更新跳转地址：后台配置了商店/下载地址时优先使用，
  /// 否则回退到 App Store 搜索页（B8），避免 iOS 上无法跳转。
  String get iosStoreUrl {
    final url = downloadUrl.trim();
    final looksLikeStore = RegExp(
      r'^https?://',
    ).hasMatch(url) && !url.contains('/api/app-update/download/');
    if (looksLikeStore) return url;
    final term = Uri.encodeComponent('ASTREVA 在线客服');
    return 'https://apps.apple.com/search?term=$term';
  }
}

class AppUpdater {
  static const MethodChannel _channel = MethodChannel(
    'com.astreva.online_customer_service/updater',
  );

  static const int currentVersionCode = int.fromEnvironment(
    'APP_VERSION_CODE',
    defaultValue: 1,
  );

  static bool get isAndroid => Platform.isAndroid;
  static bool get isIOS => Platform.isIOS;

  static Future<AppUpdateInfo> checkForUpdate(String baseUrl) async {
    final uri = Uri.parse('$baseUrl/api/app-update/latest').replace(
      queryParameters: {
        'platform': isAndroid ? 'android' : 'ios',
        'version_code': '$currentVersionCode',
      },
    );
    final response = await http.get(uri).timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw HttpException('update check failed: ${response.statusCode}');
    }
    final payload = jsonDecode(response.body) as Map<String, dynamic>;
    final latest = payload['latest'] as Map<String, dynamic>?;
    if (latest == null) {
      return const AppUpdateInfo(
        updateAvailable: false,
        forceUpdate: false,
        latestVersion: '',
        latestVersionCode: 0,
        releaseNotes: '',
        downloadUrl: '',
        checksum: '',
      );
    }
    final versionCode = (latest['version_code'] as num?)?.toInt() ?? 0;
    final downloadUrl = (latest['download_url'] as String? ?? '').trim();
    return AppUpdateInfo(
      updateAvailable: payload['update_available'] == true,
      forceUpdate: payload['force_update'] == true,
      latestVersion: latest['version_name'] as String? ?? '',
      latestVersionCode: versionCode,
      releaseNotes: latest['release_notes'] as String? ?? '',
      downloadUrl: downloadUrl.isEmpty
          ? '$baseUrl/api/app-update/download/${latest['id'] ?? ''}'
          : downloadUrl,
      checksum: latest['checksum'] as String? ?? '',
    );
  }

  static Future<bool> canInstallUnknownApps() async {
    if (!isAndroid) return true;
    try {
      return await _channel.invokeMethod<bool>('canInstallUnknownApps') ??
          false;
    } catch (_) {
      return false;
    }
  }

  static Future<void> openInstallSettings() async {
    if (!isAndroid) return;
    try {
      await _channel.invokeMethod('openInstallSettings');
    } catch (_) {
      // Settings intent is optional; the user can enable it manually.
    }
  }

  /// 用系统浏览器/商店打开外部链接（B8：iOS 跳转 App Store）。
  static Future<void> openExternalUrl(String url) async {
    final target = url.trim();
    if (target.isEmpty) {
      throw const ServiceBridgeException('更新地址为空，无法打开应用商店。');
    }
    try {
      final opened = await _channel.invokeMethod<bool>('openUrl', {
        'url': target,
      });
      if (opened == false) {
        throw const ServiceBridgeException('无法打开应用商店，请手动前往下载。');
      }
    } on MissingPluginException {
      throw ServiceBridgeException('请手动前往下载：$target');
    } on PlatformException catch (error) {
      throw ServiceBridgeException(error.message ?? '无法打开应用商店：$target');
    }
  }

  static Future<String> downloadApk(
    String url, {
    void Function(int received, int? total)? onProgress,
    String expectedChecksum = '',
  }) async {
    if (!isAndroid) {
      throw const ServiceBridgeException('当前设备不能直接安装更新包，请前往应用商店更新。');
    }
    final request = http.Request('GET', Uri.parse(url));
    final response = await request.send().timeout(const Duration(minutes: 10));
    if (response.statusCode != 200) {
      throw ServiceBridgeException('安装包下载失败：HTTP ${response.statusCode}');
    }
    final contentLength = response.contentLength;
    final directory = Directory('${Directory.systemTemp.path}/updates');
    if (!await directory.exists()) {
      await directory.create(recursive: true);
    }
    final file = File(
      '${directory.path}/astreva-online-service-${DateTime.now().millisecondsSinceEpoch}.apk',
    );
    final sink = file.openWrite();
    var received = 0;
    await for (final chunk in response.stream) {
      received += chunk.length;
      sink.add(chunk);
      onProgress?.call(received, contentLength);
    }
    await sink.close();
    await _verifyChecksum(file, expectedChecksum);
    return file.path;
  }

  /// B9：下载完成后校验 sha256，不匹配则删除文件并报错；
  /// checksum 为空时跳过校验但记录日志。
  static Future<void> _verifyChecksum(File file, String expected) async {
    final expectedHash = expected.trim().toLowerCase().replaceFirst(
      RegExp(r'^sha256:'),
      '',
    );
    if (expectedHash.isEmpty) {
      debugPrint('[update] 更新包未提供 checksum，跳过校验：${file.path}');
      return;
    }
    final actual = (await sha256.bind(file.openRead()).first).toString();
    if (actual == expectedHash) return;
    try {
      await file.delete();
    } catch (error) {
      debugPrint('[update] 校验失败后删除更新包失败：$error');
    }
    throw const ServiceBridgeException('更新包校验失败（sha256 不匹配），已删除下载文件。');
  }

  static Future<void> installApk(String path) async {
    if (!isAndroid) {
      throw const ServiceBridgeException('当前设备不能直接安装更新包。');
    }
    await _channel.invokeMethod('installApk', {'path': path});
  }
}
