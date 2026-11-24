#!/bin/sh
# 安卓代码编译检查（不打包）：javac 对着 android.jar 编一遍，抓语法 / 类型错误。真正的 APK 在 Windows 上用 build-apk.ps1 打
set -e
H="$(cd "$(dirname "$0")/.." && pwd)"
J="$H/.android-check/android.jar"
[ -f "$J" ] || { echo "没有 $J"; exit 1; }
W=$(mktemp -d /tmp/hl-javac.XXXXXX) && chmod 777 "$W"
docker run --rm --user "$(id -u):$(id -g)" -v "$H/android:/src:ro" -v "$J:/android.jar:ro" -v "$W:/w" eclipse-temurin:17-jdk-alpine sh -c '
  find /src/app/src/main/java -name "*.java" > /w/files
  javac -encoding UTF-8 --release 8 -Xlint:deprecation -cp /android.jar -d /w/out @/w/files 2>&1 | grep -v "^Note:" | grep -vE "has been deprecated|^\s+\^|^\s+(Notification|if \(Build)" | head -60
  echo "classes: $(find /w/out -name "*.class" 2>/dev/null | wc -l)"'
python3 - "$H/android/app/src/main/AndroidManifest.xml" <<'EOF'
import sys, xml.dom.minidom
xml.dom.minidom.parse(sys.argv[1]); print('manifest XML OK')
EOF
rm -rf "$W"
