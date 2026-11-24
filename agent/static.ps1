# hl-agent 用：一次性查硬件 / 系统信息，输出一行 JSON（6 小时查一次）
$ErrorActionPreference = 'SilentlyContinue'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
function Ms($d) { if ($d) { try { return ([DateTimeOffset]$d).ToUnixTimeMilliseconds() } catch { } } return 0 }

$cpu = @(Get-CimInstance Win32_Processor)
$c0 = $cpu | Select-Object -First 1
$cs = Get-CimInstance Win32_ComputerSystem
$l1 = (@(Get-CimInstance Win32_CacheMemory | Where-Object { $_.Level -eq 3 }) | Measure-Object -Property InstalledSize -Sum).Sum
$virt = [bool]$c0.VirtualizationFirmwareEnabled -or [bool]$cs.HypervisorPresent

$types = @{ 20 = 'DDR'; 21 = 'DDR2'; 24 = 'DDR3'; 26 = 'DDR4'; 30 = 'LPDDR4'; 34 = 'DDR5'; 35 = 'LPDDR5' }
$ffs = @{ 8 = 'DIMM'; 12 = 'SODIMM' }
$mods = @(Get-CimInstance Win32_PhysicalMemory | ForEach-Object {
    [ordered]@{
        slot = [string]$_.DeviceLocator; bank = [string]$_.BankLabel; capacity = [double]$_.Capacity
        type = $types[[int]$_.SMBIOSMemoryType]; formFactor = $ffs[[int]$_.FormFactor]
        speed = [int]$_.Speed; configuredSpeed = [int]$_.ConfiguredClockSpeed
        manufacturer = [string]$_.Manufacturer; partNumber = [string]$_.PartNumber
    }
})
$slots = (@(Get-CimInstance Win32_PhysicalMemoryArray) | Measure-Object -Property MemoryDevices -Sum).Sum

$gpus = @(Get-CimInstance Win32_VideoController | ForEach-Object {
    $dd = ''
    if ($_.DriverDate) { $dd = ([datetime]$_.DriverDate).ToString('yyyy-MM-dd') }
    [ordered]@{ name = [string]$_.Name; driverVersion = [string]$_.DriverVersion; driverDate = $dd; provider = [string]$_.AdapterCompatibility }
})

$os = Get-CimInstance Win32_OperatingSystem
$pack = ''
try { $pack = [string](@(Get-AppxPackage -Name MicrosoftWindows.Client.CBS) | Sort-Object -Property { [version]$_.Version } -Descending | Select-Object -First 1).Version } catch { }

# 先排序再转成字典：Sort-Object 认不了字典里的键
$ups = @(Get-CimInstance Win32_QuickFixEngineering | ForEach-Object {
    [pscustomobject]@{ kb = [string]$_.HotFixID; title = [string]$_.Description; dateMs = [int64](Ms $_.InstalledOn) }
} | Sort-Object -Property @{ Expression = 'dateMs'; Descending = $true }, @{ Expression = 'kb'; Descending = $true })

$pf = (@(Get-CimInstance Win32_PageFileUsage) | Measure-Object -Property AllocatedBaseSize -Sum).Sum

$obj = [ordered]@{
    cpu = [ordered]@{
        name = ([string]$c0.Name).Trim(); cores = [int](($cpu | Measure-Object -Property NumberOfCores -Sum).Sum)
        logical = [int](($cpu | Measure-Object -Property NumberOfLogicalProcessors -Sum).Sum); sockets = $cpu.Count
        l1KB = [int]$l1; l2KB = [int]$c0.L2CacheSize; l3KB = [int]$c0.L3CacheSize; baseMHz = [int]$c0.MaxClockSpeed; virtualization = $virt
    }
    memory = [ordered]@{
        modules = $mods; slotsTotal = [int]$slots
        type = ($mods | Select-Object -First 1).type; formFactor = ($mods | Select-Object -First 1).formFactor
        speed = [int](($mods | Select-Object -First 1).configuredSpeed)
    }
    gpu = $gpus
    os = [ordered]@{
        caption = ([string]$os.Caption).Trim(); version = [string]$os.Version; build = [string]$os.BuildNumber
        installDate = (Ms $os.InstallDate); architecture = [string]$os.OSArchitecture; systemType = [string]$cs.SystemType; experiencePack = $pack
    }
    updates = $ups
    pagefile = [ordered]@{ allocatedMB = [double]$pf }
}
[Console]::Out.WriteLine(($obj | ConvertTo-Json -Depth 6 -Compress))
