# 红果鉴 / 真果鉴

> ⚠️ **免责声明**：本项目源码来自网上大名鼎鼎的**鱼佬**（原作者）。我只是把它拿来打包、测试着玩，方便自己用，**不保证任何可用性，随时可能删库**。

Flutter 多端独立短剧 / 影视应用，原名「短剧库 APP」。站源请求、解析、下载和播放均在设备上完成，不依赖自建服务。当前源码版本：**0.2.77+196**。

如要使用多片源请使用真果鉴

## 在原项目基础上的主要升级

> 原项目为**鱼佬的 0.2.50 基线「短剧库 APP」**；本仓库在其源码之上持续升级至 **0.2.77+196**。主要改动按主题精炼如下，版本里程碑见下方。

### 1. 运行时片源生态——支持纯用户端解析libretv需要后端解析的视频源（重要改动）

- **仅安装前端app即可利用机器算力解析以下视频源，无需服务器解析**
- **LibreTV 片源导入**：订阅配置链接 / 配置 JSON / **Base58 社区订阅** / MacCMS 采集地址（JSON 与 XML）粘入即用，运行期生效，不需要自建后端
- **drpy 脚本源**：本机 goja 虚拟机执行 TVBox drpy2 脚本，浏览 / 搜索 / 详情 / 播放全链路；兼容只写 `rule` 配置的 **rule 驱动型脚本**
- **CatVod 蜘蛛**：自动下载并转换 `lib/cat.js`，跑通社区第二种 JS 方言（ESM + Node 垫片）
- **Kazumi 动漫规则源（+155）**：Go 解释器直接跑官方规则库（Predidit/KazumiRules）的 **api 模式**规则——JSON 描述「怎么搜、怎么取分集」，不跑 JS、不引 JVM、不需要 XPath 求值器；出厂内置 xfdmnext 与 giriGiriLove 两条，站源管理页可继续粘贴规则或**从官方规则库在线挑选**导入
- **四种播放解析**：直链嗅探 / JSON 解析接口 / **本地解析脚本**（goja 执行 `parse(url)`，纯 Go 无后端）/ **WebView 嗅探**（无界面浏览器拦截媒体请求）
- **片源精细化管理**：收藏（独占一组）/ 隐藏（敏感源同逻辑）/ 下拉刷新与分类面板；**+124 起随 WebDAV 跨设备同步**
- **站源分组与默认收藏（+155 / +156 / +157）**：内置源按主题合并为「动漫源集合」（青空 + Kazumi 规则源）与「预制源集合」（韩小圈 + 鬼片），LibreTV 片源与 drpy 脚本源分别归入「扩展片源集合」「drpy 脚本源集合」；**收藏的源在首页独占一组与红果同级，同时在所属分组里保留一份**（+156），点星即可取消收藏；**出厂只收藏青空，韩小圈默认回到预制源集合**（+163）；**分组顺序显式定序且可在站源管理页上移 / 下移**（+157）

### 2. 播放体验

- **清晰度自动检测**：按解码后真实像素折算 480P / 1080P / 2K / 4K，菜单内「节点进度条」展示，换集换线自动重测
- **线路记忆与自动回退**：记住起播线路（15 分钟有效），失败按环形自动切线，全部试过才重新解析
- **手势与倍速**：左右半屏横拖调进度、右半屏竖拖切集；长按倍速 **7 档**（1.5x ~ 5x），提示改顶部紧凑胶囊不挡画面
- **有声书播放修复**：`.m4a` / `.mp3` 不再被误判为不可播放

### 3. 多设备同步

- **WebDAV 同步**：坚果云 / Nextcloud / Alist 等，跨网络同步追剧、观看状态与续播进度（版本向量合并、冲突双版本保留）
- **+124 扩展到片源**：同步已导入片源与收藏 / 隐藏状态；取消可传播（墓碑记录），对端片源**自动导入**（幂等）
- **局域网互联**：mDNS 自动发现、证书指纹配对、播放进度实时续播

### 4. 画质增强体系

- 重构为**自动族**（省电增强 / 清晰优先）+ **高级六档**（极简 / 极简超分 / 均衡 / 标准 / 轻量 / 质量，按负载与效果取舍，弱 GPU 设备也能开启）
- 全部 GPU 着色器执行，不用 NNAPI / NPU；修复 shader 注释被 mpv 误判导致的**安卓端增强静默空转**
- **安卓端固定输出表面（+140，开发快照，未真机验证）**：fork `media_kit_video` 新增 `VideoFixedSurface` 策略，播放器创建时一次性把渲染表面放大到「源尺寸 × 倍率」（1080p → 2560×1440）并锁定，让 Anime4K 的 Upscale pass 真正参与渲染——这是安卓端从"同分辨率重建/降噪"走向 **1080p→2K 真超分**的唯一路径。默认不启用；命中条件（安卓 / 非电视 / 增强已开 / libmpv 后端 / 自建 VideoController）全部满足才开启，桌面行为与上游完全一致。详见下方 +140 条目与 `lib/video_enhancement_platform.dart` 注释
- **平台分流收口（+177）**：超分的平台差异全部收进 `lib/video_enhancement_platform.dart` —— Windows / macOS / Linux 走桌面实现（`VideoController.setSize` 放大表面，Windows 另有 `d3d11vpp` 硬件滤镜、成本上限 4），安卓走 `AndroidVideoEnhancementPlatform`（表面 no-op，固定表面在创建 VideoController 时锁定；成本上限 3；详情只给「已执行超分降噪」）。控制器不再自己判断 `Platform`。

### 5. 细节与性能

- **分类面板拼音首字母排序**：中文按拼音排（爱情 AI < 短剧 DJ < 动作 DZ），运行时零依赖
- **卡顿修复**：轮询状态签名去重 + 首页分组缓存，消除周期性整页重建
- **错误提示人话化**：接口返回网页、缺少函数、jar 包源等分别给出具体原因，不再统一显示「类型暂不支持」

## 版本里程碑

只记录**改变用户用法**的大版本：新增站源、新增播放能力、交互方式变化。
修复、优化、单源适配、参数调整这类改动**不单列**——它们写在 Git 提交信息里，`git log` 比这张表清楚得多。加行之前先问一句：用户会因为这个版本改变操作习惯吗？

| 版本 | 变更 |
| --- | --- |
| +194 | 首页搜索框改为随滚动方向自动收起／展开：向上滑动看剧集时收起，向下滑动或回到顶部时重新出现；品牌头与分类标签始终常驻，输入时搜索框不被收起 |
| +192 | 首页品牌头恢复「发现好剧」，站源名降为下方小字并点开切换；向下滑动时「继续观看」横卡与「为你推荐」标题行随内容收起，仅站源栏、搜索框与分类标签常驻 |
| +191 | 青空次元上游要求登录、匿名取流不可用，暂时从片源列表停用；源定义与本地收藏／观看记录均保留，上游恢复后可重新启用 |
| +190 | 首页信息架构对齐：品牌头保持站源名并点开切换站源、搜索框常驻、新增「继续观看」横卡与「为你推荐 / 换一批」标题行；红果推荐的三个档位从第二行筛选收编为标题行里的文字按钮菜单，其他站源不再出现双行筛选 |
| +188 | 「A · 影院」视觉：深色优先的近黑底与纯海报网格；底部导航四个入口改用同一套线性图标，选中只变颜色与圆点（不再 outline→filled 跳变）；播放器控制层改为上下渐变遮罩、圆形主播放键与手机端工具行均布 |
| +185 | 站源主页多选支持批量追剧／标记／清理；设置页按用途分组并补齐播放偏好与站点验证入口；播放器工具行收敛为常驻项 +「更多」 |
| +182 | 站点验证通用骨架：内置浏览器通过 Cloudflare 挑战，凭据按源存本机；Han1me 接入 |
| +181 | 移植 Han1meViewer 的 Han1me 源：分类、6 个排行榜、系列分集与多清晰度；作敏感站源默认隐藏 |
| +180 | 推荐默认改为「个性推荐」（两段式取流），原账号 Cookie 档更名「账号推荐（实验）」 |
| +178 / +179 | 推荐 tag 三档：公开推荐、账号推荐、两段式信息流与曝光回传；红果源默认打开推荐 |
| +176 / +177 | 新增独立 Windows 构建工作流；画质增强按平台分流，修复 Windows「阶段:resource」；跨设备收藏顺序修正 |
| +172 | 红果账号同步：收藏与观看记录 |
| +155 | Kazumi 动漫规则源、站源重分组与默认收藏 |
| +149 / +150 | 红果官网扫码登录；剧库分类兜底、翻页游标与推荐流修复 |
| +140 ~ +145 | 安卓端固定输出表面，1080p→2K 真超分与画质分档 |
| +101 ~ +130 | LibreTV / drpy / CatVod / 规则型 Spider 运行时片源生态 |
| +91 | WebDAV 跨设备同步 |
| +83 | 画质增强体系重构 |

