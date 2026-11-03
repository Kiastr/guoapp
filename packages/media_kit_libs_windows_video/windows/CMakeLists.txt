cmake_minimum_required(VERSION 3.14)

option(MEDIA_KIT_LIBS_AVAILABLE "package:media_kit libraries are available." ON)
set(PROJECT_NAME "media_kit_libs_windows_video")
project(${PROJECT_NAME} LANGUAGES CXX)
add_compile_definitions(_DISABLE_CONSTEXPR_MUTEX_CONSTRUCTOR)

if(NOT CMAKE_SIZEOF_VOID_P EQUAL 8 OR FLUTTER_TARGET_PLATFORM STREQUAL "windows-arm64")
  message(FATAL_ERROR "The pinned media libraries require Windows x64.")
endif()

# 下载并解压固定版本的原生依赖。
#
# urls 是分号分隔的候选地址列表，按顺序尝试——上游 shinchiro/mpv-winbuild-cmake
# 是滚动删除的（只留最近两周的 release），pin 的日期版过几天就 404，所以本仓库
# 的 native-deps Release 存了一份镜像，优先从那里拉。
#
# 提前把文件放到 ${CMAKE_BINARY_DIR}/ 可跳过下载（CI 预热、离线构建都用这个
# 口子，只要 sha256 对得上）。
function(duanju_native_archive filename urls digest destination)
  set(archive "${CMAKE_BINARY_DIR}/${filename}")
  if(EXISTS "${archive}")
    file(SHA256 "${archive}" actual_digest)
    if(NOT actual_digest STREQUAL digest)
      file(REMOVE "${archive}")
    endif()
  endif()
  if(NOT EXISTS "${archive}")
    set(download_ok FALSE)
    set(attempts "")
    foreach(url IN LISTS urls)
      if(url STREQUAL "")
        continue()
      endif()
      message(STATUS "Downloading ${filename} from ${url}")
      # 不能给 file(DOWNLOAD) 传 EXPECTED_HASH：只要下载失败（404/超时），
      # CMake 会直接抛 "file DOWNLOAD cannot compute hash on failed download"
      # 并中止整个 configure，一个源都没法回退。改成下完自己算一遍。
      file(DOWNLOAD "${url}" "${archive}"
        TLS_VERIFY ON
        TIMEOUT 300
        INACTIVITY_TIMEOUT 45
        STATUS download_status
      )
      list(GET download_status 0 download_code)
      if(download_code EQUAL 0)
        file(SHA256 "${archive}" downloaded_digest)
        if(downloaded_digest STREQUAL digest)
          set(download_ok TRUE)
          break()
        endif()
        string(APPEND attempts "  ${url} -> sha256 不符（${downloaded_digest}）\n")
        message(WARNING "校验不符：${url}")
      else()
        string(APPEND attempts "  ${url} -> ${download_status}\n")
        message(WARNING "下载失败：${url}（${download_status}）")
      endif()
      file(REMOVE "${archive}")
    endforeach()
    if(NOT download_ok)
      message(FATAL_ERROR "Failed to download ${filename}，所有源均不可用：\n${attempts}")
    endif()
  endif()

  set(valid FALSE)
  if(EXISTS "${destination}/.duanju-sha256")
    file(READ "${destination}/.duanju-sha256" extracted_digest)
    if(extracted_digest STREQUAL digest)
      set(valid TRUE)
    endif()
  endif()
  foreach(required IN LISTS ARGN)
    if(NOT EXISTS "${destination}/${required}")
      set(valid FALSE)
    endif()
  endforeach()
  if(NOT valid)
    file(REMOVE_RECURSE "${destination}")
    file(MAKE_DIRECTORY "${destination}")
    execute_process(
      COMMAND "${CMAKE_COMMAND}" -E tar xf "${archive}"
      WORKING_DIRECTORY "${destination}"
      RESULT_VARIABLE extract_result
    )
    if(NOT extract_result EQUAL 0)
      message(FATAL_ERROR "Failed to extract ${filename}")
    endif()
    foreach(required IN LISTS ARGN)
      if(NOT EXISTS "${destination}/${required}")
        message(FATAL_ERROR "${filename} is missing ${required}")
      endif()
    endforeach()
    file(WRITE "${destination}/.duanju-sha256" "${digest}")
  endif()
endfunction()

