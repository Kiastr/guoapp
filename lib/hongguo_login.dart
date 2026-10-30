// 红果官网扫码登录的数据模型。
//
// 对应 Go 侧 provider_hongguo_web_login.go 的 hongguoLoginQRCode /
// hongguoLoginPollResult / hongguoLoginView。
//
// 链路：get_qrcode 拿 token + 二维码 PNG → check_qrconnect 轮询状态 →
// 手机确认后服务端下发 sessionid，Go 侧落盘。
//
// 能力边界（重要）：登录解决的是「账号态」——收藏、观看历史、云端续播。
// 播放取流不依赖登录，登不登录都能看同样的剧，也不改变画质。界面上不要
// 暗示「登录解锁更多内容」，那是误导。

/// 一次扫码会话：token 用于轮询，image 是二维码 PNG 的纯 base64。
class HongguoLoginQRCode {
  const HongguoLoginQRCode({
    this.token = '',
    this.image = '',
    this.expiresIn = 0,
  });

  /// 轮询凭据。只在本次扫码有效，确认或过期后服务端即作废。
  final String token;

  /// 二维码 PNG 的纯 base64（不含 data URI 前缀），可直接 base64Decode。
  final String image;

  /// 剩余有效秒数（Go 侧已把服务端的 Unix 时间戳归一成倒计时）。
  /// 0 表示服务端没给，界面按「请尽快扫码」处理。
  final int expiresIn;

  factory HongguoLoginQRCode.fromJson(Map<String, dynamic> json) {
    return HongguoLoginQRCode(
      token: (json['token'] as String?) ?? '',
      image: (json['image'] as String?) ?? '',
      expiresIn: (json['expiresIn'] as num?)?.toInt() ?? 0,
    );
  }
}

/// 登录态。signedIn 为假时其余字段无意义。
class HongguoLoginState {
  const HongguoLoginState({
    this.signedIn = false,
    this.userId = '',
    this.nickname = '',
    this.updatedAt = 0,
  });

  final bool signedIn;
  final String userId;

  /// 昵称可能为空——有些账号就是没设过。
  final String nickname;

  /// 最近一次确认登录的时间（Unix 秒）。
  final int updatedAt;

  /// 界面展示名：优先昵称，其次 uid，都没有就「红果账号」。
  String get displayName {
    if (nickname.isNotEmpty) return nickname;
    if (userId.isNotEmpty) return '红果用户 $userId';
    return '红果账号';
  }

  factory HongguoLoginState.fromJson(Map<String, dynamic> json) {
    return HongguoLoginState(
      signedIn: (json['signedIn'] as bool?) ?? false,
      userId: (json['userId'] as String?) ?? '',
      nickname: (json['nickname'] as String?) ?? '',
      updatedAt: (json['updatedAt'] as num?)?.toInt() ?? 0,
    );
  }
}

/// 单次轮询结果。status 语义照搬服务端：
/// new=待扫描；confirmed=已确认（此时 state 为最新登录态）；
/// expired=过期/失效；其他值原样透出。
class HongguoLoginPollResult {
  const HongguoLoginPollResult({
    this.status = 'new',
    this.description = '',
    this.state,
  });

  final String status;

  /// 服务端的可读说明（非空时优先展示给用户）。
  final String description;

  /// 仅 confirmed 时非空。
  final HongguoLoginState? state;

  bool get confirmed => status == 'confirmed';

  bool get expired => status == 'expired';

  factory HongguoLoginPollResult.fromJson(Map<String, dynamic> json) {
    final raw = json['session'];
    return HongguoLoginPollResult(
      status: (json['status'] as String?) ?? 'new',
      description: (json['description'] as String?) ?? '',
      state: raw is Map<String, dynamic>
          ? HongguoLoginState.fromJson(raw)
          : null,
    );
  }
}
