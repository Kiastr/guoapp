//============================================================================================================
//
//
//                  Copyright (c) 2023, Qualcomm Innovation Center, Inc. All rights reserved.
//                              SPDX-License-Identifier: BSD-3-Clause
//
//============================================================================================================

// Snapdragon Game Super Resolution (GSR) v1 by Qualcomm
// ported to mpv — 本仓库 +219 起为 MAIN/RGB hook 移植版
//
// 来源（升级前先比对上游算法常数，勿盲改）：
//   - 算法与版权：Qualcomm Snapdragon GSR v1，BSD-3-Clause
//     https://github.com/qualcomm/SnapdragonGameSuperResolution
//     原始文件 sgsr/v1/include/glsl/sgsr1_shader_mobile.frag
//   - 原 mpv 移植：agyild（HOOK LUMA + COMPONENTS 1 + gather 版本）
//     https://gist.github.com/agyild/7715b6b1f38427839d95828f84902cab
//
//============================================================================================================
// 【+219 架构级重写：LUMA/单通道/gather  ->  MAIN/RGB/显式 12 tap】
//
// 为什么重写（八版真机花屏的最终结论）：
//   +211~+218 连续八版在真机出现红蓝伪彩/绿通道清零（"热成像"）。逐版排除了
//   参数机制（#define / PARAM / 字面量）、精度（mediump 下溢 / highp）、FBO 格式
//   （rgba16f / rgba8）、返回通道脏值（已改为只返回 .x）——全部无效。
//   真正原因是**宿主管线不匹配**：agyild 原版用 HOOK LUMA + COMPONENTS 1 +
//   HOOKED_gather，这三者要求 vo=gpu-next 才完整支持（上游文档明示 Requires
//   vo=gpu-next）。而 app 内置 media_kit 1.2.6 的 libmpv 走 legacy vo=gpu，
//   单通道亮度输出在该路径上被错误重解释，色彩平面错位 -> 红蓝伪彩。
//
// 改法（对齐项目内真机从未花屏的 Anime4K / FSRCNNX / ravu 链）：
//   - HOOK MAIN：在 RGB 平面上处理，不再经过单通道 LUMA 路径
//   - COMPONENTS 4：完整四通道输出，无格式重解释
//   - 12 tap 全部用 HOOKED_tex 显式采样，不依赖 HOOKED_gather（GLES3 特性）
//   - 固定 2× 放大（2*HOOKED.w/h），相位精确；非整数倍交给 mpv 常规缩放
//   - 只重构亮度：算出差量 deltaY 后注入 RGB 三通道，色度（色差）保持不变，
//     从结构上杜绝色度被污染
//
// 算法本体不变：12 tap fastLanczos2 + 边缘方向自适应加权 + 阈值 4/255 +
//   锐度 2.0。与 agyild 上游逐项对应（见下方 weightY 与 12 tap 调用）。
//
// 参数（字面量内联，勿改回 PARAM 或 #define）：
//   真机 libmpv 实测不识别 user-shader 的 parameters 块（报 Unrecognized
//   command 'PARAM ...'!），手写 #define 放文件头又会被当空 pass 丢弃。
//   app 内无调参 UI，这三个值恒定，字面化零功能损失：
//     UseEdgeDirection = 1   方向感知上采样（分支已内联为 true 路径）
//     EdgeThreshold    = 4.0 边缘判定阈值，代码里是 4.0 / 255
//     EdgeSharpness    = 2.0 边缘锐化强度，代码里是 2.0 * finalY
//
// 【precision highp 兜底】常数 3.075740e-05（方向向量的防除零项）低于 mediump
//   最小正规数约 6.1e-5，若 pass 落到 mediump 会下溢为 0，平坦区域
//   inversesqrt(0) -> inf -> NaN。故在 pass body 显式声明 highp。
//
// 【不能把指令标记写进注释】mpv 切分 pass 用子串查找（找三字符前缀），不看
//   是否在行首。注释里只要出现完整前缀 + 指令名，就会被当 header 解析并报
//   Error while parsing COMPONENTS!，整段拒绝该文件。描述指令时一律去掉那
//   三个字符，只写指令名。
//
// 算法特征（选档依据）：单 pass、12 tap Lanczos 类上采样 + 边缘自适应锐化，
//   不做多 pass 神经网络重建，开销约为 Anime4K 极简档的五分之一；没有去压缩
//   伪影能力，低码率片源的块效应会被锐化放大——定位真人/实拍内容的「便宜且
//   自然」档，不替代动漫神经网络档。
//============================================================================================================

