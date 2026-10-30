package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// hongguoAppPlaybackPath 是 App 取流端点。
//
// 【端点选择依据】2026-10-16 用完整签名（gorgon+khronos+argus+ladon）实测四个端点：
//
//	/novel/player/video_model/v1/               → 曾可用（data.video_model，单集）
//	/novel/player/multi_video_model/v1/         → 200 code:0（批量，按 video_id 索引）
//	/novel/player/multi_video_model/preload/v1/ → 200 code:0（批量预加载）
//	/novel/player/video_model/preload/v1/       → 404（**不存在**，勿照搬）
//
// 【2026-10-30 端点切换：单集 → 批量】单集端点已不再返回数据：用注册身份 + 不带任何
// 签名实测 HTTP 200 空 body，同身份同参数打批量端点 3/3 有数据——差异在端点，不在
// 身份或签名。参考实现 jackotom/hongguo-drama 的 fetchAppPlayUrl 也只用批量端点。
// 故主用批量，payload 与它同构（mixed_video_id_map，键 1004 = 内容类型编码）。
const hongguoAppPlaybackPath = "/novel/player/multi_video_model/v1/"

// hongguoAppPlaybackPayload 构造批量取流请求体（与参考实现 fetchAppPlayUrl 同构）。
func hongguoAppPlaybackPayload(videoID string) map[string]any {
	return map[string]any{
		"biz_param": map[string]any{
			"detail_page_version":       0,
			"device_level":              3,
			"disable_digg_stat":         false,
			"need_all_video_definition": true,
			"need_mp4_align":            false,
			"use_os_player":             false,
			"use_server_dns":            false,
			"video_platform":            1024,
		},
		"mixed_video_id_map": map[string]any{
			"1004": []string{videoID},
		},
	}
}

// hongguoAppModel 从批量响应里取本集的 video_model。
//
// 响应形如 {"data":{"<video_id>":{"video_model":{...}}}}；video_model 可能是对象，
// 也可能是 JSON 字符串（服务端两种形态都出现过）。
func hongguoAppModel(result map[string]any, videoID string) (map[string]any, error) {
	data := nestedMap(result, "data")
	entry := nestedMap(data, videoID)
	model := nestedMap(entry, "video_model")
	if encoded, ok := entry["video_model"].(string); ok {
		decoder := json.NewDecoder(strings.NewReader(encoded))
		decoder.UseNumber()
		if err := decoder.Decode(&model); err != nil {
			return nil, errors.New("红果 App 播放信息格式异常")
		}
	}
	if len(model) == 0 {
		// 批量端点也支持顶层 data.video_model（单集形态），作为兜底。
		model = nestedMap(data, "video_model")
	}
	if len(model) == 0 {
		return nil, errors.New("App 接口未返回所选分集")
	}
	return model, nil
}

func (downloader *Downloader) resolveHongguoAppMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("红果视频 ID 无效")
	}
	payload := hongguoAppPlaybackPayload(videoID)
	result, _, err := hongguoAnonPlayback(ctx, downloader, func(ctx context.Context, identity hongguoAnonDevice) (map[string]any, error) {
		return downloader.hongguoAppVideoModelAt(ctx, hongguoAppPlaybackPath, payload, identity)
	})
	if err != nil {
		return providerMedia{}, err
	}
	model, err := hongguoAppModel(result, videoID)
	if err != nil {
		return providerMedia{}, err
	}
	return selectHongguoAppMedia(model)
}

func selectHongguoAppMedia(model map[string]any) (providerMedia, error) {
	variants := anyList(model["video_list"])
	if rows, ok := model["video_list"].(map[string]any); ok && len(variants) == 0 {
		keys := make([]string, 0, len(rows))
		for key := range rows {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			variants = append(variants, rows[key])
		}
	}
	duration, _ := strconv.ParseFloat(mapString(model, "video_duration", "duration"), 64)
	var keyErr error
	type scoredMedia struct {
		media providerMedia
		score int
	}
	var choices []scoredMedia
	for _, row := range variants {
		variant, _ := row.(map[string]any)
		meta := nestedMap(variant, "video_meta")
		codec := strings.ToLower(mapString(meta, "codec_type"))
		if codec == "bytevc2" || strings.Contains(strings.ToLower(mapString(variant, "gear_des_key")), "bytevc2") {
			continue
		}
		addresses := hongguoMediaAddresses(variant)
		if len(addresses) == 0 {
			continue
		}
		media := providerMedia{Referer: "https://novel.snssdk.com/", Duration: time.Duration(duration * float64(time.Second))}
		encryption := nestedMap(variant, "encrypt_info")
		spade := mapString(encryption, "spade_a")
		if spade != "" || encryption["encrypt"] == true || mapString(encryption, "encryption_method") == "cenc-aes-ctr" {
			var err error
			media.CENCKey, err = hongguoContentKey(spade)
			if err != nil {
				keyErr = err
				continue
			}
		}
		height, _ := strconv.Atoi(mapString(meta, "vheight"))
		if definition, err := strconv.Atoi(hongguoQualityNumber.FindString(mapString(meta, "definition"))); err == nil && definition > 0 {
			height = definition
		} else if width, _ := strconv.Atoi(mapString(meta, "vwidth")); width > 0 && (height == 0 || width < height) {
			height = width
		}
		media.Quality = height
		quality := height * 10
		if codec == "h264" || codec == "avc1" {
			quality++
		}
		for _, address := range addresses {
			media.URL = address
			choices = append(choices, scoredMedia{media: media, score: quality})
		}
	}
	if len(choices) > 0 {
		sort.SliceStable(choices, func(i, j int) bool { return choices[i].score > choices[j].score })
		selected := choices[0].media
		for _, choice := range choices {
			selected.Variants = append(selected.Variants, choice.media)
		}
		return selected, nil
	}
	if keyErr != nil {
		return providerMedia{}, fmt.Errorf("红果 App 媒体密钥不可用: %w", keyErr)
	}
	return providerMedia{}, errors.New("红果 App 未返回兼容的媒体，已跳过不支持的编码")
}

func hongguoMediaAddresses(info map[string]any) []string {
	var addresses []string
	seen := map[string]bool{}
	var add func(any)
	add = func(value any) {
		switch value := value.(type) {
		case string:
			address := strings.TrimSpace(value)
			if len(address) > 8192 {
				return
			}
			if !isProviderHTTPMediaURL(address) {
				decoded, err := decodeHongguoBase64(address)
				if err != nil {
					return
				}
				address = strings.TrimSpace(string(decoded))
			}
			if isProviderHTTPMediaURL(address) && !seen[address] {
				seen[address] = true
				addresses = append(addresses, address)
			}
		case []any:
			for _, item := range value {
				add(item)
			}
		}
	}
	for _, key := range []string{"main_url", "backup_url", "backup_url_1", "backup_url_2", "backup_urls", "url_list"} {
		add(info[key])
	}
	return addresses
}
