# 用 aapt2 + javac + d8 + apksigner 直接构建 APK。
# 这样不需要 Gradle、不需要联网拉 AGP 依赖，只要能跑 PowerShell 就能出包。
#
# 用法：
#   .\scripts\build-apk.ps1
#   .\scripts\build-apk.ps1 -Sdk "C:\Android\sdk" -JdkHome "C:\Program Files\Microsoft\jdk-21"

[CmdletBinding()]
param(
    [string]$Sdk = $(if ($env:ANDROID_SDK_ROOT) { $env:ANDROID_SDK_ROOT } elseif ($env:ANDROID_HOME) { $env:ANDROID_HOME } else { "" }),
    [string]$JdkHome = $env:JAVA_HOME,
    [string]$OutDir = "",
    [string]$OutName = "PastePort.apk",
    [string]$VersionName = "0.1.0",
    [int]$VersionCode = 1,
    [int]$MinSdk = 24,
    [int]$TargetSdk = 34,
    [string]$BuildTools = "36.0.0",
    [string]$KeystorePath = ""
)

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot
$AndroidDir = Join-Path $RepoRoot "android"
if (-not $OutDir) { $OutDir = Join-Path $RepoRoot "dist" }
$OutDir = [System.IO.Path]::GetFullPath($OutDir)

function Fail($msg) { Write-Host "  x $msg" -ForegroundColor Red; exit 1 }
function Step($msg) { Write-Host "  > $msg" -ForegroundColor Cyan }

if (-not $Sdk) { Fail "找不到 Android SDK。请设置 `$env:ANDROID_SDK_ROOT 或用 -Sdk 指定。" }
if (-not $JdkHome) {
    $j = Get-Command javac -ErrorAction SilentlyContinue
    if ($j) { $JdkHome = Split-Path -Parent (Split-Path -Parent $j.Source) }
}
if (-not $JdkHome -or -not (Test-Path (Join-Path $JdkHome "bin\javac.exe"))) {
    Fail "找不到 JDK。请设置 `$env:JAVA_HOME 或用 -JdkHome 指定。"
}

$bt = Join-Path $Sdk "build-tools\$BuildTools"
$androidJar = Join-Path $Sdk "platforms\android-$TargetSdk\android.jar"
foreach ($f in @("aapt2.exe", "d8.bat", "zipalign.exe", "apksigner.bat")) {
    if (-not (Test-Path (Join-Path $bt $f))) { Fail "缺少 build-tools 文件: $f（期望在 $bt）" }
}
if (-not (Test-Path $androidJar)) { Fail "缺少 android.jar: $androidJar" }

$env:JAVA_HOME = $JdkHome
$env:PATH = (Join-Path $JdkHome "bin") + ";" + $env:PATH
$env:PATH = $bt + ";" + $env:PATH

$aapt2 = Join-Path $bt "aapt2.exe"
$d8 = Join-Path $bt "d8.bat"
$zipalign = Join-Path $bt "zipalign.exe"
$apksigner = Join-Path $bt "apksigner.bat"
$javac = Join-Path $JdkHome "bin\javac.exe"
$jar = Join-Path $JdkHome "bin\jar.exe"
$keytool = Join-Path $JdkHome "bin\keytool.exe"

$build = Join-Path $RepoRoot "build\android"
if (Test-Path $build) { Remove-Item $build -Recurse -Force }
New-Item -ItemType Directory -Force -Path (Join-Path $build "gen"), (Join-Path $build "classes"), (Join-Path $build "dex") | Out-Null
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

Write-Host ""
Write-Host "  PastePort APK 构建" -ForegroundColor White
Write-Host "  -----------------------------"
Write-Host "  sdk     $Sdk"
Write-Host "  jdk     $JdkHome"
Write-Host "  version $VersionName ($VersionCode)"
Write-Host ""

# 1. 编译资源
Step "aapt2 compile (资源)"
& $aapt2 compile --dir (Join-Path $AndroidDir "res") -o (Join-Path $build "res.zip")
if ($LASTEXITCODE -ne 0) { Fail "aapt2 compile 失败" }

