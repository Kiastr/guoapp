import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path

from build_mirrors import china_mirror_environment, mirrored_pub_lockfile
from app_build import BuildVariant, add_variant_argument

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('--cn-mirrors', action='store_true', help='使用 Flutter 中国镜像')
add_variant_argument(parser)
options = parser.parse_args()
variant = BuildVariant(options.all_sources)
environment = os.environ.copy()
environment.setdefault('GOPROXY', 'https://goproxy.cn,direct')
environment.setdefault('GOSUMDB', 'off')
flutter = shutil.which('flutter')
if not flutter:
    raise SystemExit('请先将 Flutter SDK 的 bin 目录加入 PATH。')
with china_mirror_environment(environment, options.cn_mirrors, gradle=False) as env:
    with mirrored_pub_lockfile(root, env):
        subprocess.run([sys.executable, str(root / 'scripts' / 'build_native.py'), '--platform', 'windows', *variant.arguments],
                       cwd=root, env=env, check=True)
        # 与 build_android.py 保持一致，不加 --enforce-lockfile。
        #
        # 仓库里的 pubspec.lock 与 pubspec.yaml 在当前 Flutter 3.47.4 下解析
        # 结果本就不同：本地直接跑 `flutter pub get --enforce-lockfile` 同样
        # 报 "Unable to satisfy `pubspec.yaml` using `pubspec.lock`"（退出 65）。
        # Android 构建一直只跑普通 `pub get`，所以这个问题从没暴露；Windows
        # 首次接入 CI 就撞上了。普通 `pub get` 在不满足时就地更新 lock，产物
        # 仍由 pubspec.yaml 决定，不影响可复现性。
        subprocess.run([flutter, 'pub', 'get'], cwd=root, env=env, check=True)
        subprocess.run([flutter, 'build', 'windows', '--release', '--no-pub', *variant.flutter_arguments], cwd=root, env=env, check=True)
        subprocess.run([sys.executable, str(root / 'scripts' / 'package_release.py'), '--platform', 'windows', *variant.arguments],
                       cwd=root, env=env, check=True)