> 说明：应用长期按「只维护源码与定向测试，集中验证另行安排」的方式推进。未完成集中验证与真机验收的项目保持未验收状态；历史版本的检查记录不能作为后续新增功能的验收结论。

| 编译方式 | 应用名称 | 可用站源 |
| --- | --- | --- |
| 默认（不加参数） | 红果鉴 | 仅红果 |
| 加 `--all-sources` | 真果鉴 | 红果、韩小圈、鬼片、青空，以及黄豆、剧果、野果、帝果、黄果视频、黄果 AI、黄果旧版、Han1me |

默认可见站源（红果、韩小圈、鬼片、青空）无需密码；其余站源（含 +181 的 Han1me）默认隐藏，在站源管理里通过密码锁解锁后显示。已有受限用户不会自动获得新增站源权限，管理员可在用户管理中勾选。

这是**编译选项**，应用内不能切换版本。标题、Android 桌面名称与电视横幅、Windows 窗口与分发文件名、iOS 显示名随编译选项变化。界面、站源调用、原生下载调度同时限制可用站源；红果版不会访问或继续执行其他站源的旧任务。

两版保留原 Android / iOS 应用标识及数据目录；Android 使用同一签名可相互覆盖升级，不能作为两个独立正式应用并排安装。切换版本保留追剧、观看记录、用户权限和下载记录；备份格式保持兼容。

## 使用

| 功能 | 操作 |
| --- | --- |
| 浏览 | 标题栏依次提供排序与筛选、榜单、多选和展开搜索。分类保持独立一行，内容区左右滑动切类；支持默认、名称、自然季号、上线日期、热度、播放量排序及连载状态筛选。列表下滑接近底部自动加载下一页，底部「加载更多」为兜底 |
| 搜索 | 红果合并官网与名称索引，输入停顿 300 毫秒显示最多 10 条联想；韩小圈、剧果、野果、鬼片调用各自的在线搜索并按分页继续加载。部分失败保留有效结果；最近 20 次搜索按用户保存 |
| 推荐 | 红果分类栏在「全部」后显示「推荐」，且为红果源默认打开项；内含三档：「个性推荐」（两段式取流 + 行为回传，默认，无需登录）、「公开推荐」（匿名）、「账号推荐（实验）」（带红果账号 Cookie，效果未验证），点「继续推荐」或下拉换一批 |
| 详情 | 普通点剧直接进入播放；下载和兜底入口保留详情页。详情页浏览封面、资料、追剧状态与可展开简介；底部固定「立即播放 / 继续播放」和下载入口。选集默认折叠，展开后每组 50 集，可切换范围、定位当前或输入集数跳转；宽屏与电视采用资料、选集分栏 |
| 追剧与历史 | 按想看 / 在看 / 已看 / 有更新筛选；红果已追剧发现新季后进入「有更新」。追剧和历史均可搜索。每个用户独立保存，每 5 秒及退出播放时记录真实进度 |
| 卡片与批量操作 | 卡片「更多」提供收藏、状态和下载选集；电脑右键，电视菜单键。发现页长按卡片或点标题栏「多选」，一次最多 50 部；选中后底部「批量操作」可批量加入／取消追剧、标记想看／在看／已看、标记更新已读、收藏／取消收藏涉及站源、删除观看记录；有下载权限时另有「下一步」进入批量下载。多选不再要求下载权限 |
| VIP | 全站源版浏览黄豆或全部站源时显示 VIP 图标，仅过滤黄豆；默认隐藏已确认的 VIP。未知、免费、VIP 分别保存，未知不会覆盖已知状态 |
| 手机播放 | 上滑下一集、下滑上一集；**左右半屏横向拖动都是进度调整**，左半屏竖向滑动调亮度，右半屏竖向滑动切集。长按 350 毫秒临时加速到「长按倍速」（默认 3 倍，播放菜单「倍速」页可选 1.5 / 1.75 / 2 / 2.5 / 3 / 4 / 5 倍，松开恢复）。轻点显示或隐藏控制条，双击播放 / 暂停。底部工具行常驻倍速、清晰度、选集、播放设置，其余（推送、原画对比、音量、画面填充、画中画）收进「更多」；「播放设置」里可改弹幕、自动连播、下一集预加载、追剧与操作说明。视频下方为「选集 / 简介 / 下载」Tab |
| 画中画 | Android 手机 / 平板播放器接入画中画按钮；进入前隐藏自绘控制层与弹幕，小窗只保留画面。Android TV、Windows、iOS 暂未接入 |
| 画质增强 | 关闭 / 自动（实验性）/ 省电增强 / 清晰优先 / 高级。高级模式可选三档：极简（负载最低，弱 GPU 也能跑）、轻量（默认）、质量（效果最好，负载最高）。全部在 GPU 上以着色器执行，不使用 NNAPI / NPU |
| 弹幕 | 红果在线播放默认开启；播放器侧边圆形「弹」字按钮可关闭、查看状态及失败重试。本地播放不加载弹幕 |
| 下载 | 播放页「下载」Tab 和详情页下载入口均可选择分集与画质；支持批量暂停、继续、重试、删除及清理任务但保留视频。「更新本剧」补新增或缺失分集 |
| 更多 | 站源管理、用户管理、设置与备份、界面模式、关于 |
| 设置 | 按「外观与交互 / 播放 / 内容源 / 下载与存储 / 账号与同步 / 高级与维护」分组。播放偏好、画质增强、界面模式、站点验证、关于均可在此找到；站点验证不再要求管理员权限。播放类偏好与播放器「播放设置」改的是同一份数据 |
| 外观 | 「A · 影院」视觉以深色为基准（近黑底、纯海报、更多留白），浅色为派生；底部导航四个入口改用同一套线性图标，选中时只改变颜色与状态圆点、不再切换图形；播放器画面上下使用渐变遮罩，主播放键为圆形实心按钮，手机工具行按宽度均布。在「设置 → 外观主题」切换明暗 |