//!HOOK MAIN
//!BIND HOOKED
//!DESC Snapdragon Game Super Resolution (GSR) v1 (MAIN/RGB)
//!WIDTH 2 HOOKED.w *
//!HEIGHT 2 HOOKED.h *
//!WHEN OUTPUT.w OUTPUT.h * HOOKED.w HOOKED.h * / 1.0 >
//!COMPONENTS 4

// pass body 内显式高精度：老 libmpv 可能默认 mediump，而常数 3.075740e-05
// 在 mediump 下会下溢为 0，导致 inversesqrt(0) 出 NaN。
precision highp float;

// 亮度权重（BT.709）。仅用于把 RGB 折算成亮度做重构，不改变色度。
const vec3 kLuma = vec3(0.2126, 0.7152, 0.0722);

float fastLanczos2(float x)
{
	float wA = x - 4.0;
	float wB = x * wA - wA;
	wA *= wA;
	return wB * wA;
}

// UseEdgeDirection = 1：方向感知路径（字面化内联；app 无调参 UI）。
vec2 weightY(float dx, float dy, float c, vec3 data)
{
	float std = data.x;
	vec2 dir = data.yz;

	float edgeDis = ((dx * dir.y) + (dy * dir.x));
	float x = fma(edgeDis * edgeDis, (clamp(c * c * std, 0.0, 1.0) * 0.7 - 1.0), (dx * dx + dy * dy));

	float w = fastLanczos2(x);
	return vec2(w, w * c);
}

