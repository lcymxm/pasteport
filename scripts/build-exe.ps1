# 构建 Windows 单文件 exe。
# 用法： .\scripts\build-exe.ps1            （默认输出 dist\PastePort.exe）
#        .\scripts\build-exe.ps1 -Arch arm64

[CmdletBinding()]
param(
    [ValidateSet("amd64", "arm64")]
    [string]$Arch = "amd64",
    [string]$OutDir = ""
)

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot
if (-not $OutDir) { $OutDir = Join-Path $RepoRoot "dist" }
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$out = Join-Path $OutDir "PastePort.exe"
if ($Arch -ne "amd64") { $out = Join-Path $OutDir "PastePort-$Arch.exe" }

Write-Host ""
Write-Host "  PastePort exe 构建 (windows/$Arch)" -ForegroundColor White
Write-Host "  -----------------------------"

Push-Location $RepoRoot
try {
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = $Arch
    & go build -trimpath -ldflags "-s -w -X main.buildArch=$Arch" -o $out ./cmd/pasteport
    if ($LASTEXITCODE -ne 0) { throw "go build 失败" }
} finally {
    Pop-Location
}

$size = [math]::Round((Get-Item $out).Length / 1MB, 2)
Write-Host ""
Write-Host "  OK  $out  ($size MB)" -ForegroundColor Green
Write-Host ""
