# 只下载构建 APK 必需的两块 SDK 组件（platform + build-tools），
# 不装 Android Studio，也不装 Gradle。
#
# 用法： .\scripts\setup-android-sdk.ps1 -Sdk "C:\Android\sdk"

[CmdletBinding()]
param(
    [string]$Sdk = $(if ($env:ANDROID_SDK_ROOT) { $env:ANDROID_SDK_ROOT } else { "" }),
    [string]$BuildTools = "36.0.0",
    [int]$Platform = 34
)

$ErrorActionPreference = "Stop"
if (-not $Sdk) { throw "请用 -Sdk 指定 Android SDK 目录，或设置 `$env:ANDROID_SDK_ROOT" }
$Sdk = [System.IO.Path]::GetFullPath($Sdk)
New-Item -ItemType Directory -Force -Path $Sdk | Out-Null

$cache = Join-Path $Sdk ".downloads"
New-Item -ItemType Directory -Force -Path $cache | Out-Null

function Fetch($url) {
    $name = Split-Path $url -Leaf
    $file = Join-Path $cache $name
    if ((Test-Path $file) -and (Get-Item $file).Length -gt 1MB) {
        Write-Host "  cached  $name"
        return $file
    }
    Write-Host "  get     $url"
    & curl.exe -S -L --fail --retry 3 --retry-delay 2 -o $file $url
    if ($LASTEXITCODE -ne 0) { throw "下载失败: $url" }
    return $file
}

function Unzip-To($zip, $dest) {
    if (Test-Path $dest) { Remove-Item $dest -Recurse -Force }
    New-Item -ItemType Directory -Force -Path $dest | Out-Null
    & tar.exe -xf $zip -C $dest
    if ($LASTEXITCODE -ne 0) { throw "解压失败: $zip" }
}

# ---------- platform ----------
$platformDir = Join-Path $Sdk "platforms\android-$Platform"
if (Test-Path (Join-Path $platformDir "android.jar")) {
    Write-Host "  ok      platforms;android-$Platform"
} else {
    Write-Host "  安装 platforms;android-$Platform"
    $zip = Fetch "https://dl.google.com/android/repository/platform-$Platform-ext7_r03.zip"
    $tmp = Join-Path $Sdk ".tmp-platform"
    Unzip-To $zip $tmp
    $jar = Get-ChildItem $tmp -Recurse -Filter "android.jar" | Select-Object -First 1
    if (-not $jar) { throw "压缩包里没有 android.jar" }
    if (Test-Path $platformDir) { Remove-Item $platformDir -Recurse -Force }
    New-Item -ItemType Directory -Force -Path (Split-Path $platformDir -Parent) | Out-Null
    Move-Item $jar.Directory.FullName $platformDir
    Remove-Item $tmp -Recurse -Force
    Write-Host "  ok      platforms;android-$Platform"
}

# ---------- build-tools ----------
$btDir = Join-Path $Sdk "build-tools\$BuildTools"
if (Test-Path (Join-Path $btDir "aapt2.exe")) {
    Write-Host "  ok      build-tools;$BuildTools"
} else {
    Write-Host "  安装 build-tools;$BuildTools"
    # 34 及更早是 build-tools_r34-windows.zip，35 起改成 build-tools_r36_windows.zip
    $ver = ($BuildTools -split '\.')[0]
    $zip = $null
    foreach ($name in @("build-tools_r$ver-windows.zip", "build-tools_r$ver`_windows.zip")) {
        try {
            $zip = Fetch "https://dl.google.com/android/repository/$name"
            break
        } catch {
            Write-Host "  跳过 $name"
        }
    }
    if (-not $zip) { throw "下载 build-tools;$BuildTools 失败" }
    $tmp = Join-Path $Sdk ".tmp-buildtools"
    Unzip-To $zip $tmp
    $aapt = Get-ChildItem $tmp -Recurse -Filter "aapt2.exe" | Select-Object -First 1
    if (-not $aapt) { throw "压缩包里没有 aapt2.exe" }
    if (Test-Path $btDir) { Remove-Item $btDir -Recurse -Force }
    New-Item -ItemType Directory -Force -Path (Split-Path $btDir -Parent) | Out-Null
    Move-Item $aapt.Directory.FullName $btDir
    Remove-Item $tmp -Recurse -Force
    Write-Host "  ok      build-tools;$BuildTools"
}

Write-Host ""
Write-Host "  SDK 就绪: $Sdk"
