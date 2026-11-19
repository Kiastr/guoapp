//============================================================================================================
//
//
//                  Copyright (c) 2023, Qualcomm Innovation Center, Inc. All rights reserved.
//                              SPDX-License-Identifier: BSD-3-Clause
//
//============================================================================================================

// Snapdragon Game Super Resolution (GSR) v1 by Qualcomm
// ported to mpv by agyild
//
// 来源（升级前先比对上游，勿盲改算法常数）：
//   - 算法与版权：Qualcomm Snapdragon GSR v1，BSD-3-Clause
//     https://github.com/qualcomm/SnapdragonGameSuperResolution
//     原始文件 sgsr/v1/include/glsl/sgsr1_shader_mobile.frag
//   - mpv 移植：agyild，2025-07-13 初版 / 2025-12-23 hotfix
//     （hotfix 修正非 Edge Direction 分支的 std 计算，此前输出偏糊）
//     https://gist.github.com/agyild/7715b6b1f38427839d95828f84902cab
//   - 同源参考移植：Magpie（2025-08）
//
// 本仓库改动（仅此一处，其余逐字保留上游移植）：
//   上游用 mpv user-shader parameters 块（PARAM / TYPE DEFINE 块）暴露三个
//   参数。本仓库全部现有 shader 均未使用该特性，且旧版 mpv 会把参数值行当作
//   shader 代码导致编译失败——为消除版本依赖，这里把官方默认值直接展开成
//   预处理宏。要调参改 hook 块下方（COMPONENTS 指令之后）的三个 #define：
//     UseEdgeDirection  1     方向感知上采样（关掉更快，边缘会糊）
//     EdgeThreshold     4.0   边缘判定阈值（1..16，越低细节越多也越慢）
//     EdgeSharpness     2.0   边缘锐化强度（1..2，过高会振铃）
//
// 【#define 位置是硬约束，不要挪回文件头】mpv 解析 user shader 时，把每个
// pass 的 body 定义为「HOOK 指令行之后」到下一个 HOOK 指令之间的内容；文件开头
// 到第一个 HOOK 指令之间的文本属于无 hook 的空 pass，**整段丢弃**。曾经把三个
// #define 写在文件头，结果宏没有进入编译单元，着色器报
//   error: `EdgeThreshold' undeclared
// 编译失败后 Android GLES 驱动仍会执行该 pass，输出整幅网格噪点、色彩发灰的
// 花屏（俗称「X 光」效果）。桌面 mpv 会打印 compile log 并可回退，移动端没有
// 控制台，只能靠肉眼看到花屏，因此这里写死注释防止回退。
//
// 【同样不能把指令标记写进注释】mpv 切分 pass 用的是子串查找（找三字符
// 前缀），不看是否在行首。注释里只要出现完整的三字符前缀 + 指令名，就会被
// 当成真的 header 去解析，报
//   Error while parsing COMPONENTS!
// 并整段拒绝该文件。描述这些指令时一律去掉那三个字符，只写指令名。
//
// 算法特征（选档依据）：单 pass、12 tap Lanczos 类上采样 + 边缘自适应锐化，
// 不做多 pass 神经网络重建，开销约为 Anime4K 极简档的五分之一；只处理亮度
// 平面（LUMA），色度交给 mpv 常规缩放；没有去压缩伪影能力，低码率片源的
// 块效应会被锐化放大——定位是真人/实拍内容的「便宜且自然」档，不替代动漫
// 神经网络档。
//============================================================================================================

//!HOOK LUMA
//!BIND HOOKED
//!DESC Snapdragon Game Super Resolution (GSR) v1
//!WHEN OUTPUT.w OUTPUT.h * HOOKED.w HOOKED.h * / 1.0 >
//!WIDTH OUTPUT.w
//!HEIGHT OUTPUT.h
//!COMPONENTS 1

// 三个宏必须在这里（HOOK 指令之后）。放到文件头会被 mpv 当作空 pass 丢弃，
// 导致 EdgeThreshold / EdgeSharpness 未声明、着色器编译失败、画面花屏。
#define UseEdgeDirection 1
#define EdgeThreshold 4.0
#define EdgeSharpness 2.0

// Shader code

float fastLanczos2(float x)
{
	float wA = x - 4.0;
	float wB = x * wA - wA;
	wA *= wA;
	return wB * wA;
}