# 2. 链接资源 + 生成 R.java 和基础 APK
Step "aapt2 link (清单 + 资源表)"
& $aapt2 link -o (Join-Path $build "base.apk") `
    -I $androidJar `
    --manifest (Join-Path $AndroidDir "AndroidManifest.xml") `
    -R (Join-Path $build "res.zip") `
    --java (Join-Path $build "gen") `
    --auto-add-overlay `
    --min-sdk-version $MinSdk `
    --target-sdk-version $TargetSdk `
    --version-code $VersionCode `
    --version-name $VersionName
if ($LASTEXITCODE -ne 0) { Fail "aapt2 link 失败" }

# 3. 编译 Java（源码列表写进 @argfile，避免命令行过长和转义问题）
Step "javac (Java 源码)"
$rjava = Get-ChildItem (Join-Path $build "gen") -Recurse -Filter "R.java" | ForEach-Object { $_.FullName }
$sources = Get-ChildItem (Join-Path $AndroidDir "src") -Recurse -Filter "*.java" | ForEach-Object { $_.FullName }
if (-not $sources) { Fail "android/src 下没有找到 .java" }
$argfile = Join-Path $build "sources.txt"
$lines = @($rjava) + @($sources) | ForEach-Object { '"' + ($_ -replace '\\', '/') + '"' }
Set-Content -Path $argfile -Value $lines -Encoding Ascii
# 用 android.jar 当引导类路径，这样只会用到 Android 里真实存在的 API，
# 不会不小心编进桌面 JDK 才有的方法。
# javac 会往 stderr 打过时 API 提示，临时放宽策略，靠退出码判断成败。
$prevEap = $ErrorActionPreference
$ErrorActionPreference = "Continue"
& $javac -encoding UTF-8 -source 8 -target 8 -nowarn `
    -bootclasspath $androidJar `
    -d (Join-Path $build "classes") ("@" + $argfile)
$javacCode = $LASTEXITCODE
$ErrorActionPreference = $prevEap
if ($javacCode -ne 0) { Fail "javac 失败" }

# 4. 打包 class -> dex
Step "d8 (dex)"
& $jar --create --file (Join-Path $build "classes.jar") -C (Join-Path $build "classes") .
if ($LASTEXITCODE -ne 0) { Fail "jar 打包失败" }
& $d8 --release --min-api $MinSdk --lib $androidJar --output (Join-Path $build "dex") (Join-Path $build "classes.jar")
if ($LASTEXITCODE -ne 0) { Fail "d8 失败" }
$dex = Join-Path $build "dex\classes.dex"
if (-not (Test-Path $dex)) { Fail "没有生成 classes.dex" }

# 5. 把 classes.dex 塞回 APK
Step "合并 classes.dex"
Add-Type -AssemblyName System.IO.Compression.FileSystem
$apkPath = Join-Path $build "base.apk"
$zip = [System.IO.Compression.ZipFile]::Open($apkPath, "Update")
try {
    $entry = $zip.CreateEntry("classes.dex", [System.IO.Compression.CompressionLevel]::Optimal)
    $dst = $entry.Open()
    $src = [System.IO.File]::OpenRead($dex)
    try { $src.CopyTo($dst) } finally { $src.Close(); $dst.Close() }
} finally {
    $zip.Dispose()
}

# 6. 对齐
Step "zipalign"
$aligned = Join-Path $build "aligned.apk"
& $zipalign -f -p 4 $apkPath $aligned
if ($LASTEXITCODE -ne 0) { Fail "zipalign 失败" }

# 7. 签名（没有指定 keystore 时，自动生成一个调试用的）
if ($KeystorePath) {
    $ks = [System.IO.Path]::GetFullPath($KeystorePath)
    if (-not (Test-Path $ks)) { Fail "指定的 keystore 不存在: $ks" }
} else {
    $ks = Join-Path $RepoRoot "build\pasteport-debug.keystore"
}
if (-not (Test-Path $ks)) {
    Step "生成调试签名密钥"
    & $keytool -genkeypair -keystore $ks -storetype JKS -storepass android -keypass android `
        -alias pasteport -keyalg RSA -keysize 2048 -validity 10000 `
        -dname "CN=PastePort, OU=Dev, O=PastePort, L=NA, ST=NA, C=CN" | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "keytool 生成密钥失败" }
}

Step "apksigner (签名)"
$final = Join-Path $OutDir $OutName
$final = [System.IO.Path]::GetFullPath($final)
& $apksigner sign --ks $ks --ks-key-alias pasteport `
    --ks-pass pass:android --key-pass pass:android `
    --out $final $aligned
if ($LASTEXITCODE -ne 0) { Fail "apksigner 签名失败" }

Step "apksigner verify"
& $apksigner verify --print-certs $final
if ($LASTEXITCODE -ne 0) { Fail "签名校验失败" }

$size = [math]::Round((Get-Item $final).Length / 1KB, 1)
Write-Host ""
Write-Host "  OK  $final  ($size KB)" -ForegroundColor Green
Write-Host ""