set(LIBMPV_SRC "${CMAKE_BINARY_DIR}/libmpv")
# 升级这个 pin 的三步：改文件名 → 改 URL 里的日期/commit → 改 sha256
# （GitHub API 的 release asset 有 digest 字段，直接取）。改完顺手跑一次
# Build native deps 工作流把新版本镜像到 native-deps Release。
set(DUANJU_MPV_ARCHIVE "mpv-dev-x86_64-20261103-git-a687796d9d.7z")
set(DUANJU_MPV_SHA256 "41cbfda596aaf1dc30053679bcd22828afaeec411340de6bf4c0e786fed1480b")
set(DUANJU_MPV_URLS
  "https://github.com/Kiastr/guoapp/releases/download/native-deps/${DUANJU_MPV_ARCHIVE}"
  "https://github.com/shinchiro/mpv-winbuild-cmake/releases/download/20261103/${DUANJU_MPV_ARCHIVE}"
)
# 故意不走 CACHE：build/ 被复用时缓存会锁着旧值，改了 pin 却不生效最容易踩。
# 需要临时换源（内网镜像、离线包）就设同名环境变量，分号分隔。
if(DEFINED ENV{DUANJU_MPV_URLS})
  set(DUANJU_MPV_URLS "$ENV{DUANJU_MPV_URLS}")
endif()
duanju_native_archive(
  "${DUANJU_MPV_ARCHIVE}"
  "${DUANJU_MPV_URLS}"
  "${DUANJU_MPV_SHA256}"
  "${LIBMPV_SRC}"
  "libmpv-2.dll" "libmpv.dll.a" "include/mpv/client.h" "include/mpv/render.h"
)
file(COPY "${LIBMPV_SRC}/include/mpv/" DESTINATION "${LIBMPV_SRC}/include")

set(ANGLE_SRC "${CMAKE_BINARY_DIR}/ANGLE")
set(DUANJU_ANGLE_ARCHIVE "ANGLE-v1.0.1.7z")
set(DUANJU_ANGLE_SHA256 "cc5911bb15d596fd5a2b362613ad35b7093b427117269a7359054a65746a5f9a")
set(DUANJU_ANGLE_URLS
  "https://github.com/Kiastr/guoapp/releases/download/native-deps/${DUANJU_ANGLE_ARCHIVE}"
  "https://github.com/alexmercerind/flutter-windows-ANGLE-OpenGL-ES/releases/download/v1.0.1/ANGLE.7z"
)
if(DEFINED ENV{DUANJU_ANGLE_URLS})
  set(DUANJU_ANGLE_URLS "$ENV{DUANJU_ANGLE_URLS}")
endif()
duanju_native_archive(
  "${DUANJU_ANGLE_ARCHIVE}"
  "${DUANJU_ANGLE_URLS}"
  "${DUANJU_ANGLE_SHA256}"
  "${ANGLE_SRC}"
  "include/EGL/egl.h" "lib/libEGL.dll.lib" "lib/libGLESv2.dll.lib"
  "d3dcompiler_47.dll" "libc++.dll" "libEGL.dll" "libGLESv2.dll"
  "vk_swiftshader.dll" "vulkan-1.dll" "zlib.dll"
)

set(PLUGIN_NAME "media_kit_libs_windows_video_plugin")
add_library(${PLUGIN_NAME} SHARED
  "include/media_kit_libs_windows_video/media_kit_libs_windows_video_plugin_c_api.h"
  "media_kit_libs_windows_video_plugin_c_api.cpp"
)
apply_standard_settings(${PLUGIN_NAME})
set_target_properties(${PLUGIN_NAME} PROPERTIES CXX_VISIBILITY_PRESET hidden)
target_compile_definitions(${PLUGIN_NAME} PRIVATE FLUTTER_PLUGIN_IMPL)
target_include_directories(${PLUGIN_NAME} INTERFACE "${CMAKE_CURRENT_SOURCE_DIR}/include")
target_link_libraries(${PLUGIN_NAME} PRIVATE flutter flutter_wrapper_plugin)

set(media_kit_libs_windows_video_bundled_libraries
  "${LIBMPV_SRC}/libmpv-2.dll"
  "${ANGLE_SRC}/d3dcompiler_47.dll"
  "${ANGLE_SRC}/libc++.dll"
  "${ANGLE_SRC}/libEGL.dll"
  "${ANGLE_SRC}/libGLESv2.dll"
  "${ANGLE_SRC}/vk_swiftshader.dll"
  "${ANGLE_SRC}/vulkan-1.dll"
  "${ANGLE_SRC}/zlib.dll"
  PARENT_SCOPE
)