| 站源 | 浏览与播放 | 搜索 |
| --- | --- | --- |
| 红果 | 真人剧、漫剧、AI 剧及分集 | 联网搜索与官网搜索联想 |
| 韩小圈 | MacCMS 模板：最新韩剧 / 韩国电影 / 韩国综艺 / 韩国动漫，多线路分集 | 站源在线搜索 |
| 鬼片 | MacCMS 站点：鬼片 / 电视剧 / 动漫，多线路分集 | RSS 最新条目标题匹配（站点搜索已停用） |
| 青空 | 番剧、剧场动画、特摄及分集 | 站源在线搜索 |
| 黄豆 | 列表、VIP 标记及分集 | 筛选已加载短剧 |
| 剧果 | 热门、最新、接口分类、详情及可播放分集；签名 Cookie 接入在线播放、预加载和下载 | 站源在线搜索，支持继续加载 |
| 野果 | 接口实际分类、目录、详情及真实分集；按分集重新取流，保留 H.264 / H.265 地址 | 站源在线分页搜索 |
| 帝果 | 网页分类、目录、详情及分集；vplayer 签名解析 | 站源在线分页搜索 |
| 黄果视频 / 黄果 AI / 黄果旧版 | 列表、分类、详情及分集 | 筛选已加载短剧 |
| Han1me（+181，默认隐藏） | 9 个分类、系列播放列表映射成分集、多清晰度线路；6 个站点排行榜（本日 / 本週 / 本月 / 观看次数 / 点赞比例 / 他们也在看）；浏览器 TLS 指纹 + 站点验证过 Cloudflare | 站源在线分页搜索 |

站源可用性、清晰度和区域限制取决于源站及网络；应用不解除源站 VIP 或其他授权限制。

Han1me 的解析可用 `cd native && CHECK_LIVE_PROVIDERS=true go test ./core -run TestLiveHanime -v` 在能访问该站点的机器上做联网冒烟（列表 / 榜单 / 详情 / 取流）。

Han1me 被 Cloudflare 拦截时，先看错误文案区分两种情况：

- **「当前节点 IP 被 Cloudflare 封禁（Error 1020）」**：站点把整个节点段拉黑了，没有挑战页可解，站点验证救不了。只能换节点——日本节点基本必封，优先新加坡 / 香港 / 台湾。
- **「站点要求浏览器验证」**：站源主页的错误面板会直接给一个「去站点验证」按钮，点进去过一次挑战即可（也可以走「设置 → 内容源 → 站点验证」）。Windows 端没有内置浏览器，该入口退化为粘贴 Cookie。凭据只存本机（0600）、不进配置备份，换代理节点或换域名后需要重新验证。

反复挑战、点很多次也过不了的，按这个顺序排查：

1. **节点 IP 信誉**（占绝大多数）。机场用的数据中心 IP 被大量滥用，Cloudflare 直接判高风险，Turnstile 会一轮接一轮地给新题，点多少次都是同一个结果。判别方法：同一个节点用手机 Chrome App（不是 App 内置浏览器）打开站点，如果同样无限挑战，就是节点问题，换节点，代码救不了。
2. **WebView 内核太旧**。Chrome < 100 跑不动 Turnstile 的 WASM。验证页顶部会直接报出内核版本，去应用商店更新「Android System WebView」。
3. **出口 IP 不一致**。App 走代理、内置浏览器走系统网络时，浏览器拿到的 `cf_clearance` 绑的是另一个 IP，脚本一用就废。保存后的自检失败就是这个症状。
4. **设备时间偏差**超过几秒，Turnstile 的 token 校验必失败。

三个实现前提，改代码前务必守住：

1. **请求必须走浏览器指纹通道**。裸 Go 的 ClientHello（Ja3/Ja4）是公布过的非浏览器指纹，Cloudflare 对 `hanime1.me` 直接回 1020 封锁页而不是挑战页——没有挑战页就永远拿不到 `cf_clearance`，站点验证这条路是死的。所以 Han1me 登记在 `browserFingerprintSources`，由 tls-client 用 `Chrome_150` profile 发请求。
2. **UA 必须跟随验证页的真实浏览器，不能伪造桌面身份**。Android WebView 的 JS 环境（`navigator.userAgentData.platform`、触摸点、屏幕尺寸）不会因为改了 UA 头就变成桌面，把 UA 头写成 Windows 桌面 Chrome 等于自己举报自己，Turnstile 只会无限挑战。现在的做法：验证页从 WebView 读真实 UA，去掉 `; wv` 与 `Version/4.0` 两个内嵌标记，用它加载页面；验证成功后把这串 UA 一起存进凭据，Go 侧按它发请求（`browserFingerprintSources` 只提供兜底身份）。
3. **UA / Client Hints / TLS 要自洽**。Go 侧解析 UA 里的平台与 Chrome 主版本，决定 `sec-ch-ua-platform`、`sec-ch-ua-mobile`、`sec-ch-ua` 的取值：桌面 UA 配 `?0`，Android UA 配 `?1`。改了 UA 却忘了改 Client Hints，等于换个角度暴露自己。

验证页会实时显示「已获取 cf_clearance」；没拿到时先警告一次，确认页面已能正常浏览可再点「完成」强制保存（部分节点放行后只下发 `__cf_bm`）。保存后会自动跑一次自检——用刚存的凭据真发一个请求，把「浏览器里过了、脚本仍然 403」当场指出来，并按状态码区分 1020 封禁 / 挑战未解 / 429 限流。

### 线路记忆与自动回退

播放器会记住每部剧上次成功起播的线路，切集时直接从该线路开始，减少「先试一条慢线」的等待（纯本地记录，不额外发请求）。记忆 **15 分钟**后失效，过期后回到首选线路重新试；起播 3 秒（预加载已验证的线路 6 秒）没有画面或播放出错时，自动切到同集的下一条线路，最后一条之后回到首选线路（环形），避免停留在备选线上永远回不到默认线。全部线路都试过仍失败才重新解析，重新解析前会清掉线路记忆、从默认线路开始；连续失败达到上限则停止自动恢复，提示重试或换集。

取流本身也是多线路按序回退：drpys 真签名 → App 接口（需登录，未登录时跳过）→ 网页播放页 → 备用接口。每条线路的成败都会记账，**连续失败 3 次进入冷却**，冷却期内直接跳过而不是每次都白试一遍；冷却到期只放行一次试探，成功才恢复。失败按类型分档：网络抖动 3 分钟、签名被拒／响应改版 30 分钟、其他 5 分钟。**已知停用的线路静态禁用**（当前为备用接口 djapi，上游域名已不可达），不再发起请求——它不会自愈，每次白试都要额外等一次握手超时；表项保留，上游复活时改一个字段即可恢复。

> 2026-11-11 起红果服务端对**播放模型接口**开始校验真签名：占位签名会被静默拒绝（HTTP 200、0 字节），而目录、详情、注册接口全部正常，很容易误判成「站点挂了」。本项目已按上游修法改用 fqapp 真 551 Argus/Ladon 签名，**只作用于播放模型请求**，其余接口的签名策略不变。签名常量集中在 `native/core/provider_hongguo_playback_sign.go`，开关 `hongguoPlaybackRealSignature` 可一键退回旧占位签名用于对照排查。

### LibreTV 片源：导入订阅配置，兼容 MacCMS 采集站与社区订阅

站源管理顶部提供「LibreTV 片源」卡片，可把 LibreTV 订阅配置、社区工具导出的 Base58 订阅，或单个 MacCMS（苹果 CMS）采集地址导入为本机片源，导入后即可像内置站源一样浏览、搜索与播放，不需要自建服务。

- **入口**：「站源管理 → LibreTV 片源 → 导入片源」。导入 / 删除仅管理员可操作；LibreTV 片源不占用敏感站源密码锁——导入即授权，不受隐藏源密码门控（多用户配置里的用户级站源权限仍然生效）。
- **四种输入形态**：
  - 订阅配置链接：返回 `{"sites":[{"key","name","type","api"}]}` 的 JSON 地址（兼容 `var config = {...};` 的 JS 包裹）；
  - 配置 JSON 原文：直接粘贴上述结构；
  - **Base58 订阅文本**：LunaTV-config 等社区工具导出的编码串（含换行也可），解码后即为上述 JSON；
  - 裸采集地址：形如 `https://.../provide/vod` 的 JSON 接口，或 `https://.../at/xml/` 的 XML 接口，按单站点导入。
