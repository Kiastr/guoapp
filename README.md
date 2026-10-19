# 红果鉴 / 真果鉴

> ⚠️ **免责声明**：本项目源码来自网上大名鼎鼎的**鱼佬**（原作者）。我只是把它拿来打包、测试着玩，方便自己用，**不保证任何可用性，随时可能删库**。

Flutter 多端独立短剧 / 影视应用，原名「短剧库 APP」。站源请求、解析、下载和播放均在设备上完成，不依赖自建服务。当前源码版本：**0.2.73+107**。

早期版本在 0.2.50 基线上新增两个原生站源：**韩小圈**（`hanxiaoquan`，韩剧 / 韩国电影 / 韩国综艺 / 韩国动漫）与**鬼片网**（`guipian`，鬼片 / 电视剧 / 动漫）。默认可见站源顺序为：**红果 → 韩小圈 → 鬼片 → 青空**。

## 最近更新（0.2.73+107）

| 版本 | 变更 |
| --- | --- |
| +107 | 新增第三层 **WebView 嗅探**（解析 type 0 / 4 等价实现）：播放前用无界面 WebView 打开播放页，拦截 m3u8 / mp4 请求得到直链，再交给播放器（Android / iOS）。站源管理可识别并选择「WebView 嗅探」类解析；该方式不支持下载 |
| --- | --- |
| +106 | LibreTV 订阅支持 **parses 解析规则**：导入时保留 name/type/url/ext，站源管理新增「播放解析方式」下拉。默认「直链 / 嗅探」无需后端；可选标准 **type=1 JSON 解析接口**（依赖第三方服务）；新增**本地解析脚本**扩展类型（type=2 且 url 为 .js，goja 执行 `parse(url)`，纯 Go 无后端）。仅对非直链播放页生效 |
| --- | --- |
| +105 | 播放器与 LibreTV 细节完善：**左右半屏横向拖动都可调进度**（左半屏竖向仍调亮度）；采集源播放页新增**线路选择**（LibreTV 多播放源同集切换），无画质档位时不再显示空的「清晰度」；线路记忆加 15 分钟有效期并支持环形回退，刷新恢复改从默认线路重试；顶部栏时钟电量与倍速/拖动提示改为白字；LibreTV 详情补 **vod_time / vod_tag**（更新于 … + 站点标签） |
| --- | --- |
| +104 | 新增 **drpy 脚本源**：站源管理可导入 TVBox drpy2 脚本（粘贴含 `var rule` 的 JS 或 .js 地址），本机 goja 运行时执行，覆盖浏览 / 搜索 / 详情 / 播放全链路；LibreTV 混合订阅里的 type≥3 脚本收成候选，确认后一键导入 |
| --- | --- |
| +103 | LibreTV 片源扩展：支持 **type=2 的苹果 CMS XML 采集接口**（`/at/xml/`），并可直接读取 **LunaTV-config 等社区工具导出的 Base58 订阅**（`format=2` / `format=3`），预先筛好的源无需自建后端即可导入 |
| --- | --- |
| +102 | LibreTV 片源详情页补全**评分 / 主演 / 导演**：解析 `vod_score`、`vod_actor`、`vod_director` 并透传到详情页，评分兼容数字与字符串、`0` 视为未评分，字段缺失不占位 |
| +101 | 新增 **LibreTV 片源导入**：站源管理可粘贴订阅配置链接、配置 JSON 或裸 MacCMS provide/vod 采集地址，导入后即可浏览、搜索与播放；type≥3 的 drpy 源自动跳过；已导入站点可随时删除（连同目录缓存） |
| +100 | 红果账号新增**短信登录**与**扫码登录**；扫码为短信被服务端风控拦截时的替代路径，登录后可恢复取流并同步账号数据 |
| +99 | 红果账号合并改为单一原子提交，修复跨区快照过期 |
| +98 | 红果账号数据同步：拉取账号侧收藏、观看记录与播放进度，按时间戳判优并入本机（单向） |
| +97 | 红果登录态取流管道：会话持久化、Cookie 注入与错误提示人话化 |
| +96 | 修复黄果三个榜单（潜力榜 / 热播榜 / 推荐榜）全部报「页面结构变化」的问题：站点 JSON-LD 条目路径由 /detail/ 改为 /video/，解析器现两种路径均接受；另补齐遗漏的测试定义使 core 包可编译 |
| +95 | 高级画质增强新增**「极简档」**，负载更低，弱 GPU 设备也能开启增强 |
| +94 | 修复长按倍速**选中态回跳 3x**（菜单回显时没带上已选档位，设置本身是生效的）；长按倍速新增 **1.5x / 1.75x** 档位 |
| +93 | 播放器菜单新增**「长按倍速」**档位（1.5x / 1.75x / 2x / 2.5x / 3x / 4x / 5x，默认 3x），同时修复 WebDAV 记录文件被写到上一级目录导致坚果云上传 404 的问题 |
| +92 | 长按三倍速提示改为**顶部紧凑小胶囊**（字号 12.5、内边距收紧、位置上移），速看刷剧时不再挡住画面主体 |
| +91 | 新增 **WebDAV 同步**：跨网络在手机、电脑、电视之间同步追剧、观看状态和续播进度，复用局域网同步的合并引擎，详见「WebDAV 同步」小节 |
| +89 / +90 | 播放器右半屏手势改为进度调整；高级画质增强新增**质量档**；长按 3 倍速不再弹出暂停键与进度条 |
| +87 / +88 | 安卓端画质增强改为**轻量档**（还原清晰度而不强求放大），状态文案简化为「已执行超分降噪」；自动模式标记实验性 |
| +83 ~ +86 | 画质增强模式重构为自动族子档 + 高级强制档，增加失败诊断字段；修复 shader 注释被 mpv 误判导致的安卓端增强静默空转（见「画质增强」小节） |

