# build.ps1 - Kịch bản build mã máy CEO Plugin (3 Nhị phân Độc lập)

$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
Write-Host ">>> [1/5] Dang don dep thu muc dist va temp..." -ForegroundColor Cyan
$buildRoot = [IO.Path]::GetFullPath($PSScriptRoot)
foreach ($relativeBuildPath in @('dist', 'cmd/setup/dist_bin')) {
    $buildTarget = [IO.Path]::GetFullPath((Join-Path $buildRoot $relativeBuildPath))
    if (-not $buildTarget.StartsWith($buildRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Build cleanup escaped repository' }
    if (Test-Path -LiteralPath $buildTarget) {
        if ((Get-Item -LiteralPath $buildTarget).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Refusing build cleanup through a reparse point' }
        Remove-Item -LiteralPath $buildTarget -Recurse -Force
    }
}
New-Item -ItemType Directory -Path dist/bin -Force | Out-Null
New-Item -ItemType Directory -Path cmd/setup/dist_bin -Force | Out-Null

$env:GOOS = "windows"
$env:GOARCH = "amd64"

Write-Host ">>> [2/5] Dang bien dich ceo-plugin.exe (Loi CLI Engine & MCP)..." -ForegroundColor Cyan
go build -ldflags="-s -w" -o dist/bin/ceo-plugin.exe ./cmd/engine
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
if (-not (Test-Path dist/bin/ceo-plugin.exe)) {
    Write-Host "[!] Loi: Khong the build ceo-plugin.exe" -ForegroundColor Red
    exit 1
}

# Catalog descriptions must cover the embedded inventory before packaging.
go test ./cmd/engine -run '^TestCatalogCoverageAndRawOutput$' -count=1
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host ">>> [3/5] Dang bien dich ceo-plugin-uninstall.exe (Trinh Go Cai Dat Chuyen Biet)..." -ForegroundColor Cyan
go build -ldflags="-s -w" -o dist/bin/ceo-plugin-uninstall.exe ./cmd/uninstall
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
if (-not (Test-Path dist/bin/ceo-plugin-uninstall.exe)) {
    Write-Host "[!] Loi: Khong the build ceo-plugin-uninstall.exe" -ForegroundColor Red
    exit 1
}

Write-Host ">>> [4/5] Nen 2 binary vao Setup package de nhung..." -ForegroundColor Cyan
foreach ($payloadName in @('ceo-plugin.exe', 'ceo-plugin-uninstall.exe')) {
    $payloadSource = Join-Path $buildRoot ('dist/bin/' + $payloadName)
    $payloadTarget = Join-Path $buildRoot ('cmd/setup/dist_bin/' + $payloadName + '.gz')
    $inputStream = [IO.File]::OpenRead($payloadSource)
    try {
        $outputStream = [IO.File]::Create($payloadTarget)
        try {
            $gzipStream = [IO.Compression.GZipStream]::new($outputStream, [IO.Compression.CompressionLevel]::Optimal, $true)
            try { $inputStream.CopyTo($gzipStream) } finally { $gzipStream.Dispose() }
        } finally { $outputStream.Dispose() }
    } finally { $inputStream.Dispose() }
}

Write-Host ">>> [5/5] Dang bien dich ceo-plugin-setup.exe (Trinh cai dat All-in-One)..." -ForegroundColor Cyan
go build -ldflags="-s -w" -o dist/bin/ceo-plugin-setup.exe ./cmd/setup
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

# Duy tri cmd/setup/dist_bin de phuc vu //go:embed va test suites
Write-Host ">>> [Note] Duy tri cmd/setup/dist_bin phuc vu //go:embed va test suites..." -ForegroundColor DarkGray

if (-not (Test-Path dist/bin/ceo-plugin-setup.exe)) {
    Write-Host "[!] Loi: Khong the build ceo-plugin-setup.exe" -ForegroundColor Red
    exit 1
}

Write-Host ">>> BUILD THANH CONG RUC RO TRON GOI 3 NHI PHAN CEO-PLUGIN!" -ForegroundColor Green
Get-ChildItem -Recurse dist/bin