- **兼容范围**：
  - `type` 为 0 / 1 或缺省：MacCMS provide/vod **JSON** 采集源，覆盖 `?ac=detail` 列表与分页、`?ac=list` 分类、`wd=` 在线搜索、`$$$` 多播放源与 `#` 分集；
  - `type` 为 2：MacCMS **XML** 采集源（`/at/xml/`），响应为 `<rss><list><video>` 结构，分类取同响应的 `<class><ty>`，分集取多个 `<dd flag="源名">`，字段与 JSON 版一致（含评分 / 主演 / 导演 / 简介）；
  - `type ≥ 3` 的 drpy / spider 脚本源：优先取 **`ext` 里的 `.js` 站点脚本**收成 **drpy 候选**（TVBox 约定 `api` 是 drpy 运行时库 `drpy2.min.js`，`ext` 才是站点脚本；只有 `api` 也是脚本的 drpy-node 变体才回退用 `api`），`drpy.min.js / drpy2.min.js / drpy2.js` 这类运行时地址不会成为候选。导入完成后弹出候选框，用户确认后逐个或一次性导入，详见下方「drpy 脚本源」小节；其余情况会按条目内容给出具体跳过原因：

| type=3 的形态 | 结果 | 提示原因 |
| --- | --- | --- |
| drpy 脚本（ext 为 `.js`，或 api 直接是脚本） | 收成候选，可导入 | — |
| **CatVod 蜘蛛**（`api` / `ext` 是 `./cat/xxx_open.js` 这类相对路径） | **收成候选**（按订阅地址补全后导入，自动下载 `lib/cat.js`） | — |
| **type=4 服务器源**（T4 / CatVod / drpy-node，api 是 vod 接口） | **按 JSON 采集站导入** | — |
| **`csp_AppYsV2`**（苹果CMS 二开 `/v1.vod`，接口地址在 ext） | **按 JSON 采集站导入** | — |
| api 只是 drpy 运行时库（`drpy.min.js` / `drpy2.min.js` / `drpy2.js`）且无 ext 脚本 | 跳过 | 只有 drpy 运行时库（api），缺少站点脚本（ext） |
| **`csp_XBPQ` / `csp_XYQHiker` / `csp_Bili`**（规则型 Spider） | **按规则源导入**（ext 为规则地址或内联规则） | — |
| `csp_Jpys` / `csp_Dm84` 等其余内置 Java Spider | 跳过 | **点名具体 Spider**：`csp_Jpys 是 TVBox 内置 Java Spider，暂不支持` |
| `jar` / spider 包 | 跳过 | jar / spider 包源暂不支持（仅支持 drpy JS 脚本） |
| `py` 源 | 跳过 | type=3 类型暂不支持（仅支持 provide/vod 采集接口与 drpy 脚本） |
  - 站点类型未标注（例如裸地址）时按响应内容自动回退：JSON 解析失败会再按 XML 解析，反之亦然，避免因类型标错整站不可用。
- **社区配置工具（LunaTV-config 等）**：这类工具会预先筛掉失效源、可选地给 `api` 加代理前缀，并输出多种订阅格式——`format=0` 原始 JSON、`format=1` 代理前缀 JSON、`format=2` 原始 JSON 的 Base58、`format=3` 代理前缀 JSON 的 Base58。把 `format=2 / format=3` 的链接或文件内容直接粘进来即可：客户端只做 Base58 解码 + JSON 解析，**不需要实时解析 type≥3 脚本，也不需要自建后端**。编码沿用 bs58 / Bitcoin 字母表，无校验和。
- **导入探测**：导入即逐站探测（总预算 15 秒、单站 5 秒），探测成功的站点在卡片上直接显示缓存剧集数；探测失败或超出预算的站点同样保留，之后在片源卡片上「更新 / 检测连接与播放」即可重试，不会一次网络抖动整批丢弃。
- **播放解析（parses）**：订阅里的 `parses` 会一并导入（保留 name / type / url / ext），站源管理「播放解析方式」可选择处理方式，默认**直链 / 嗅探**（不需要后端）。解析只对**非直链播放页**生效：采集站返回的 m3u8 / mp4 直链永远直接播，不会因为选了解析而绕路。
  - **直链 / 嗅探（默认）**：抓取播放页，用正则从 HTML / JSON 里提取媒体地址（移动端 UA，30 秒上限）。
  - **JSON 解析接口（type=1）**：`GET <url>?url=<播放页>`，从返回 JSON 的 `url / playUrl / data.url` 取地址，并透传 `header.Referer`。**依赖第三方解析服务，不是无后端方案**，仅在对方接口可用时有效。
  - **本地解析脚本（type=2 且 url 为 .js）——本项目扩展类型，不是 TVBox 标准语义**：脚本按需下载并缓存到本机 `parses/<hash>.js`，由 goja 执行脚本里的 `parse(url)`，返回 `{url, jx, parse, header}`（当前仅应用 `header.Referer`，其余头与 drpy 保持一致暂不转发）；`jx / parse > 0` 或返回的仍不是直链时，再按默认嗅探处理。宿主环境与 drpy 完全一致（`req` / `pdfh` / `pdfa` / 30 秒看门狗），Android / Windows / iOS 同一套纯 Go 实现，**不需要后端**；代价是脚本要自己维护或从社区找 JS 版。标准 TVBox 配置里的 `type=2`（聚合解析）不会被误当成脚本，选中时会明确提示「暂不支持」。
  - **WebView 嗅探（type 0 / 4，TVBox 标准语义）**：播放前用**无界面的内置 WebView**（Headless）以移动端 UA 打开播放页，拦截页面发出的 m3u8 / mp4 请求，命中即把该地址交给播放器；页面不自动播放时再用 JS 读取 `<video>/<source>` 兜底，25 秒超时。**Android / iOS 可用**（插件无 Windows 实现，桌面端会提示改用其他解析方式）。该方式**不支持下载**（下载链路没有浏览器环境），也不写入「下一集预加载」。
  - 解析方式与订阅一起保存在本机，对全部已导入的 LibreTV 站点生效；订阅更新后若该解析已不存在，自动回到「直链 / 嗅探」。
- **去重与上限**：同一采集接口在配置里重复出现会静默去重；单次最多导入 50 个站点，key 冲突时自动改为按地址派生。站点记录只保存在本机。
- **管理**：导入卡片列出全部已导入站点（名称、采集地址、**采集类型（JSON / XML / 规则引擎名）**、缓存剧集数）并可单个删除（规则源连同规则快照一并删除）；片源卡片「更多」菜单也有「删除此片源」。删除会同时清除该片源的本机目录缓存，之后需重新导入。
- **详情信息**：详情页展示采集接口返回的**评分**（`vod_score`）、**主演**（`vod_actor`）、**导演**（`vod_director`）、**更新时间**（`vod_time`，XML 为 `<last>`）与**站点标签**（`vod_tag`）。评分并入标题下的信息行（如 `★ 8.6 · 帝屎迪 · 共 10 集 · 连载中`），`vod_score` 为数字或字符串都能识别，`0` 视为未评分；`vod_time` 归一化为「YYYY-MM-DD HH:mm」显示在「上线 / 热度 / 播放」同一行；`vod_tag`（逗号 / 顿号 / 斜杠分隔）与地区、语言、年份合并去重后以标签形式展示。主演按中英文逗号切分后以顿号连接，过长自动省略。任一字段缺失时对应位置不渲染，不留「主演：」之类的空壳。
- **播放**：分集播放地址为 m3u8 时自动拉取播放列表（Referer 取采集站主机）；其他地址按源站返回的直链播放，不需要二级解析。同一集有多个播放源时，播放页「清晰度 / 线路」面板会列出各条线路（默认线路显示为「线路 1」，其余取播放源名），可手动切换且保留当前进度；某条线路起播超时或出错时，播放器会自动回退到其他线路（环形，最后一条之后回到首选线路），全部试过才重新解析。