> 说明：应用长期按「只维护源码与定向测试，集中验证另行安排」的方式推进。未完成集中验证与真机验收的项目保持未验收状态；历史版本的检查记录不能作为后续新增功能的验收结论。

| 编译方式 | 应用名称 | 可用站源 |
| --- | --- | --- |
| 默认（不加参数） | 红果鉴 | 仅红果 |
| 加 `--all-sources` | 真果鉴 | 红果、韩小圈、鬼片、青空，以及黄豆、剧果、野果、帝果、黄果视频、黄果 AI、黄果旧版 |

默认可见站源（红果、韩小圈、鬼片、青空）无需密码；其余站源默认隐藏，在站源管理里通过密码锁解锁后显示。已有受限用户不会自动获得新增站源权限，管理员可在用户管理中勾选。

这是**编译选项**，应用内不能切换版本。标题、Android 桌面名称与电视横幅、Windows 窗口与分发文件名、iOS 显示名随编译选项变化。界面、站源调用、原生下载调度同时限制可用站源；红果版不会访问或继续执行其他站源的旧任务。

两版保留原 Android / iOS 应用标识及数据目录；Android 使用同一签名可相互覆盖升级，不能作为两个独立正式应用并排安装。切换版本保留追剧、观看记录、用户权限和下载记录；备份格式保持兼容。

## 使用

