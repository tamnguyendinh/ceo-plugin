package installstate

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Bootstrap owns the final exit status and temporary worker cleanup. Paths are
// base64 data, never interpolated PowerShell code. The worker owns mutation.
func BootstrapScript(uninstaller string, quiet bool, parentPID int, parentStart int64, expectedGeneration string) string {
	pathData := base64.StdEncoding.EncodeToString([]byte(uninstaller))
	script := `$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
function FileDigest([string]$path) {
    $algorithm = [Security.Cryptography.SHA256]::Create()
    $stream = [IO.File]::OpenRead($path)
    try { return [BitConverter]::ToString($algorithm.ComputeHash($stream)).Replace('-', '') }
    finally { $stream.Dispose(); $algorithm.Dispose() }
}
$sourceExe = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('__PATH__'))
$quietMode = __QUIET__
$waitPid = __PID__
$waitStart = __START__
$expectedGeneration = '__GENERATION__'
$stage = $null
$code = 1
$summary = ''
try {
    if ($waitPid -ne 0) {
        $parentProcess = $null
        try { $parentProcess = [Diagnostics.Process]::GetProcessById($waitPid) } catch [ArgumentException] { }
        if ($null -ne $parentProcess -and $parentProcess.StartTime.ToUniversalTime().ToFileTimeUtc() -eq $waitStart) {
            if (-not $parentProcess.WaitForExit(30000)) { throw 'Original uninstaller did not exit within 30 seconds' }
        }
    }
    $original = [IO.FileInfo]::new($sourceExe)
    if (-not $original.Exists) { throw 'Installed uninstaller is missing' }
    if ($original.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Refusing linked uninstaller' }
    $stamp = $original.LastWriteTimeUtc.Ticks.ToString([Globalization.CultureInfo]::InvariantCulture)
    $digest = FileDigest $sourceExe
    $identityPath = [IO.Path]::Combine($original.Directory.Parent.FullName, '.installation-id')
    $generation = 'legacy'
    if ([IO.File]::Exists($identityPath)) { $generation = [IO.File]::ReadAllText($identityPath).Trim() }
    if ($generation -ne 'legacy' -and $generation -notmatch '^[a-f0-9]{32}$') { throw 'Invalid installation identity' }
    if ($expectedGeneration.Length -ne 0 -and $expectedGeneration -ne $generation) { throw 'Installation changed after launch; retry uninstall' }
    $stage = [IO.Path]::Combine([IO.Path]::GetTempPath(), ('ceo-uninstall-' + [Guid]::NewGuid().ToString('N')))
    $null = [IO.Directory]::CreateDirectory($stage)
    $worker = [IO.Path]::Combine($stage, 'ceo-plugin-uninstall.exe')
    [IO.File]::Copy($sourceExe, $worker, $false)
    if ((FileDigest $worker) -ne $digest) { throw 'Uninstaller copy hash mismatch' }
    $start = New-Object System.Diagnostics.ProcessStartInfo
    $start.FileName = $worker
    $start.Arguments = '--internal-remove ' + $digest + ' ' + $stamp + ' ' + $generation
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    $start.StandardOutputEncoding = [Text.UTF8Encoding]::new($false)
    $start.StandardErrorEncoding = [Text.UTF8Encoding]::new($false)
    $workerProcess = [Diagnostics.Process]::Start($start)
    $stdoutRead = $workerProcess.StandardOutput.ReadToEndAsync()
    $stderrRead = $workerProcess.StandardError.ReadToEndAsync()
    if (-not $workerProcess.WaitForExit(30000)) {
        $workerProcess.Kill()
        $workerProcess.WaitForExit()
        throw 'Uninstall timed out; it may be partial. Retry after checking the installation.'
    }
    $code = $workerProcess.ExitCode
    $summary = ($stdoutRead.Result + $stderrRead.Result).Trim()
    $workerProcess.Dispose()
    if ($code -ne 0 -and $summary.Length -eq 0) { $summary = 'Uninstaller failed without a result' }
} catch {
    $code = 1
    $summary = $_.Exception.Message
} finally {
    if ($null -ne $stage) {
        try {
            $worker = [IO.Path]::Combine($stage, 'ceo-plugin-uninstall.exe')
            if ([IO.File]::Exists($worker)) { [IO.File]::Delete($worker) }
            [IO.Directory]::Delete($stage, $false)
        } catch {
            $code = 1
            $summary += [Environment]::NewLine + 'Temporary cleanup failed: ' + $stage + ' : ' + $_.Exception.Message
        }
    }
}
if ($code -eq 0) { [Console]::Out.WriteLine($summary) } else { [Console]::Error.WriteLine($summary) }
if (-not $quietMode) {
    $null = [Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms')
    $icon = [System.Windows.Forms.MessageBoxIcon]::Information
    if ($code -ne 0) { $icon = [System.Windows.Forms.MessageBoxIcon]::Error }
    $null = [System.Windows.Forms.MessageBox]::Show($summary, 'CEO Plugin Uninstall', [System.Windows.Forms.MessageBoxButtons]::OK, $icon)
}
exit $code
`
	quietValue := "$false"
	if quiet {
		quietValue = "$true"
	}
	// The identity is data too, although normal callers provide only a validated hex token.
	genData := base64.StdEncoding.EncodeToString([]byte(expectedGeneration))
	script = strings.ReplaceAll(script, "'__GENERATION__'", "[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('"+genData+"'))")
	return strings.NewReplacer("__PATH__", pathData, "__QUIET__", quietValue, "__PID__", strconv.Itoa(parentPID), "__START__", strconv.FormatInt(parentStart, 10)).Replace(script)
}

func EncodedScript(script string) string {
	units := utf16.Encode([]rune(script))
	data := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(data[i*2:], v)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func PowerShellPath() (string, error) {
	root := os.Getenv("SystemRoot")
	if root == "" {
		return "", fmt.Errorf("SystemRoot is unresolved")
	}
	path := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

func UninstallCommand(uninstaller string, quiet bool) (string, error) {
	exe, err := PowerShellPath()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`"%s" -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand %s`, exe, EncodedScript(BootstrapScript(uninstaller, quiet, 0, 0, ""))), nil
}