### drpy 脚本源：goja 运行时执行 TVBox drpy2 脚本

站源管理顶部「LibreTV / 扩展片源」卡片（与 LibreTV 片源同卡分两区）支持导入 **drpy 脚本源**：把 TVBox / drpy2 生态的 JS 脚本（含 `var rule = {...}` 定义，或指向 `.js` 的 http(s) 地址）导入后，由本机 goja 虚拟机执行脚本里的 `init / home / category / detail / search / play` 等函数，像内置站源一样浏览、搜索、看详情与播放，脚本只在设备上运行，不依赖自建后端。

- **入口**：「站源管理 → LibreTV / 扩展片源 → 导入片源 / 脚本」。粘贴的文本含 `var/let/const rule` 或是 `.js` 地址时自动识别为 drpy 脚本；LibreTV 混合订阅（JSON / Base58）里 `type ≥ 3` 且 `api` 为 `.js` 的条目则收成候选，导入完成后弹出「发现 drpy 脚本候选」框，可逐个导入或「全部导入」，也可以暂不导入。
- **脚本约定**：执行环境提供 `req`（同步 HTTP，返回 `{status, headers, content}`，带 UA 伪装与 Referer）、`pdfh / pdfa / pd`（HTML 选择器，支持 `&&` 后代链、`||` 备选与 `[attr]` 取值）、`localStorage`（`getItem / setItem`）等宿主函数；`rule` 需暴露 `init / home / category / detail / search / play`（部分脚本用 `lazy` 惰性取流，两者都兼容）。脚本单次调用有 30 秒看门狗，超时中断并回报，不会卡死界面。
- **分集与播放**：分集 ID 记录 `$` 后的播放源标识（drpy2 约定），播放时依次尝试 `play` → `lazy`（含全局 `input` 兼容） → 结果里的 `url` 直链；返回 `jx=1 / parse=1` 时按解析页做简单嗅探（正则提取媒体地址），嗅探失败回退原地址。播放地址为 m3u8 时自动拉取播放列表（Referer 取脚本声明的 host）。
- **来源链接（+126）**：粘贴脚本文本时会多出一个「脚本来源链接（可选）」输入框——脚本文本本身没有地址，不记来源的话跨设备同步（WebDAV）只能跳过；记下来源后，对端按该地址重新拉取脚本，源站脚本更新也会一并带过去。直接粘贴 `.js` 地址导入时以地址为准，无需另填。
- **上限与去重**：单次最多导入 50 个脚本源；同一脚本重复导入按内容 key 覆盖更新。脚本正文保存在本机 `scripts/<key>.js`，站点记录在 `drpy.json`，删除时连同目录缓存一并清除。
- **兼容边界**：只实现 drpy2 核心函数约定，不模拟完整 TVBox 环境（如 `js2p` 之外的加解密引擎、WebView 依赖的脚本暂不支持）；导入前会做静态校验，明显不是 drpy 脚本的内容会被拒绝并说明原因。若脚本以 `import` / `export` 开头（ES Module），会提示「这是 ES Module 脚本，请导入 ext 里的站点脚本」——TVBox 的 drpy 站点 `api` 字段指向的运行时库正是这种格式。

#### rule 驱动型脚本（只写配置、不写函数）

社区里很大一部分 drpy2 脚本只声明 `rule`，浏览逻辑交给运行时内置函数（例如「荐片」）。这类脚本没有 `home / category / detail / search`，我们按下面的方式补齐：

| 环节 | 取值来源 |
| --- | --- |
| 分类 | `class_name` / `class_url`（`&` 分隔，按顺序配对） |
| 列表 | `url` 模板（`fyclass` → 分类 ID、`fypage` → 页码、`fyfilter` → 由 `filter_url` + `filter_def` 拼出的筛选串）+ `一级` 片段 |
| 详情 | `detailUrl`（`fyid` → 剧集 ID）+ `二级` 片段（片段写全局 `VOD`） |
| 搜索 | `searchUrl`（`**` → 关键词）+ `搜索` 片段 |
| 取流 | 没有 `play / lazy` 时直接用分集地址：相对路径按站点 host 补全，页面地址走嗅探，`tvbox-xg:` 这类 TVBox 专有协议给出明确提示 |

片段运行环境与 drpy2 对齐：`input / MY_PAGE / MY_URL / MY_HOME / HOST / cateObj / fl` 变量，**同步** `request()`（返回响应正文，与异步 `req()` 不同）、`setResult()`、`pdfh / pdfa / pd / joinUrl`；`rule.headers` 会作为 `request` 的默认请求头。片段被包在 IIFE 里执行，同一虚拟机内重复调用不会因 `let` 重复声明失败。

> 接口本身失效时（返回 HTML 404），错误会写成「一级解析失败（URL）：…（接口返回的是网页而不是 JSON，源接口可能已失效或被拦截）」，便于判断是源的问题还是脚本的问题。

#### CatVod 蜘蛛（`[cat]` / `./cat/*.js`）

订阅里标注 `[cat]` 的源是 **CatVod JS 蜘蛛**，和 drpy2 是两种方言，但在本机走同一套 goja 宿主，导入后同样出现在「drpy 脚本源」列表里（带 `CatVod` 标记）：

| 差异点 | drpy2 | CatVod 蜘蛛 |
|---|---|---|
| 形态 | 普通脚本 + 全局 `rule` | **ES Module**：`import { _, load } from './lib/cat.js'` |
| 入口 | 全局函数 `home / category / detail / play` | 末尾 `export function __jsEvalReturn() { return { init, home, … } }` |
| 返回 | 直接返回对象 | 返回 **JSON 字符串**（同构字段：`class` / `list` / `vod_*`） |
| 依赖库 | 无 | `lib/cat.js`（约 500 KB 打包产物，含 cheerio / dayjs / jinja2 / Crypto） |

- **导入链路**：粘贴蜘蛛的 `.js` 地址（或经混合订阅收成候选）→ 从脚本的 `import` 里解析出 `./lib/cat.js` 的相对路径，按蜘蛛地址补全后**下载一次并缓存**在本机 `catlib/<hash>.js`（保存的是转换后的版本）；蜘蛛脚本剥离 `import` / 顶层 `export` 后写入 `scripts/<key>.js`，`import` 的绑定名记录在站点记录里，运行前注入成全局变量。
- **运行时**：先执行 `lib/cat.js`（尾部 `export{…}` 已被替换成 `globalThis.__cat = {…}` 注入），再补 4 个轻量 Node 垫片（`process` / `Buffer`（`isBuffer` `from` `alloc` `concat` `byteLength`）/ `console` / `setTimeout`），然后跑蜘蛛、调 `__jsEvalReturn()` 取 API 对象，后续按名调用 `home / homeVod / category / search / detail / play`；`init(cfg)` 用空配置调用，失败不致命。
- **结果映射**：`{class:[{type_id,type_name}]}`、`{list:[{vod_id,vod_name,vod_pic,vod_remarks}]}`、`detail` 里的 `vod_play_from / vod_play_url` 与 MacCMS 同构，直接复用 drpy 的行映射与分集解析；`play` 返回的 `{url, parse}` 与 drpy 同构，`parse > 0` 时沿用同一套嗅探兜底。宿主函数 `req / pdfh / pdfa / pd / joinUrl` 是运行时全局，蜘蛛里直接可用，不需要额外注入。

#### 规则型 Spider：XBPQ / 海阔视界 / Bili（纯 Go 规则解释器，不需要 jar 与 JVM）

订阅里这三类 `csp_` 源原本依赖 Java Spider（`XBPQ.jar` / `XYQHiker.jar` / 内置 `Bili`），本机用 **Go 重写的规则解释器**实现，导入后直接可用：

