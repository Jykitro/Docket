# Plandillo: installs plandillo-cli from a GitHub release, checks its SHA-256 and signs it in to PLANDILLO_URL.
# PLANDILLO_BIN_DIR picks the folder (default %LOCALAPPDATA%\Programs\plandillo-cli); PLANDILLO_CLI_VERSION pins a release.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$repo = 'huntly-digital/plandillo'
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$file = "plandillo-cli_windows_$arch.zip"
$base = if ($env:PLANDILLO_CLI_VERSION) { "https://github.com/$repo/releases/download/v$($env:PLANDILLO_CLI_VERSION.TrimStart('v'))" } else { "https://github.com/$repo/releases/latest/download" }
$dir = if ($env:PLANDILLO_BIN_DIR) { $env:PLANDILLO_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\plandillo-cli' }
$tmp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  Invoke-WebRequest -UseBasicParsing -Uri "$base/$file" -OutFile (Join-Path $tmp $file)
  Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')
  $want = Get-Content (Join-Path $tmp 'checksums.txt') | ForEach-Object { $f = $_ -split '\s+'; if ($f[1] -eq $file) { $f[0] } } | Select-Object -First 1
  $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $file)).Hash.ToLower()
  if (-not $want -or $want -ne $got) { throw "The checksum of $file does not match checksums.txt; nothing was installed." }
  Expand-Archive -Path (Join-Path $tmp $file) -DestinationPath (Join-Path $tmp 'out') -Force
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  Copy-Item (Join-Path (Join-Path $tmp 'out') 'plandillo-cli.exe') (Join-Path $dir 'plandillo-cli.exe') -Force
} finally {
  Remove-Item -Recurse -Force $tmp
}
$exe = Join-Path $dir 'plandillo-cli.exe'
Write-Host "Installed $exe $(& $exe version)"
if ($PSVersionTable.PSEdition -eq 'Desktop' -or $IsWindows) {
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (-not (($userPath -split ';') -contains $dir)) {
    [Environment]::SetEnvironmentVariable('Path', $(if ($userPath) { "$userPath;$dir" } else { $dir }), 'User')
    $env:Path = "$env:Path;$dir"
    Write-Host "Added $dir to your PATH; open a new terminal to use plandillo-cli everywhere."
  }
}
if (-not $env:PLANDILLO_URL) {
  Write-Host 'Sign in to your board with: plandillo-cli login https://your-board'
  return
}
& $exe login $env:PLANDILLO_URL
if ($LASTEXITCODE -ne 0) { throw "Sign in later with: plandillo-cli login $($env:PLANDILLO_URL)" }