| 功能 | 操作 |
| --- | --- |
| 浏览 | 标题栏依次提供排序与筛选、榜单、多选下载和展开搜索。分类保持独立一行，内容区左右滑动切类；支持默认、名称、自然季号、上线日期、热度、播放量排序及连载状态筛选。列表下滑接近底部自动加载下一页，底部「加载更多」为兜底 |
| 搜索 | 红果合并官网与名称索引，输入停顿 300 毫秒显示最多 10 条联想；韩小圈、剧果、野果、鬼片调用各自的在线搜索并按分页继续加载。部分失败保留有效结果；最近 20 次搜索按用户保存 |
| 推荐 | 仅红果分类栏在「全部」后显示「推荐」，可切换真人剧、漫剧、AI 剧并分别记住位置 |
| 详情 | 普通点剧直接进入播放；下载和兜底入口保留详情页。详情页浏览封面、资料、追剧状态与可展开简介；底部固定「立即播放 / 继续播放」和下载入口。选集默认折叠，展开后每组 50 集，可切换范围、定位当前或输入集数跳转；宽屏与电视采用资料、选集分栏 |
| 追剧与历史 | 按想看 / 在看 / 已看 / 有更新筛选；红果已追剧发现新季后进入「有更新」。追剧和历史均可搜索。每个用户独立保存，每 5 秒及退出播放时记录真实进度 |
| 卡片与批量下载 | 卡片「更多」提供收藏、状态和下载选集；电脑右键，电视菜单键。发现页长按卡片或点标题栏「多选下载」，一次最多 50 部 |
| VIP | 全站源版浏览黄豆或全部站源时显示 VIP 图标，仅过滤黄豆；默认隐藏已确认的 VIP。未知、免费、VIP 分别保存，未知不会覆盖已知状态 |
| 手机播放 | 上滑下一集、下滑上一集；**左右半屏横向拖动都是进度调整**，左半屏竖向滑动调亮度，右半屏竖向滑动切集。长按 350 毫秒临时加速到「长按倍速」（默认 3 倍，播放菜单「倍速」页可选 1.5 / 1.75 / 2 / 2.5 / 3 / 4 / 5 倍，松开恢复）。竖屏轻点播放 / 暂停，横屏轻点控制条，双击播放 / 暂停。视频下方为「选集 / 简介 / 下载」Tab |
| 画中画 | Android 手机 / 平板播放器接入画中画按钮；进入前隐藏自绘控制层与弹幕，小窗只保留画面。Android TV、Windows、iOS 暂未接入 |
| 画质增强 | 关闭 / 自动（实验性）/ 省电增强 / 清晰优先 / 高级。高级模式可选三档：极简（负载最低，弱 GPU 也能跑）、轻量（默认）、质量（效果最好，负载最高）。全部在 GPU 上以着色器执行，不使用 NNAPI / NPU |
| 弹幕 | 红果在线播放默认开启；播放器侧边圆形「弹」字按钮可关闭、查看状态及失败重试。本地播放不加载弹幕 |
| 下载 | 播放页「下载」Tab 和详情页下载入口均可选择分集与画质；支持批量暂停、继续、重试、删除及清理任务但保留视频。「更新本剧」补新增或缺失分集 |
| 更多 | 站源管理、用户管理、设置与备份、界面模式、关于 |

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

站源可用性、清晰度和区域限制取决于源站及网络；应用不解除源站 VIP 或其他授权限制。

### 线路记忆与自动回退

播放器会记住每部剧上次成功起播的线路，切集时直接从该线路开始，减少「先试一条慢线」的等待（纯本地记录，不额外发请求）。记忆 **15 分钟**后失效，过期后回到首选线路重新试；起播 3 秒（预加载已验证的线路 6 秒）没有画面或播放出错时，自动切到同集的下一条线路，最后一条之后回到首选线路（环形），避免停留在备选线上永远回不到默认线。全部线路都试过仍失败才重新解析，重新解析前会清掉线路记忆、从默认线路开始；连续失败达到上限则停止自动恢复，提示重试或换集。

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
  - `type ≥ 3` 的 drpy / spider 脚本源：`api` 为 `.js` 地址时收成 **drpy 候选**（不自动导入），导入完成后弹出候选框，用户确认后逐个或一次性导入，详见下方「drpy 脚本源」小节；`api` 不是 `.js` 的其他类型在结果里说明原因并跳过。
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
- **管理**：导入卡片列出全部已导入站点（名称、采集地址、**采集类型（JSON / XML）**、缓存剧集数）并可单个删除；片源卡片「更多」菜单也有「删除此片源」。删除会同时清除该片源的本机目录缓存，之后需重新导入。
- **详情信息**：详情页展示采集接口返回的**评分**（`vod_score`）、**主演**（`vod_actor`）、**导演**（`vod_director`）、**更新时间**（`vod_time`，XML 为 `<last>`）与**站点标签**（`vod_tag`）。评分并入标题下的信息行（如 `★ 8.6 · 帝屎迪 · 共 10 集 · 连载中`），`vod_score` 为数字或字符串都能识别，`0` 视为未评分；`vod_time` 归一化为「YYYY-MM-DD HH:mm」显示在「上线 / 热度 / 播放」同一行；`vod_tag`（逗号 / 顿号 / 斜杠分隔）与地区、语言、年份合并去重后以标签形式展示。主演按中英文逗号切分后以顿号连接，过长自动省略。任一字段缺失时对应位置不渲染，不留「主演：」之类的空壳。
- **播放**：分集播放地址为 m3u8 时自动拉取播放列表（Referer 取采集站主机）；其他地址按源站返回的直链播放，不需要二级解析。同一集有多个播放源时，播放页「清晰度 / 线路」面板会列出各条线路（默认线路显示为「线路 1」，其余取播放源名），可手动切换且保留当前进度；某条线路起播超时或出错时，播放器会自动回退到其他线路（环形，最后一条之后回到首选线路），全部试过才重新解析。

