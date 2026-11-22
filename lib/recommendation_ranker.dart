import 'models.dart';

/// [+221] 方案 B：本地题材画像重排。
///
/// 【要解决什么】「个性推荐」(feed_behavior) 与「公开推荐」(feed) 首批内容一致——
/// 根因是服务端只认「签名设备身份 + 账号 Cookie + 曝光回传」，匿名拼凑的曝光
/// 上下文不被采纳。既然服务端这条路对匿名客户端走不通，就在**客户端本地**做一件
/// 真正有区分度的事：用本机「看过 / 收藏过」的剧做题材画像，把推荐批次里同题材的
/// 剧往前排。这样首批立刻和公开流不同，且不依赖任何登录态或服务端风控豁免。
///
/// 【为什么用标题 n-gram，而不是 tags/category】实测 2026-11-22 推荐卡片：
///   cell_extra_data = null，video_detail 只有 series_title / content_type /
///   series_cover / episode_cnt —— 没有 tag / category 字段（见 provider_hongguo_cell_feed.go
///   的解析与 /tmp/hgref/probe2.py 的真实响应）。而本地 history/favorites 的剧
///   也常常没有完整 tags。唯一稳定存在的是 **title**。中文题材词（修仙 / 神医 /
///   赘婿 / 重生 / 保镖 / 年代…）多为 2 字，用相邻 2 字 gram 能很好捕获题材信号，
///   不依赖任何服务端字段，纯本地可算。
///
/// 【信号强度与诚实降级】
///   - 收藏 gram 权重 3.0，观看 gram 权重 2.0→1.0（越近越高）。
///   - 候选标题 gram 命中画像才加分；全不命中 = 0 分，保持原顺序。
///   - 本地没有任何历史/收藏时不构建画像，原样返回——不假装个性化。
///   - 这是「重排」（从服务端给的批次里挑你爱看的排前面），不是「召回」，
///     池子里没有的题材推不出来。局限写在这里，避免误当成真·服务端画像。
///
/// 【为什么是纯 Dart 类、不依赖 Flutter】便于用 Python 复刻算法做逻辑核验
/// （本环境 Dart 3.7 低于项目要求的 3.12，跑不了 flutter test）；无副作用、
/// 输入只来自参数，不读全局，方便未来单测。
class LocalTasteRanker {
  LocalTasteRanker._(this._profile, this.hasProfile);

  final Map<String, double> _profile;

  /// 画像是否非空。false 时调用方应直接用原列表（无历史 = 不做假个性化）。
  final bool hasProfile;

  /// 从本地观看历史 + 收藏构建题材画像。
  ///
  /// [maxHistory] 只取最近 N 条观看，避免陈年记录稀释当下偏好；
  /// 收藏全量计入（收藏本身就是强偏好信号）。
  factory LocalTasteRanker.build({
    required List<WatchEntry> history,
    required List<Drama> favorites,
    int maxHistory = 40,
  }) {
    final profile = <String, double>{};
    final recent = history.length > maxHistory
        ? history.sublist(0, maxHistory)
        : history;
    for (var i = 0; i < recent.length; i++) {
      // 越靠前（越近）权重越高：2.0 线性衰减到 1.0。
      final weight = 2.0 - (i / (maxHistory == 0 ? 1 : maxHistory));
      for (final gram in _titleGrams(recent[i].drama.title)) {
        profile[gram] = (profile[gram] ?? 0) + weight;
      }
    }
    for (final drama in favorites) {
      for (final gram in _titleGrams(drama.title)) {
        profile[gram] = (profile[gram] ?? 0) + 3.0;
      }
    }
    return LocalTasteRanker._(profile, profile.isNotEmpty);
  }

  /// 稳定重排：有题材匹配的排前面，同分保持服务端原顺序。
  ///
  /// Dart 的 List.sort 不保证稳定，所以用 (score, 原下标) 复合比较：
  /// 分数降序、同分按下标升序，避免无偏好时把列表打乱成随机序。
  List<Drama> rank(List<Drama> candidates) {
    if (!hasProfile || candidates.length < 2) return candidates;
    final indexed = <_Scored>[];
    for (var i = 0; i < candidates.length; i++) {
      indexed.add(_Scored(_score(candidates[i].title), i, candidates[i]));
    }
    indexed.sort((a, b) {
      final byScore = b.score.compareTo(a.score);
      return byScore != 0 ? byScore : a.index.compareTo(b.index);
    });
    return [for (final item in indexed) item.drama];
  }

  /// 候选标题的题材分：命中的 gram 权重求和，按 gram 数轻微归一化，
  /// 防止超长标题仅凭「撞词多」就压过精准匹配。
  double _score(String title) {
    final grams = _titleGrams(title);
    if (grams.isEmpty) return 0;
    var total = 0.0;
    for (final gram in grams) {
      total += _profile[gram] ?? 0;
    }
    if (total <= 0) return 0;
    return total / (1 + 0.15 * grams.length);
  }

  /// 提取标题的相邻 2 字中文 gram。
  ///
  /// 用 rune 遍历（避免代理对问题）；只保留汉字；过滤「第/季/部/集/卷」等
  /// 高频季数词与营销词，否则「第七季」「全集免费」会让所有剧互相撞词、
  /// 画像退化成噪声。
  static List<String> _titleGrams(String title) {
    final chars = <String>[];
    for (final rune in title.runes) {
      final ch = String.fromCharCode(rune);
      final code = rune;
      if (code >= 0x4E00 && code <= 0x9FFF) chars.add(ch);
    }
    const stopChars = '第季部集卷篇局全集免费观看完整高清大结局';
    final grams = <String>[];
    for (var i = 0; i + 1 < chars.length; i++) {
      final gram = chars[i] + chars[i + 1];
      if (gram.runes.any((r) => stopChars.runes.contains(r))) continue;
      grams.add(gram);
    }
    return grams;
  }
}

class _Scored {
  _Scored(this.score, this.index, this.drama);

  final double score;
  final int index;
  final Drama drama;
}
