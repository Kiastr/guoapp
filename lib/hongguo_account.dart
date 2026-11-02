// 红果账号数据同步的模型。
//
// 对应 Go 侧 provider_hongguo_account.go 的 hongguoAccountSnapshot。
//
// 同步是**只读**的：把账号里的收藏与观看记录拉回来并入本地，不回写账号。
// 这是与「登录」分开的两件事——登录只负责拿到 Cookie，同步才把数据取出来。
//
// 边界：账号里的剧 source 固定 `hongguo`——红果 App 的 book_id 与官网
// detail 路径是同一套数字 id；点开仍走现有红果详情链路。同步失败不影响
// 正常看剧，设置页只提示一句，不弹阻断式弹窗。

import 'models.dart';

/// 一条账号条目（收藏或观看记录）。
///
/// 收藏只填前六个字段；观看记录额外带集号与进度。
class HongguoAccountEntry {
  const HongguoAccountEntry({
    this.seriesId = '',
    this.title = '',
    this.cover = '',
    this.description = '',
    this.episodes = 0,
    this.category = '',
    this.episode = 0,
    this.episodeId = '',
    this.position = 0,
    this.duration = 0,
    this.updatedAt = 0,
    this.collectedAt = 0,
  });

  final String seriesId;
  final String title;
  final String cover;
  final String description;
  final int episodes;
  final String category;

  /// 观看记录专用：集号（1 起）、分集 ID、播放位置与时长（秒）。
  final int episode;
  final String episodeId;
  final double position;
  final double duration;

  /// 观看时间 / 收藏时间（Unix 毫秒）。
  final int updatedAt;
  final int collectedAt;

  /// 转成本地剧对象。
  ///
  /// id 必须是 `hongguo:<seriesId>`——本地剧目的身份约定是「源:源内 id」，
  /// 只写数字会在写入收藏时被判成身份不一致。sourceId 存纯数字，详情请求
  /// 时由 provider 自己拼回源站路径。
  Drama toDrama() => Drama(
    id: '${SourceSite.hongguo.id}:$seriesId',
    source: SourceSite.hongguo.id,
    sourceId: seriesId,
    title: title.isEmpty ? '红果剧集 $seriesId' : title,
    cover: cover,
    description: description,
    episodes: episodes,
    category: category,
  );

  factory HongguoAccountEntry.fromJson(Map<String, dynamic> json) {
    String text(String key) => (json[key] as String?) ?? '';
    int number(String key) => (json[key] as num?)?.toInt() ?? 0;
    double decimal(String key) => (json[key] as num?)?.toDouble() ?? 0;
    return HongguoAccountEntry(
      seriesId: text('seriesId'),
      title: text('title'),
      cover: text('cover'),
      description: text('description'),
      episodes: number('episodes'),
      category: text('category'),
      episode: number('episode'),
      episodeId: text('episodeId'),
      position: decimal('position'),
      duration: decimal('duration'),
      updatedAt: number('updatedAt'),
      collectedAt: number('collectedAt'),
    );
  }
}

/// 一次账号同步的结果。
class HongguoAccountSnapshot {
  const HongguoAccountSnapshot({
    this.signedIn = false,
    this.userId = '',
    this.nickname = '',
    this.favorites = const [],
    this.history = const [],
    this.syncedAt = 0,
    this.warning = '',
  });

  final bool signedIn;
  final String userId;
  final String nickname;
  final List<HongguoAccountEntry> favorites;
  final List<HongguoAccountEntry> history;

  /// 本次同步时间（Unix 秒），0 表示没同步过。
  final int syncedAt;

  /// 部分失败说明（例如收藏成功、观看记录失败）。空串表示全部成功。
  final String warning;

  String get displayName {
    if (nickname.isNotEmpty) return nickname;
    if (userId.isNotEmpty) return '红果用户 $userId';
    return '红果账号';
  }

  factory HongguoAccountSnapshot.fromJson(Map<String, dynamic> json) {
    List<HongguoAccountEntry> rows(String key) => [
      for (final row in (json[key] as List?) ?? const [])
        if (row is Map)
          HongguoAccountEntry.fromJson(Map<String, dynamic>.from(row)),
    ];
    return HongguoAccountSnapshot(
      signedIn: (json['signedIn'] as bool?) ?? false,
      userId: (json['userId'] as String?) ?? '',
      nickname: (json['nickname'] as String?) ?? '',
      favorites: rows('favorites'),
      history: rows('history'),
      syncedAt: (json['syncedAt'] as num?)?.toInt() ?? 0,
      warning: (json['warning'] as String?) ?? '',
    );
  }
}