| Spider | 规则形态 | 本机实现 |
| --- | --- | --- |
| `csp_XBPQ` | 猫系「截取」规则：`分类` / `分类url` / `数组` / `标题` / `链接` / `线路` / `播放` 等中文字段，`前标记&&后标记` 截取 | 字符串截取引擎（支持 `$$` 等价分隔、`*` 通配、`[不包含]` / `[替换]` 指令、`{cateId}` / `{catePg}` / `{wd}` 占位符、JSON 路径模式） |
| `csp_XYQHiker` | 海阔视界 51 字段规则：分类 / 搜索 / 播放三段独立配置，`0=JSON / 1=HTML` 两种截取模式 | JSON 路径导航 + CSS 选择器链（复用 drpy 的 `pdfh / pdfa` 方言，末段支持 `video-src` 等自定义属性）；支持 JSONP 二次截取、POST 搜索（表单或 JSON 体） |
| `csp_Bili` / `csp_BiliGuard` | 规则只有 `classes` 分类（+ 可选 `filter` / `cookie`），数据靠 B 站公开接口；Guard 是加固版（cookie 池 / 风控重试），规则同构 | 分类直出（零网络）；搜索 / 详情走 `search/type` 与 `view` 接口（含 **wbi 签名**），分 P 即分集，播放地址指向节目页由 WebView 嗅探取流 |

- **识别与导入**：订阅里 `api` 为上述三者（含 `csp_BiliGuard`）时自动识别（识别顺序：drpy 脚本候选 → 规则源 → `csp_AppYsV2` → 其余跳过）。`ext` 三种形态都认：远程规则地址、`{"json":"地址"}` 间接写法、内联规则对象；规则文件被托管页包裹（如 `file.html?id=…` 返回 HTML 包着 JSON）时会按 JSON 边界抽取，不会整条判无效。**规则是纯数据，不做二次确认**——导入订阅即落地，失败仅跳过该源并在结果里给出原因。
- **规则持久化**：规则快照保存在本机 `rules/<key>.json`，索引在 `rules.json`（含引擎、来源地址与更新时间）。**订阅链接失效或下线不影响已导入的规则源**；源 ID 仍是 `libretv:<key>`，与普通采集站共用站源注册、收藏 / 隐藏、线路记忆与播放链路。
- **分集与播放**：分类页 / 详情的播放地址按规则抽取后，直链直接播；播放页地址沿用「直链 / 嗅探」或用户选择的解析方式（规则抓不到播放列表时，会退化为「详情页单集」，交给 WebView 嗅探兜底）。
- **规则源标识**：站源管理里规则源显示 `XBPQ 规则` / `海阔规则` / `Bili` 标签与专用图标，长按图标可看该引擎的说明。导入结果会单独统计「含 N 个规则源」。
- **兼容边界**：不支持 `js:` 片段型规则、图片代理、短视频模式与筛选面板（v1）；规则目标站点改版导致选择器失效时，分类 / 详情会给出「没有解析到内容，规则可能已失效」的提示，重新导入更新后的规则即可恢复。
- **限制**：蜘蛛必须提供地址（裸文本无法定位 `lib/cat.js`）；`lib/cat.js` 下载失败或没有 `export` 语句时导入直接报错并说明原因；单次调用仍受 30 秒看门狗约束。

### 网络与资源设置

管理员从「设置与备份 → 网络与资源」选择自动、直连或手动代理，手动地址支持 HTTP、HTTPS、SOCKS5 / SOCKS5h。代理地址默认遮蔽、不进入备份；设备内播放服务始终绕过代理。自动模式读取系统静态代理与排除列表，恢复前台及每 30 秒更新；代理不会关闭 TLS 校验。

目录请求并发可设 1–6、间隔 0–5000 毫秒（默认 3 / 250ms）；下载并发默认 2、可设 1–6。仍保留前台优先、后台让行、取消、退避及请求数量边界。

### 局域网互联：追剧同步与推送播放（已接入，待验证）

两台设备在同一局域网直连，自动同步追剧记录或接续播放，不依赖账号服务器。服务类型 `_zgj-link._tcp`（DNS-SD / mDNS），按设备 ID 合并多网卡地址，首次配对记录证书指纹，后续连接固定校验。

入口：「追剧 → 标题栏同步」或「设置与备份 → 设备互联」。手动同步默认「双向合并」，可选覆盖对方 / 覆盖本机，先「查看预览」再执行；自动同步仅发送改动条目与删除标记，播放期间约每 10 秒发送进度。冲突记录保留候选，用户从同步页逐项处理。

### WebDAV 同步：跨网络多设备追剧同步（已接入，待验证）

通过 WebDAV 网盘（坚果云、Nextcloud、Alist、ownCloud 等）在手机、电脑、电视之间同步追剧、观看状态和续播进度，并自 +124 起扩展同步**片源订阅与收藏 / 隐藏状态**。不同步视频文件和主题等设置；「设备互联」（局域网）链路仍只同步追剧记录，片源偏好仅随 WebDAV 传输。

入口：「设置与备份 → WebDAV 同步」。填写服务器地址（http / https）、账号、密码和同步目录（默认 `zhenguoju-sync`），可先「测试连接」再「立即同步」。地址要填到服务商给出的 WebDAV 目录为止：**坚果云是 `https://dav.jianguoyun.com/dav/`**（缺 `/dav/` 会 404 / 410），Nextcloud 通常形如 `https://example.com/remote.php/dav/files/用户名/`。同步目录下的每一级会自动创建。同步时下载同步目录内其他设备的记录文件，按版本向量合并后写入本机，再把合并结果上传为本机的记录文件；同名文件被覆盖不影响最终一致，下次同步会补齐。打开应用时可自动同步一次，失败不影响使用，错误在同步页显示。

**片源订阅与收藏 / 隐藏状态（+124）**：

- **同步内容**：已导入的 LibreTV / drpy 片源（采集地址 / 脚本 URL；规则源同步的是「规则地址」，对端凭它重新下载规则快照），以及每个运行时片源的收藏、隐藏状态。收藏 / 隐藏状态按「最新操作时间」合并，取消操作同样会传播到其他设备（本地以取消墓碑记录，不是只增不减的并集）；两端同一毫秒生效时按字段取并集。
- **清理云端站源（+129）**：WebDAV 页新增「清理云端站源」按钮——同步是取并集，对端设备导过的失效站源会被反复带回来；点一次会把云端保存的片源订阅清单**整体替换为本机当前站源**（本机那份写本机清单，其余设备那份清空），下次同步各设备即对齐。只动片源订阅，**追剧记录与收藏 / 隐藏状态不受影响**；执行前弹确认框，完成后提示「改写几个文件 / 清掉几条 / 云端现为本机几条」。
- **自动导入**：对端设备导入、本机还没有的片源会在同步时自动导入并刷新运行时注册表，立即可用。既有片源按「身份指纹」比对（采集站比接口地址、规则源比规则地址、脚本源比脚本地址），**指纹变了才会重新导入**，避免每次同步整批重导；粘贴导入且没有记录来源链接的 drpy 脚本仍会跳过并给出警告（可在导入时补填来源链接）。个别片源导入失败只记警告，不影响追剧记录同步。
- **流量**：片源订阅与偏好每条仅百余字节，追剧记录文件整体上限仍为 8 MiB。

同一处记录在不同设备被改成不同值时会保留双版本，同步结果显示冲突数量，在「设备互联」页逐项选择保留哪一条。设备恢复配置或更换副本标识后会在同步目录留下旧记录文件，暂不自动清理。

账号密码只保存在本机配置中，界面默认遮蔽且不进入导出备份；同步使用基本认证，自签名证书不会被接受。单个记录文件上限 8 MiB，超过会在同步前提示整理记录。

## 安装包与平台状态