### drpy 脚本源：goja 运行时执行 TVBox drpy2 脚本

站源管理顶部「LibreTV / 扩展片源」卡片（与 LibreTV 片源同卡分两区）支持导入 **drpy 脚本源**：把 TVBox / drpy2 生态的 JS 脚本（含 `var rule = {...}` 定义，或指向 `.js` 的 http(s) 地址）导入后，由本机 goja 虚拟机执行脚本里的 `init / home / category / detail / search / play` 等函数，像内置站源一样浏览、搜索、看详情与播放，脚本只在设备上运行，不依赖自建后端。

- **入口**：「站源管理 → LibreTV / 扩展片源 → 导入片源 / 脚本」。粘贴的文本含 `var/let/const rule` 或是 `.js` 地址时自动识别为 drpy 脚本；LibreTV 混合订阅（JSON / Base58）里 `type ≥ 3` 且 `api` 为 `.js` 的条目则收成候选，导入完成后弹出「发现 drpy 脚本候选」框，可逐个导入或「全部导入」，也可以暂不导入。
- **脚本约定**：执行环境提供 `req`（同步 HTTP，返回 `{status, headers, content}`，带 UA 伪装与 Referer）、`pdfh / pdfa / pd`（HTML 选择器，支持 `&&` 后代链、`||` 备选与 `[attr]` 取值）、`localStorage`（`getItem / setItem`）等宿主函数；`rule` 需暴露 `init / home / category / detail / search / play`（部分脚本用 `lazy` 惰性取流，两者都兼容）。脚本单次调用有 30 秒看门狗，超时中断并回报，不会卡死界面。
- **分集与播放**：分集 ID 记录 `$` 后的播放源标识（drpy2 约定），播放时依次尝试 `play` → `lazy`（含全局 `input` 兼容） → 结果里的 `url` 直链；返回 `jx=1 / parse=1` 时按解析页做简单嗅探（正则提取媒体地址），嗅探失败回退原地址。播放地址为 m3u8 时自动拉取播放列表（Referer 取脚本声明的 host）。
- **上限与去重**：单次最多导入 50 个脚本源；同一脚本重复导入按内容 key 覆盖更新。脚本正文保存在本机 `scripts/<key>.js`，站点记录在 `drpy.json`，删除时连同目录缓存一并清除。
- **兼容边界**：只实现 drpy2 核心函数约定，不模拟完整 TVBox 环境（如 `js2p` 之外的加解密引擎、WebView 依赖的脚本暂不支持）；导入前会做静态校验，明显不是 drpy 脚本的内容会被拒绝并说明原因。

### 网络与资源设置

管理员从「设置与备份 → 网络与资源」选择自动、直连或手动代理，手动地址支持 HTTP、HTTPS、SOCKS5 / SOCKS5h。代理地址默认遮蔽、不进入备份；设备内播放服务始终绕过代理。自动模式读取系统静态代理与排除列表，恢复前台及每 30 秒更新；代理不会关闭 TLS 校验。

目录请求并发可设 1–6、间隔 0–5000 毫秒（默认 3 / 250ms）；下载并发默认 2、可设 1–6。仍保留前台优先、后台让行、取消、退避及请求数量边界。

### 局域网互联：追剧同步与推送播放（已接入，待验证）

两台设备在同一局域网直连，自动同步追剧记录或接续播放，不依赖账号服务器。服务类型 `_zgj-link._tcp`（DNS-SD / mDNS），按设备 ID 合并多网卡地址，首次配对记录证书指纹，后续连接固定校验。

入口：「追剧 → 标题栏同步」或「设置与备份 → 设备互联」。手动同步默认「双向合并」，可选覆盖对方 / 覆盖本机，先「查看预览」再执行；自动同步仅发送改动条目与删除标记，播放期间约每 10 秒发送进度。冲突记录保留候选，用户从同步页逐项处理。

### WebDAV 同步：跨网络多设备追剧同步（已接入，待验证）

通过 WebDAV 网盘（坚果云、Nextcloud、Alist、ownCloud 等）在手机、电脑、电视之间同步追剧、观看状态和续播进度，同步范围与「局域网互联」一致：只同步追剧记录，不同步视频文件和主题等设置。

