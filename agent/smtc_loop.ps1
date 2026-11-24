# hl-agent 用：常驻读 Windows 的媒体会话（SMTC）。
# 从标准输入每读到一行就答一行 JSON；标准输入关了（主程序退出）就结束。
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$mgr = $null
$asTask = $null
function Init {
    Add-Type -AssemblyName System.Runtime.WindowsRuntime
    $script:asTask = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
        $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1'
    })[0]
    $t = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager, Windows.Media.Control, ContentType=WindowsRuntime]
    $script:mgr = Await ($t::RequestAsync()) ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager])
}
function Await($op, $type) {
    $task = $script:asTask.MakeGenericMethod($type).Invoke($null, @($op))
    if (-not $task.Wait(4000)) { throw 'timeout' }
    return $task.Result
}
while ($true) {
    $line = [Console]::In.ReadLine()
    if ($null -eq $line) { break }
    try {
        if ($null -eq $script:mgr) { Init }
        $sess = $script:mgr.GetCurrentSession()
        if (-not $sess) {
            $obj = [ordered]@{ status = 'none' }
        } else {
            $info = Await ($sess.TryGetMediaPropertiesAsync()) ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties])
            $play = $sess.GetPlaybackInfo()
            $obj = [ordered]@{
                status = [string]$play.PlaybackStatus
                app = [string]$sess.SourceAppUserModelId
                title = [string]$info.Title
                artist = [string]$info.Artist
                album = [string]$info.AlbumTitle
                playbackType = [int]$info.PlaybackType
            }
        }
        $json = $obj | ConvertTo-Json -Compress
    } catch {
        $script:mgr = $null
        $json = '{"status":"none","error":"smtc failed"}'
    }
    [Console]::Out.WriteLine($json)
    [Console]::Out.Flush()
}