| 平台 | 包与状态 |
| --- | --- |
| Android 8.0+ | 三架构（arm64-v8a / armeabi-v7a / x86_64）APK；同一签名可覆盖升级 |
| Windows 10/11 x64 | 完整 ZIP 解压后运行 `hongguojian.exe` / `zhenguojian.exe`，保留所有 DLL 与 `data`；局域网原生发现依赖 Windows 10 1903+ |
| Android TV | 与手机共用源码，自动识别电视模式并保持横屏；待电视 / 盒子实机验收 |
| iOS 15.1+ | 已加入工程、Go 核心链接、媒体依赖、文件管理与构建脚本；iOS 播放页禁用 media_kit_video 硬件纹理加速以规避 libmpv 渲染崩溃；待 Xcode 构建与真机验收，无已签名 IPA |

`INSTALL_FAILED_NO_MATCHING_ABIS` 表示 APK 与设备架构不匹配，请更换对应架构安装包。

### GitHub Actions

推送 `main` / `master`、`v*` 标签、提交 PR，或手动运行 **Build app packages**，会先检查再构建两版（默认与 `--all-sources`）的 Android 包：

| 产物 | 内容 |
| --- | --- |
| `*-android` | 三种架构 APK 和 SHA256 |

推送 `main` 且 android 构建成功时，自动创建 / 更新 GitHub Release（tag `app-v{version}`）。发布新版本前需先在 `pubspec.yaml` 提升 `version`，否则会覆盖同名 tag 的 Release。

**Windows 包由独立工作流 Build Windows package 构建**（仅手动触发）：在 `windows-latest` 上依次编译红果鉴 / 真果鉴，产出两个 `*-windows-x64.zip`，直接上传到 pubspec 版本对应的 Release，并把这两行补进该 Release 的 `SHA256SUMS.txt`（校验文件只下载自身，不重拉 APK）。单次约 30~40 分钟；要构建历史版本，在 GitHub 的「Use workflow from」里选对应 tag——源码版本与目标 Release 对不上会在解析阶段直接失败。iOS 暂未接入 CI。

Windows 构建要用的 libmpv 与 ANGLE 是固定日期的 7z，而 `shinchiro/mpv-winbuild-cmake` 只保留最近两周的 Release，pin 的版本很快就会 404（第一次跑 Build Windows package 就是这么挂的）。因此 `packages/media_kit_libs_windows_video/windows/CMakeLists.txt` 里 pin 的版本会镜像到本仓库的 `native-deps` Release，CMake 优先从那里下载、失败再回退上游。**升级 mpv / ANGLE pin 之后要跑一次 Mirror native deps 工作流**把新版本镜像上去，否则下次构建只能靠上游（上游删档即失败）。

Android 正式发布使用同一签名并递增构建号，在仓库 Secrets 配置：`ANDROID_KEYSTORE_BASE64`、`ANDROID_KEYSTORE_PASSWORD`、`ANDROID_KEY_ALIAS`、`ANDROID_KEY_PASSWORD`。未配置时生成 debug 签名预览包。

本地不入库的 `android/key.properties`：

~~~properties
storeFile=/absolute/path/zhenguojian-release.jks
storePassword=你的密码
keyAlias=zhenguojian
keyPassword=你的密码
~~~

## 开发与构建

Flutter `3.47.4`、Dart `3.12+`、Go `1.24.1+`、Python `3.10+`。Android 需要 JDK 17、SDK 36、NDK `28.2.13676358`；Windows 需要 Visual Studio C++ 桌面组件及 MinGW-w64 x64；iOS 需要 macOS、完整 Xcode 和 CocoaPods。

构建脚本对子进程默认设置 `GOPROXY=https://goproxy.cn,direct`、`GOSUMDB=off`，同名环境变量可覆盖。

~~~sh
python3 scripts/build_android.py                 # 红果鉴
python3 scripts/build_android.py --all-sources   # 真果鉴
python3 scripts/build_android.py --abi arm64-v8a
python3 scripts/build_android.py --cn-mirrors    # 国内镜像
~~~

~~~powershell
.\scripts\build_windows.ps1
.\scripts\build_windows.ps1 -AllSources
.\scripts\build_windows.ps1 -ChinaMirrors
~~~

~~~sh
python3 scripts/build_ios.py
python3 scripts/build_ios.py --all-sources
python3 scripts/build_ios.py --core-only [--simulator]
python3 scripts/build_ios.py --export-options /path/to/ExportOptions.plist
~~~

产物在 `dist/android`、`dist/windows`、`dist/ios`，红果版以 `hongguojian-` 开头，全站源版以 `zhenguojian-` 开头。

首次 Android 调试先编译对应架构核心，再运行：

~~~sh
python3 scripts/build_native.py --platform android --abi arm64-v8a
flutter pub get --enforce-lockfile
flutter run
~~~

调试全站源版：先给 `build_native.py` 加 `--all-sources`，再 `flutter run --dart-define=ALL_SOURCES=true`；iOS 对应 `build_ios.py --core-only --all-sources`。脚本会同步设置 Dart 常量和 Go 编译参数，应用启动时校验二者一致，避免混装原生库。

版本号唯一权威源是 `pubspec.yaml` 的 `version: x.y.z+N`：构建脚本（`scripts/app_build.py`）自动提取 `N` 并注入 `--dart-define=BUILD_NUMBER=N`，三个平台的 `flutter build` 均走此路径；手动 `flutter run/build` 不传时，`lib/app_build.dart` 的 `duanjuBuildNumber` 回落 defaultValue，启动日志与关于页会显示旧值——发版后核对日志版本号应与 pubspec 一致，不一致说明构建没走脚本。

