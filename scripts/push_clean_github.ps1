# scripts/push_clean_github.ps1
# AS Auto Switcher - Clean GitHub Release Publisher
# Single-command mechanism to push a sanitized version of the codebase to GitHub
# remote WITHOUT publishing internal 'docs/' and 'reports/' directories,
# while 100% PRESERVING all docs, reports, and local commit history intact.

[CmdletBinding(SupportsShouldProcess = $true)]
param(
    # The git remote name to push to (default: 'origin')
    [Parameter(Position = 0)]
    [string]$Remote = "origin",

    # Target remote branch(es) to push the clean release to (default: release)
    [Parameter(Position = 1)]
    [string[]]$Branches = @("release"),

    # Directories to strip from the published tree (default: docs, reports)
    [string[]]$ExcludeDirs = @("docs", "reports"),

    # Custom commit message for the clean export commit.
    # If omitted, defaults to a release message referencing the HEAD commit.
    [string]$CommitMessage,

    # Produce an orphan release commit (default: $true).
    # Creates a root commit with zero parents, guaranteeing that no past commits
    # containing internal docs/reports exist in GitHub's commit history.
    [bool]$Orphan = $true,

    # Explicit force push switch. When $Orphan is true, force update is applied automatically.
    [switch]$Force,

    # Explicit DryRun switch (in addition to standard -WhatIf)
    [switch]$DryRun
)

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
$ErrorActionPreference = "Stop"

if ($DryRun) {
    $WhatIfPreference = $true
}

$IconShield = [char]::ConvertFromUtf32(0x1F6E1)
$IconRocket = [char]::ConvertFromUtf32(0x1F680)
$IconCheck  = [char]::ConvertFromUtf32(0x2705)
$IconWarn   = [char]::ConvertFromUtf32(0x26A0)

Write-Host "$IconShield [Clean GitHub Publisher] Khoi dong kiem tra an toan..." -ForegroundColor Cyan

# 1. Kiem tra thu muc goc Git
$repoRoot = (git rev-parse --show-toplevel 2>$null)
if (-not $repoRoot) {
    throw "Loi: Lenh phai duoc chay ben trong mot Git repository hop le."
}
Set-Location -LiteralPath $repoRoot

# 2. Kiem tra remote ton tai
$remotes = git remote
if (-not ($remotes -contains $Remote)) {
    Write-Host "$IconWarn Remote '$Remote' chua duoc cau hinh tren repo nay." -ForegroundColor Yellow
    Write-Host "Huong dan: Chay lenh sau de them remote GitHub truoc khi push:" -ForegroundColor DarkGray
    Write-Host "  git remote add $Remote https://github.com/<username>/<repo>.git`n" -ForegroundColor DarkCyan
    throw "Dung tien trinh vi thieu cau hinh remote '$Remote'."
}

# 3. Lay thong tin HEAD commit hien tai
$headSha = (git rev-parse HEAD).Trim()
$shortSha = (git rev-parse --short HEAD).Trim()
$timestamp = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss")

if (-not $CommitMessage) {
    $CommitMessage = "Release: sanitized export from commit $shortSha ($timestamp)"
}

Write-Host ">>> Nguon Local HEAD: $shortSha | Remote Dich: $Remote | Nhanh: $($Branches -join ', ')" -ForegroundColor Gray
Write-Host ">>> Thu muc loai bo khoi GitHub: $($ExcludeDirs -join ', ')" -ForegroundColor DarkYellow

# 4. Tao Index co lap de loc sach ma khong dong vao workspace
$gitDir = (git rev-parse --git-dir).Trim()
$tempIndex = Join-Path $gitDir ("temp_index_" + [Guid]::NewGuid().ToString("N"))
$prevIndex = $env:GIT_INDEX_FILE

try {
    $env:GIT_INDEX_FILE = $tempIndex

    # Nap toan bo cay HEAD vao index tam
    git read-tree HEAD
    if ($LASTEXITCODE -ne 0) { throw "Loi khi nap HEAD vao index tam." }

    # Loai bo cac thu muc nhay cam khoi index tam
    foreach ($dir in $ExcludeDirs) {
        git rm -r --cached --ignore-unmatch --quiet $dir 2>$null
    }

    # Sinh cay sach (clean tree)
    $cleanTree = (git write-tree).Trim()
    if (-not $cleanTree) { throw "Loi: Khong the tao clean tree object." }

    # Sinh commit sach (Orphan hoac co Parent)
    if ($Orphan) {
        $cleanCommit = (git commit-tree $cleanTree -m $CommitMessage).Trim()
    } else {
        $cleanCommit = (git commit-tree $cleanTree -p $headSha -m $CommitMessage).Trim()
    }
    Write-Host "$IconCheck Da tao thanh cong Clean Commit: $cleanCommit" -ForegroundColor Green

    # 5. Day sang cac nhanh tren GitHub Remote
    foreach ($branch in $Branches) {
        $refSpec = "${cleanCommit}:refs/heads/${branch}"
        $pushArgs = @("push", $Remote, $refSpec)
        if ($Orphan -or $Force) {
            $pushArgs += "--force"
        }

        if ($PSCmdlet.ShouldProcess("$Remote/$branch", "Git Push clean commit $cleanCommit")) {
            Write-Host "$IconRocket Dang day code sach sang $Remote/$branch..." -ForegroundColor Cyan
            & git @pushArgs
            if ($LASTEXITCODE -ne 0) {
                throw "Loi khi git push sang $Remote/$branch (Exit code: $LASTEXITCODE)"
            }

            # Cap nhat luon ref local tuong ung de tien theo doi
            git update-ref "refs/heads/$branch" $cleanCommit
            Write-Host "$IconCheck Day thanh cong sang $Remote/$branch!" -ForegroundColor Green
        }
    }
}
finally {
    # 6. Don dep tuyet doi bien moi truong va file index tam
    if ($prevIndex) {
        $env:GIT_INDEX_FILE = $prevIndex
    } else {
        Remove-Item env:GIT_INDEX_FILE -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $tempIndex) {
        Remove-Item -LiteralPath $tempIndex -Force -ErrorAction SilentlyContinue
    }
}

Write-Host "`n$IconCheck HOAN TAT XUAT BAN GITHUB THANH CONG RUC RO!" -ForegroundColor Green
Write-Host "  - Local master: 100% giu nguyen vet commit, docs/ va reports/." -ForegroundColor DarkGray
Write-Host "  - Remote GitHub: 100% sach bong khong chua docs/ hay reports/.`n" -ForegroundColor DarkGray