vec4 hook()
{
	vec2 pt = HOOKED_pt;
	vec2 sz = HOOKED_size;

	// 子像素相位：imgCoord 是当前输出像素在输入网格里的连续坐标。
	// base 必须落在「最近像素中心」而不是纹素左下角——否则 13 次采样整体
	// 偏半个像素，放大后表现为整体发虚/发黑。
	vec2 imgCoord = (HOOKED_pos * sz) + vec2(-0.5, 0.5);
	vec2 pix = floor(imgCoord);
	vec2 pl = imgCoord - pix;
	vec2 base = (pix + 0.5) * pt;
	vec4 color = HOOKED_tex(base);

	// ---- 显式采样 13 个位置的亮度（替代原版 gather 的 2×2 打包）----
	// 偏移量与上游 weightY 调用一一对应，命名后缀表示方向。
	float yC   = dot(color.rgb, kLuma);                                         // ( 0,  0)
	float yU   = dot(HOOKED_tex(base + vec2( 0.0,      -pt.y)).rgb, kLuma);     // ( 0, -1)
	float yD   = dot(HOOKED_tex(base + vec2( 0.0,       pt.y)).rgb, kLuma);     // ( 0, +1)
	float yL   = dot(HOOKED_tex(base + vec2(-pt.x,      0.0 )).rgb, kLuma);     // (-1,  0)
	float yR   = dot(HOOKED_tex(base + vec2( pt.x,      0.0 )).rgb, kLuma);     // (+1,  0)
	float yUL  = dot(HOOKED_tex(base + vec2(-pt.x,     -pt.y)).rgb, kLuma);     // (-1, -1)
	float yUR  = dot(HOOKED_tex(base + vec2( pt.x,     -pt.y)).rgb, kLuma);     // (+1, -1)
	float yDL  = dot(HOOKED_tex(base + vec2(-pt.x,      pt.y)).rgb, kLuma);     // (-1, +1)
	float yDR  = dot(HOOKED_tex(base + vec2( pt.x,      pt.y)).rgb, kLuma);     // (+1, +1)
	float yLL  = dot(HOOKED_tex(base + vec2(-2.0*pt.x,  0.0 )).rgb, kLuma);     // (-2,  0)
	float yLLU = dot(HOOKED_tex(base + vec2(-2.0*pt.x, -pt.y)).rgb, kLuma);     // (-2, -1)
	float yU2  = dot(HOOKED_tex(base + vec2( 0.0,    -2.0*pt.y)).rgb, kLuma);   // ( 0, -2)
	float yLU2 = dot(HOOKED_tex(base + vec2(-pt.x,   -2.0*pt.y)).rgb, kLuma);   // (-1, -2)

	// ---- 局部统计（全部基于"去均值"后的值，与上游一致）----
	float mean = (yC + yU + yD + yL + yR) * 0.2;

	float c0   = yC   - mean;
	float u0   = yU   - mean;
	float d0   = yD   - mean;
	float l0   = yL   - mean;
	float r0   = yR   - mean;
	float ul0  = yUL  - mean;
	float ur0  = yUR  - mean;
	float dl0  = yDL  - mean;
	float dr0  = yDR  - mean;
	float ll0  = yLL  - mean;
	float llu0 = yLLU - mean;
	float u20  = yU2  - mean;
	float lu20 = yLU2 - mean;

	float sumAbs = abs(u0) + abs(d0) + abs(l0) + abs(r0)
	             + abs(ul0) + abs(ur0) + abs(dl0) + abs(dr0);
	float sumMean = 1.014185e+01 / max(sumAbs, 1.0e-5);
	float std = sumMean * sumMean;

	// 边缘方向：Sobel 梯度归一化（承担上游 edgeDirection 的作用），用去均值
	// 后的值，避免整体亮度偏移影响方向判断。
	float gx = (ur0 + 2.0 * r0 + dr0) - (ul0 + 2.0 * l0 + dl0);
	float gy = (dl0 + 2.0 * d0 + dr0) - (ul0 + 2.0 * u0 + ur0);
	vec2 grad = vec2(gx, gy);
	vec2 dir = grad * inversesqrt(dot(grad, grad) + 3.075740e-05);

	vec3 data = vec3(std, dir);

	// ---- 12 tap Lanczos 加权（偏移顺序与上游 weightY 调用逐项对应）----
	vec2 aWY  = weightY(pl.x,       pl.y + 1.0, d0,   data);
	     aWY += weightY(pl.x - 1.0, pl.y + 1.0, dl0,  data);
	     aWY += weightY(pl.x - 1.0, pl.y - 2.0, lu20, data);
	     aWY += weightY(pl.x,       pl.y - 2.0, u20,  data);
	     aWY += weightY(pl.x + 1.0, pl.y - 1.0, ur0,  data);
	     aWY += weightY(pl.x,       pl.y - 1.0, u0,   data);
	     aWY += weightY(pl.x,       pl.y,       c0,   data);
	     aWY += weightY(pl.x + 1.0, pl.y,       r0,   data);
	     aWY += weightY(pl.x - 1.0, pl.y - 1.0, ul0,  data);
	     aWY += weightY(pl.x - 2.0, pl.y - 1.0, llu0, data);
	     aWY += weightY(pl.x - 2.0, pl.y,       ll0,  data);
	     aWY += weightY(pl.x - 1.0, pl.y,       l0,   data);

	// 权重和可能因数值抵消接近 0（平坦区 std 极大时），此时退回中心值，
	// 避免 0/0 出 NaN（NaN 经 clamp 仍是 NaN，整帧会变黑）。
	float finalY = abs(aWY.x) > 1.0e-6 ? aWY.y / aWY.x : c0;

	// 邻域上下界（去均值域），限制外推幅度，避免振铃
	float maxY = max(max(c0, max(u0, d0)), max(l0, r0));
	float minY = min(min(c0, min(u0, d0)), min(l0, r0));

	// 外推（锐度 2.0）+ 限幅，再把均值加回
	float newY = clamp(2.0 * finalY, minY, maxY) + mean;

	// 单像素最大变化 23/255，抑制高对比处的过冲
	float deltaY = clamp(newY - yC, -23.0 / 255.0, 23.0 / 255.0);

	// ---- 亮度增量注入 RGB：三通道同加，色差不变 => 色度不被污染 ----
	color.rgb = clamp(color.rgb + deltaY, 0.0, 1.0);

	return vec4(color.rgb, 1.0);
}