入口：「设置与备份 → WebDAV 同步」。填写服务器地址（http / https）、账号、密码和同步目录（默认 `zhenguoju-sync`），可先「测试连接」再「立即同步」。地址要填到服务商给出的 WebDAV 目录为止：**坚果云是 `https://dav.jianguoyun.com/dav/`**（缺 `/dav/` 会 404 / 410），Nextcloud 通常形如 `https://example.com/remote.php/dav/files/用户名/`。同步目录下的每一级会自动创建。同步时下载同步目录内其他设备的记录文件，按版本向量合并后写入本机，再把合并结果上传为本机的记录文件；同名文件被覆盖不影响最终一致，下次同步会补齐。打开应用时可自动同步一次，失败不影响使用，错误在同步页显示。

同一处记录在不同设备被改成不同值时会保留双版本，同步结果显示冲突数量，在「设备互联」页逐项选择保留哪一条。设备恢复配置或更换副本标识后会在同步目录留下旧记录文件，暂不自动清理。

账号密码只保存在本机配置中，界面默认遮蔽且不进入导出备份；同步使用基本认证，自签名证书不会被接受。单个记录文件上限 8 MiB，超过会在同步前提示整理记录。

### 红果账号数据同步：账号收藏与观看记录并入本机剧库（单向）

已登录红果账号时，可把账号侧的收藏、观看记录与播放进度同步到本机。入口：「设置与备份 → 红果账号 → 同步账号数据」。

- **同步内容**：红果书架（收藏）、观看历史、播放进度三类数据，按剧集维度合并进本机追剧库与观看记录。
- **合并规则**：按服务器时间戳判优——同一条记录，服务器时间戳较新的一方生效；本机较新的记录不会被覆盖。进度（上次看到第几集、看到几分几秒）折叠进观看记录。
- **单向同步**：当前只做「账号 → 本机」。本机新增的追剧和观看记录暂不上传红果账号；账号里删除的条目也不会删本机（本机独有的收藏始终保留）。
- **容错**：登录态失效（服务端不识别）会提示重新登录；单个端点失败不影响其余数据，同步结果会列出新增/更新的条数与未完成的部分。
- 登录态同时用于缓解红果对未登录设备的取流限制。

合并结果经现有「局域网互联」与「WebDAV 同步」链路自然分发到其他设备，无需额外操作。

### 红果短信登录：手机号 + 验证码登录红果账号（Phase 3）

入口：「设置与备份 → 红果账号 → 短信登录」。未登录时账号对话框显示登录按钮；已登录时同一处可「退出登录」，副标题会即时刷新。

- **流程**：填 11 位手机号（自动补 +86）→「获取验证码」→ 填短信验证码 →「登录」。登录成功后会话由服务端 Cookie 构成并只保存在本机，设置页账号区块显示「已登录」。
- **图形验证码**：部分号码会被服务端要求先过图形验证码，此时对话框会明确提示并停在手机号步，不会把用户带进必败的填码步。
- **隐私边界**：验证码由红果服务端发送，应用只转发、不接收也不储存短信内容；登录态与 Phase 1/2 的会话持久化同源，退出登录即清除本机会话。
- **错误提示**：服务端错误码会转成人话（如「验证码错误」「访问太频繁」）；风控要求换设备身份等场景会直接展示服务端描述。
- 登录态的作用与上一节相同：恢复取流 + 同步账号数据；两者可分别使用，不同步数据也可以只登录。

### 红果扫码登录：手机扫码确认登录红果账号（Phase 3d）

入口：「设置与备份 → 红果账号 → 扫码登录」，与短信登录并列显示在账号对话框中。部分号码发短信验证码会被服务端风控拒绝（要求「SDK 接入」），扫码登录是这种情况下的替代路径。