#if (UseEdgeDirection == 1)
vec2 weightY(float dx, float dy, float c, vec3 data)
#else
vec2 weightY(float dx, float dy, float c, float data)
#endif
{
#if (UseEdgeDirection == 1)
	float std = data.x;
	vec2 dir = data.yz;

	float edgeDis = ((dx * dir.y) + (dy * dir.x));
	float x = fma(edgeDis * edgeDis, (clamp(c * c * std, 0.0, 1.0) * 0.7 - 1.0), (dx * dx + dy * dy));
#else
	float std = data;
	float x = fma((dx * dx + dy * dy), 0.55, clamp(abs(c) * std, 0.0, 1.0));
#endif

	float w = fastLanczos2(x);
	return vec2(w, w * c);
}

vec2 edgeDirection(vec4 left, vec4 right)
{
	vec2 delta;
	delta.x = (right.x - left.z) + (right.w - left.y);
	delta.y = (right.x - left.z) - (right.w - left.y);
	return delta * inversesqrt(dot(delta, delta) + 3.075740e-05);
}

vec4 hook()
{
	vec4 color = HOOKED_texOff(0);

	vec2 imgCoord = ((HOOKED_pos * HOOKED_size) + vec2(-0.5, 0.5));
	vec2 imgCoordPixel = floor(imgCoord);
	vec2 coord = (imgCoordPixel * HOOKED_pt);
	vec2 pl = (imgCoord + (-imgCoordPixel));
	vec4 left = HOOKED_gather(coord, 0);

	float edgeVote = abs(left.z - left.y) + abs(color.x - left.y)  + abs(color.x - left.z) ;
	if (edgeVote > (EdgeThreshold / 255))
	{
		coord.x += HOOKED_pt.x;

		vec4 right = HOOKED_gather(coord + vec2(HOOKED_pt.x, 0.0), 0);
		vec4 upDown;
		upDown.xy = HOOKED_gather(coord + vec2(0.0, -HOOKED_pt.y), 0).wz;
		upDown.zw  = HOOKED_gather(coord+ vec2(0.0, HOOKED_pt.y), 0).yx;

		float mean = (left.y + left.z + right.x + right.w) * 0.25;
		left -= vec4(mean);
		right -= vec4(mean);
		upDown -= vec4(mean);
		color.w = color.x - mean;

		float sum = dot(abs(left) + abs(right) + abs(upDown), vec4(1.0));

#if (UseEdgeDirection == 1)
		float sumMean = 1.014185e+01 / sum;
		float std = sumMean * sumMean;
		vec3 data = vec3(std, edgeDirection(left, right));
#else
		float std = 2.181818 / sum;
		float data = std;
#endif
		vec2 aWY  = weightY(pl.x,       pl.y + 1.0, upDown.x, data);
		     aWY += weightY(pl.x - 1.0, pl.y + 1.0, upDown.y, data);
		     aWY += weightY(pl.x - 1.0, pl.y - 2.0, upDown.z, data);
		     aWY += weightY(pl.x,       pl.y - 2.0, upDown.w, data);
		     aWY += weightY(pl.x + 1.0, pl.y - 1.0,   left.x, data);
		     aWY += weightY(pl.x,       pl.y - 1.0,   left.y, data);
		     aWY += weightY(pl.x,       pl.y,         left.z, data);
		     aWY += weightY(pl.x + 1.0, pl.y,         left.w, data);
		     aWY += weightY(pl.x - 1.0, pl.y - 1.0,  right.x, data);
		     aWY += weightY(pl.x - 2.0, pl.y - 1.0,  right.y, data);
		     aWY += weightY(pl.x - 2.0, pl.y,        right.z, data);
		     aWY += weightY(pl.x - 1.0, pl.y,        right.w, data);

		float finalY = aWY.y / aWY.x;
		float maxY = max(max(left.y, left.z), max(right.x, right.w));
		float minY = min(min(left.y, left.z), min(right.x, right.w));
		float deltaY = clamp(EdgeSharpness * finalY, minY, maxY) - color.w;

		//smooth high contrast input
		deltaY = clamp(deltaY, -23.0 / 255.0, 23.0 / 255.0);

		color.x = clamp((color.x + deltaY), 0.0, 1.0);
	}

	color.w = 1.0;

	return color;
}
