// Package hgv5 封装红果 2026-11-23 升级后的 V5 取流协议（对齐 Amor5207/lvguo-tv v2.0.8，GPL-3.0）。
//
// 背景：2026-11-23 起红果服务端收紧了 V5 取流身份与签名校验。旧的精简 Medusa 结构、
// 3 个签名头、22 字段 query 一律返回 HTTP 200 空 body。v2.0.8 用完整设备注册画像
// （device_id/install_id/cdid/openudid/clientudid + klink_egdi + app_alert_check 激活）、
// 完整扩展 Medusa + 25 位 sec_device_token、9 个安全头、36 字段 query 解决了该问题。
// 本包在沙箱实网验证：GET video_tab 返回 code=0 + 23KB 完整 video_model。
//
// 本包为纯标准库实现，作为独立子包引入，不改动既有取流/解析/健康度逻辑。
// 对外只暴露 FetchModel：返回与 legacy data[vid].video_model 兼容的响应 map，
// 由调用方复用既有的 fallback_api 解析、第二段取址与清晰度选择。
package hgv5

import (
	"context"

	play "duanjuapp/native/core/hgv5/play"
)

// FetchModel 用 v2.0.8 的 V5 身份+签名体系取 vid 的播放模型。
// 返回值形如 {"code":0,"data":{"<vid>":{"video_model":{...}, ...}}}，
// 与 provider_hongguo_drpy_media.go 的 hgDrpyFallbackFromModel 期望结构一致。
func FetchModel(ctx context.Context, videoID string) (map[string]any, error) {
	return play.PlayVideoTab(ctx, videoID)
}
