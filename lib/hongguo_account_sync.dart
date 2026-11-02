// 红果账号同步的执行入口。
//
// 启动时的自动同步与设置页的「立即同步」走同一段逻辑：拉数据 → 并入本地
// → 记录同步时间。放在独立文件里，避免两个调用点各写一遍 try/catch 与
// 提示文案，也保证「启动同步失败不弹窗」这类约定只有一处。

import 'core_bridge.dart';
import 'local_store.dart';

/// 一次同步的结果。调用方按 [error] / [warning] 决定怎么展示。
class HongguoAccountOutcome {
  const HongguoAccountOutcome({
    this.signedIn = false,
    this.merged = 0,
    this.warning = '',
    this.error = '',
  });

  /// 是否已登录。未登录时其余字段无意义，界面不该报错。
  final bool signedIn;

  /// 实际写入本地的条目数（收藏 + 历史的新增/更新条数）。
  final int merged;

  /// 部分失败说明（例如收藏失败、历史成功）。空串表示全成功。
  final String warning;

  /// 整体失败（网络、登录态失效、设备身份不可用等）。空串表示没出错。
  final String error;

  bool get ok => error.isEmpty;

  /// 给界面用的一句话。
  String get summary {
    if (error.isNotEmpty) return error;
    if (!signedIn) return '未登录红果账号';
    if (warning.isNotEmpty) return warning;
    if (merged == 0) return '已是最新，没有新内容';
    return '已同步 $merged 项';
  }
}

/// 同步一次账号数据。
///
/// 不抛异常：启动阶段没人接错误，抛出去只会变成未捕获异常。所有失败都
/// 收敛到 [HongguoAccountOutcome.error]。
Future<HongguoAccountOutcome> syncHongguoAccount(
  AppRepository repository,
  LocalStore store,
) async {
  try {
    final snapshot = await repository.hongguoAccountSync();
    if (!snapshot.signedIn) {
      return const HongguoAccountOutcome(signedIn: false);
    }
    final merged = await store.mergeHongguoAccount(snapshot);
    if (snapshot.syncedAt > 0) {
      await store.setHongguoAccountSyncAt(snapshot.syncedAt);
    }
    return HongguoAccountOutcome(
      signedIn: true,
      merged: merged,
      warning: snapshot.warning,
    );
  } catch (error) {
    return HongguoAccountOutcome(error: _errorText(error));
  }
}

String _errorText(Object error) {
  if (error is AppFailure) return error.message;
  final text = error.toString();
  return text.startsWith('Exception: ') ? text.substring(11) : text;
}