播放器使用 [media_kit](https://github.com/media-kit/media-kit) / libmpv，合并和导出使用 [FFmpegKit min-gpl](https://github.com/sk3llo/ffmpeg_kit_flutter)（含 GPL 媒体组件）。FFmpegKit 不参与正常播放或下载的转码。

### 集中检查与真机回归

~~~sh
python3 -m unittest discover -s scripts -p 'test_*.py'
dart format --output=none --set-exit-if-changed lib test integration_test test_driver
dart analyze --fatal-infos lib test integration_test test_driver
flutter test --dart-define=DISABLE_REMOTE_IMAGES=true
flutter test --dart-define=DISABLE_REMOTE_IMAGES=true --dart-define=ALL_SOURCES=true
cd native
go test -race ./...
go test -race -ldflags="-X duanjuapp/native/core.buildAllSources=true" ./...
~~~

Android 设备回归（连接并授权 USB 调试、保持解锁）：

~~~sh
python3 scripts/create_test_media.py
python3 scripts/serve_test_media.py
adb reverse tcp:38473 tcp:38473
flutter drive --driver=test_driver/playback.dart --target=integration_test/playback_test.dart \
  --dart-define=DISABLE_REMOTE_IMAGES=true --dart-define=FIXTURE_BASE_URL=http://127.0.0.1:38473
~~~

结果在 `build/device-test/results/`；结束后停服务并 `adb reverse --remove tcp:38473`。

### 源码同步

~~~sh
python3 scripts/finish_task.py --message "本次实际完成的变更"
python3 scripts/sync_source.py --check
~~~

脚本只同步纯源码到同级 `../guoapp`，并生成 `真果·鉴-YYYYMMDDHHMM.zip` 源码压缩包；不执行 Git 提交、分支或推送。

## 目录结构

| 目录 | 内容 |
| --- | --- |
| `lib` | 页面、播放器、本地用户、FFI、下载和媒体处理 |
| `native/core`、`native/bridge` | 独立站源核心、缓存、下载、目录迁移及 C ABI |
| `android`、`windows`、`ios` | 平台工程与必要资源 |
| `assets/video_enhancement`、`packages/media_kit_libs_windows_video` | 增强 Shader 与许可、固定 Windows 媒体依赖插件 |
| `scripts`、`.github/workflows` | 构建、签名、验证、同步和版本快照 |
| `test`、`integration_test` | 自动化与设备回归 |

## 站源开发约定

站源是 Go 原生 provider（`native/core/provider_*.go`），不是运行期加载的 Python 源。新增站源需接入：`provider_huangguo.go`（常量 / 白名单 / `canonicalProviderSource` / `GetHuangguoChapters`）、`provider_media.go`（baseURL / host 反查 / 播放分派）、`app_categories.go`、`app_cover_metadata.go`、`app_runtime.go`（Config 字段 + 目录 / 搜索 / 详情分派 + 空页放行），并在 `lib/models.dart` 登记 `SourceSite`。

注意：Go RE2 正则**不支持 lookahead**。解析多线路播放列表时不能用 `(?=...)` 做分段，否则非捕获组会消耗下一段开头；应改用显式字符串截断（从容器标记之后查找下一段标记）。

例外：**LibreTV 片源**（`native/core/provider_libretv.go`）不走上述编译期内置流程，而是用户在站源管理粘贴订阅配置 / 采集地址后**运行期导入**的动态源：源 ID 为 `libretv:<key>`（站点记录落本机 `libretv.json`），Dart 侧经 `SourceSite.setRuntimeSources` 注册进运行时注册表（`lib/models.dart`），`app_build.go` 的 `nativeSourceAvailable` 对 `libretv:` 前缀直接放行（不受 `--all-sources` 编译开关限制）。新增此类运行期源时需同步：`splitProviderDramaID` 的 ID 切分、`nativeCatalog` / `nativeDetail` / `nativeCategories` 的 default 分支、删除时的缓存清理，以及 `lib/core_bridge.dart` 的 repository 方法与 `lib/sources_screen.dart` 的导入 / 删除 UI。

**drpy 脚本源**（`native/core/provider_drpy*.go`）是第二类运行期源：源 ID 为 `drpy:<key>`，脚本正文落本机 `scripts/<key>.js`、站点记录落 `drpy.json`，`nativeSourceAvailable` 同样放行。解析与执行分层：

- `provider_drpy.go`：导入 / 删除 / 静态校验（`validateDrpyScript` / `drpyExtractMeta`）、片源记录存储，以及 home / category / detail / search 四类浏览函数的调用与结果映射（`drpyDramaFromRow` / `drpyChaptersFromGroups`）；
- `provider_drpy_goja.go`：goja 桥接层——`req`（同步 HTTP）、`pdfh / pdfa / pd` 选择器宿主函数、`installDrpyCallHelper`（空 program 泵微任务驱动 async 脚本函数）、`vm.Interrupt` + `cancel` 双保险看门狗（30 秒）、Program 按 key 缓存与 Runtime 用后即弃；
- `provider_drpy_select.go`：drpy 选择器方言的 `x/net/html` 实现（`&&` 后代链、`||` 备选、`[attr]` / `#id` / `.class`）；
- `provider_drpy_media.go`：播放闭环——`play` / `lazy` 分流、`jx=1 / parse=1` 嗅探、直链兜底与 m3u8 播放列表拉取；
- `provider_libretv.go` 的 `parseLibreTVSubscription`：混合订阅里 `type ≥ 3` 且 `api` 为 `.js` 的条目收成 `drpyCandidate`，经 `importLibreTVSubscription` 的 `drpy` 字段透传给 Dart 侧候选框（CatVod 蜘蛛的相对路径按订阅地址补全后再判定）。
- `provider_catvod.go`：**CatVod 蜘蛛**方言（`drpySite.Engine == "catvod"`）——ESM → 普通脚本转换（剥离 `import` / 顶层 `export`，`export{…}` 改成 `globalThis.__cat = {…}` 注入）、`lib/cat.js` 下载与 `catlib/<hash>.js` 缓存、Node 垫片（`process` / `Buffer` / `console` / `setTimeout`）、`__jsEvalReturn()` 取 API 对象后经 `__catCall` 调方法；浏览 / 详情 / 播放在 `provider_drpy.go` 与 `provider_drpy_media.go` 里按 `drpySiteIsCatVod` 分流，结果映射与 drpy 共用（CatVod 多一层「返回 JSON 字符串」解码）。
- `provider_drpy_rule.go`：**rule 驱动型脚本**的默认函数——`drpyHasFunction` 判断脚本是否真的定义了 `home / category / detail / search`，缺失时按 `rule` 模板 + `js:` 片段补齐（模板替换、`filter_url` 默认值、同步 `request`、`setResult` / `VOD`、片段 IIFE 包装）；片段错误会带上请求地址，接口返回网页时追加「源接口可能已失效」的说明。

注意：goja 的 `Export()` 对 JS 整数返回 `int64`，宿主侧取整需同时兼容 int64 / int32 / float32；Go 结构体直接转 JS 对象会暴露 Go 字段名，drpy 脚本按小写约定读取，`req` 的 resolve 必须用 `map[string]any`。

LibreTV 的解析层按采集格式分两个文件，新增格式时按同样方式扩展：

- `provider_libretv.go`：MacCMS **JSON** 采集（`macCMSResponse` / `rows()` / `libretvDramaFromRow` / `libretvScoreText`）；
- `provider_libretv_xml.go`：MacCMS **XML** 采集（`<rss><list page pagecount><video>`、分类 `<class><ty id>`、分集 `<dl><dd flag>`），与 JSON 版共用 `libretvChaptersFromGroups` 做分集解析；
- `provider_libretv_base58.go`：**Base58** 订阅解码（bs58 / Bitcoin 字母表，无校验和），导入时解码后复用同一套 JSON 解析；
- `provider_libretv_parse.go`：**播放解析**——直链直接播、非直链按所选方式走嗅探 / JSON 接口 / 本地脚本（goja 复用 drpy 宿主，脚本缓存于 `parses/`）；type 0/4 的 **WebView 嗅探**在 Dart 侧（`lib/webview_sniffer.dart`，Headless InAppWebView 拦截媒体请求）先换成直链，Go 只报出「需要 WebView」的明确错误；
- `libretvSite.Type` 记录采集格式（1 = JSON、2 = XML），旧订阅文件缺省按 JSON 处理；类型未标注时 `errLibreTVNotJSON` / `errLibreTVNotXML` 触发按响应内容自动回退。

## 待研究

### 黄果源镜像（已决定不做自适应）

- **结论**：不引入发布页 / footer 动态镜像池。实际抓取后确认「永久地址」不是内容站：
  - `https://huangguoai.ai`（发布页 `foreverUrls` 里的永久地址）是**导航页**，`/detail/10586/`、`/recommend/` 全部 404，页面只负责列出最新入口；
  - 该导航页当前指向 `xvlp.dsqqahhno.cc` / `ajbcs.dsqqahhno.cc` / `j0176.dsqqahhno.cc`（沙箱实测均 200），这类 `*.cc` 域名会轮换，写死必然过期；
  - 发布页 `https://huangguoai.pages.dev/publish.js` 的 `urls[]` 是 `jxqgmx5ga.cc` / `arj2ftl0.cc` / `40ygo615.cc`（同为临时域名），`otherUrls` 是两个 pages.dev 发布页，`foreverUrls` 只有 `huangguoai.com`、空 `JumpPage`、`huangguoai.ai`。
- **所以**：唯一稳定的内容域名就是 `huangguoai.com`，候选池固定为它一个。镜像域名（`*.ediayikma.cc` / `*.agdkczeyx.cc`）通信成功率低于官方、有梯子时更慢，已从候选池移除；`providerSourceForURL` 里仍保留这两个后缀的识别，旧缓存里的图片 URL 还能正常带 Referer。
- **真要做无梯子兜底**：只能在运行时抓 `huangguoai.ai` 导航页、解析 `href="https://xxx.dsqqahhno.cc"`，把命中的域名临时塞进候选池，并同步扩 `providerSourceForURL` 的白名单。不写死、不预置。
