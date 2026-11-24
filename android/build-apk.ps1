# Build Android debug APK (requires JDK, Android SDK, and Gradle).
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -File android/build-apk.ps1
# Environment variables (optional if already on PATH / configured):
#   JAVA_HOME        Path to JDK 17+ / 21
#   ANDROID_HOME     Path to Android SDK (or ANDROID_SDK_ROOT)
#   GRADLE_BIN       Path to gradle.bat (defaults to `gradle` on PATH)
$ErrorActionPreference = "Stop"

$proj = $PSScriptRoot
$log = Join-Path $proj "build.log"
"[$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')] Building $proj" | Out-File -FilePath $log -Encoding utf8

$sdk = if ($env:ANDROID_HOME) { $env:ANDROID_HOME } else { $env:ANDROID_SDK_ROOT }
if (-not $sdk -or -not (Test-Path $sdk)) {
    Write-Host "ANDROID_HOME (or ANDROID_SDK_ROOT) is not set or does not exist." -ForegroundColor Red
    exit 1
}

$gradle = if ($env:GRADLE_BIN) { $env:GRADLE_BIN } else {
    $cmd = Get-Command gradle -ErrorAction SilentlyContinue
    if ($cmd) { $cmd.Source } else { $null }
}
if (-not $gradle -or -not (Test-Path $gradle)) {
    Write-Host "Gradle not found. Install Gradle on PATH or set GRADLE_BIN." -ForegroundColor Red
    exit 1
}

$localProps = Join-Path $proj "local.properties"
if (-not (Test-Path $localProps)) {
    $escapedSdk = $sdk.Replace("\", "\\").Replace(":", "\:")
    "sdk.dir=$escapedSdk" | Out-File -FilePath $localProps -Encoding ascii
}

Write-Host "Building APK... (full log: $log)"
$ErrorActionPreference = "Continue"
& $gradle -p $proj :app:assembleDebug --console=plain --no-daemon >> $log 2>&1
$code = $LASTEXITCODE
$ErrorActionPreference = "Stop"

$apk = Join-Path $proj "app\build\outputs\apk\debug\app-debug.apk"
$out = Join-Path (Split-Path -Parent $proj) "hongtang-live.apk"
if ($code -eq 0 -and (Test-Path $apk)) {
    Copy-Item $apk $out -Force
    Write-Host "BUILD_OK $out" -ForegroundColor Green
    exit 0
} else {
    Get-Content $log -Tail 25 | Write-Host
    Write-Host "BUILD_FAILED (full log: $log)" -ForegroundColor Red
    exit 1
}
