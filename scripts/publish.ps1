# 一键发布：配置 remote、推 main、打 tag 触发 CI 出 exe + apk。
#
# 用法：
#   1. 先在网页上建一个空仓库（不要勾 README / .gitignore / LICENSE）
#   2. .\scripts\publish.ps1 -Repo lcymxm/pasteport -Tag v0.3.1
#
# 推完 tag 之后，GitHub Actions 会在 windows-latest 上重新编译两个产物并建 Release。

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Repo,
    [string]$Tag = "",
    [string]$Branch = "main"
)

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot

function Fail($msg) { Write-Host "  x $msg" -ForegroundColor Red; exit 1 }
function Step($msg) { Write-Host "  > $msg" -ForegroundColor Cyan }

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Fail "找不到 git，请先安装 Git for Windows"
}

Push-Location $RepoRoot
try {
    $dirty = git status --porcelain
    if ($dirty) {
        Write-Host "  未提交的改动：" -ForegroundColor Yellow
        $dirty | ForEach-Object { "    $_" }
        Fail "先把改动 commit 掉再发布"
    }

    $url = "https://github.com/$Repo.git"
    $existing = git remote get-url origin 2>$null
    if ($LASTEXITCODE -eq 0) {
        if ($existing -ne $url) {
            Step "更新 remote: $url"
            git remote set-url origin $url
        }
    } else {
        Step "添加 remote: $url"
        git remote add origin $url
    }

    Step "推送 $Branch"
    git push -u origin $Branch
    if ($LASTEXITCODE -ne 0) { Fail "推送失败（仓库建了吗？有推送权限吗？）" }

    if ($Tag) {
        if (git tag --list $Tag) {
            Step "tag $Tag 已存在，跳过"
        } else {
            Step "打 tag $Tag 并推送"
            git tag $Tag
            git push origin $Tag
            if ($LASTEXITCODE -ne 0) { Fail "推送 tag 失败" }
        }
        Write-Host ""
        Write-Host "  完成。GitHub Actions 正在编译 exe + apk，几分钟后看：" -ForegroundColor Green
        Write-Host "    https://github.com/$Repo/actions" -ForegroundColor Green
        Write-Host "  构建成功会自动创建 Release：https://github.com/$Repo/releases" -ForegroundColor Green
    } else {
        Write-Host ""
        Write-Host "  完成。想发版的话补一个 tag：git tag v0.3.1 && git push origin v0.3.1" -ForegroundColor Green
    }
} finally {
    Pop-Location
}