- **流程**：打开对话框即创建扫码会话（token + 二维码）→ 每 2 秒轮询一次 → 用**番茄作家助手**扫描二维码并按提示确认 → 应用自动完成登录，账号对话框与设置页状态即时刷新。二维码有效期约 10 分钟，过期后点「刷新二维码」重新创建。
- **与短信登录的关系**：两者是并列入口而非替代关系。短信登录走应用端接口，可能被服务端风控拦截；扫码登录走网页端扫码确认，不受该风控影响，也无需在应用里填任何账号密码。
- **隐私边界**：扫码确认发生在手机端，应用只拿到扫码链路下发的会话 Cookie，不接触也拿不到扫码设备上的账号凭据；会话持久化与 Phase 1/2 及短信登录同源，只保存在本机，退出登录即清除。
- **扫码入口说明**：当前扫码页是番茄小说网页端的登录入口，需要用**番茄作家助手**这类可识别该码的客户端确认，抖音等其他 App 不识别此二维码。
- **已知限制**：该入口拿到的是**网页端会话**，与红果 App 接口（另一套 aid / 域）不匹配，因此**对播放取流没有帮助**；账号数据同步同样走 App 接口，可用性未验证。免费集的播放取流走 App 接口的匿名请求，不依赖登录。

## 安装包与平台状态

| 平台 | 包与状态 |
| --- | --- |
| Android 8.0+ | 三架构（arm64-v8a / armeabi-v7a / x86_64）APK；同一签名可覆盖升级 |
| Windows 10/11 x64 | 完整 ZIP 解压后运行 `hongguojian.exe` / `zhenguojian.exe`，保留所有 DLL 与 `data`；局域网原生发现依赖 Windows 10 1903+ |
| Android TV | 与手机共用源码，自动识别电视模式并保持横屏；待电视 / 盒子实机验收 |
| iOS 15.1+ | 已加入工程、Go 核心链接、媒体依赖、文件管理与构建脚本；iOS 播放页禁用 media_kit_video 硬件纹理加速以规避 libmpv 渲染崩溃；待 Xcode 构建与真机验收，无已签名 IPA |

`INSTALL_FAILED_NO_MATCHING_ABIS` 表示 APK 与设备架构不匹配，请更换对应架构安装包。

### GitHub Actions

推送 `main` / `master`、`v*` 标签、提交 PR，或手动运行 **Build app packages**，会先检查再构建两版（默认与 `--all-sources`）：

| 产物 | 内容 |
| --- | --- |
| `*-android` | 三种架构 APK 和 SHA256 |
| `*-windows` | 完整 ZIP 和 SHA256 |
| `*-ios-unsigned` | 未签名 `.app` ZIP 和 SHA256，不能直接当已签名 IPA 安装 |

推送 `main` 且 android / ios / windows 全部构建成功时，自动创建 / 更新 GitHub Release（tag `app-v{version}`）。发布新版本前需先在 `pubspec.yaml` 提升 `version`，否则会覆盖同名 tag 的 Release。

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
- `provider_libretv.go` 的 `parseLibreTVSubscription`：混合订阅里 `type ≥ 3` 且 `api` 为 `.js` 的条目收成 `drpyCandidate`，经 `importLibreTVSubscription` 的 `drpy` 字段透传给 Dart 侧候选框。

注意：goja 的 `Export()` 对 JS 整数返回 `int64`，宿主侧取整需同时兼容 int64 / int32 / float32；Go 结构体直接转 JS 对象会暴露 Go 字段名，drpy 脚本按小写约定读取，`req` 的 resolve 必须用 `map[string]any`。

LibreTV 的解析层按采集格式分两个文件，新增格式时按同样方式扩展：

- `provider_libretv.go`：MacCMS **JSON** 采集（`macCMSResponse` / `rows()` / `libretvDramaFromRow` / `libretvScoreText`）；
- `provider_libretv_xml.go`：MacCMS **XML** 采集（`<rss><list page pagecount><video>`、分类 `<class><ty id>`、分集 `<dl><dd flag>`），与 JSON 版共用 `libretvChaptersFromGroups` 做分集解析；
- `provider_libretv_base58.go`：**Base58** 订阅解码（bs58 / Bitcoin 字母表，无校验和），导入时解码后复用同一套 JSON 解析；
- `provider_libretv_parse.go`：**播放解析**——直链直接播、非直链按所选方式走嗅探 / JSON 接口 / 本地脚本（goja 复用 drpy 宿主，脚本缓存于 `parses/`）；type 0/4 的 **WebView 嗅探**在 Dart 侧（`lib/webview_sniffer.dart`，Headless InAppWebView 拦截媒体请求）先换成直链，Go 只报出「需要 WebView」的明确错误；
- `libretvSite.Type` 记录采集格式（1 = JSON、2 = XML），旧订阅文件缺省按 JSON 处理；类型未标注时 `errLibreTVNotJSON` / `errLibreTVNotXML` 触发按响应内容自动回退。
