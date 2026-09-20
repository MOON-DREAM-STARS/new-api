param(
    [switch]$SkipBuild,
    [switch]$SmokeOnly,
    [string[]]$Renderers = @('baseline', 'kasmvnc'),
    [int[]]$Widths = @(1280, 2048)
)

$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
$artifactDir = Join-Path $root 'artifacts'
$serverImage = 'web-workspace-spike-kasmvnc:20260920'
$clientImage = 'web-workspace-spike-client:20260920'
$networkName = 'wsp-kasmvnc-spike-net'
$serverName = 'wsp-kasmvnc-spike-server'
$clientName = 'wsp-kasmvnc-spike-client'
$label = 'com.dreamstars.spike=kasmvnc-20260920'

New-Item -ItemType Directory -Force -Path $artifactDir | Out-Null
$transcript = Join-Path $artifactDir 'run-spike.log'
"[$(Get-Date -Format o)] start renderers=$($Renderers -join ',') widths=$($Widths -join ',') smoke=$SmokeOnly" |
    Set-Content -LiteralPath $transcript -Encoding utf8

function Invoke-Logged {
    param([string]$Label, [string]$File, [string[]]$Arguments)
    $line = "COMMAND $Label`n docker $($Arguments -join ' ')"
    Add-Content -LiteralPath $transcript -Value $line -Encoding utf8
    & docker @Arguments 2>&1 | Tee-Object -FilePath $File -Append
    if ($LASTEXITCODE -ne 0) { throw "docker $($Arguments -join ' ') failed with exit code $LASTEXITCODE" }
}

function Remove-OwnContainer {
    param([string]$Name)
    docker inspect $Name *> $null
    if ($LASTEXITCODE -ne 0) { return }
    $labels = docker inspect -f '{{index .Config.Labels "com.dreamstars.spike"}}' $Name 2>$null
    if ($labels -eq 'kasmvnc-20260920') {
        Add-Content -LiteralPath $transcript -Value "COMMAND cleanup`n docker rm -f $Name" -Encoding utf8
        docker rm -f $Name | Out-Null
    } else {
        throw "refusing to remove container $Name because its spike label does not match"
    }
}

function Get-CgroupSample {
    param([string]$Name)
    $remote = "set -eu; ts=`$(date +%s%3N); cpu=`$(awk '/^usage_usec /{print `$2}' /sys/fs/cgroup/cpu.stat); mem=`$(cat /sys/fs/cgroup/memory.current); anon=`$(awk '/^anon /{print `$2}' /sys/fs/cgroup/memory.stat); printf '%s|%s|%s|%s\n' `"`$ts`" `"`$cpu`" `"`$mem`" `"`$anon`""
    $line = docker exec $Name sh -lc $remote
    if ($LASTEXITCODE -ne 0) { throw "failed to sample cgroup for $Name" }
    $parts = $line.Trim().Split('|')
    return [pscustomobject]@{
        tsMs = [long]$parts[0]
        cpuUsec = [long]$parts[1]
        memoryCurrentBytes = [long]$parts[2]
        anonBytes = [long]$parts[3]
    }
}

function Get-ScenarioMetrics {
    param([object[]]$Samples, [long]$StartMs, [long]$EndMs)
    $selected = @($Samples | Where-Object { $_.tsMs -ge ($StartMs - 1000) -and $_.tsMs -le ($EndMs + 1000) } | Sort-Object tsMs)
    if ($selected.Count -lt 2) {
        return [pscustomobject]@{ cpuPeakPercent = $null; cpuAveragePercent = $null; cpuTicksAtOrAbove95 = $null; rssPeakMiB = $null; anonPeakMiB = $null; sampleCount = $selected.Count }
    }
    $intervalCpus = @()
    for ($i = 1; $i -lt $selected.Count; $i++) {
        $prev = $selected[$i - 1]
        $cur = $selected[$i]
        $wallUsec = ($cur.tsMs - $prev.tsMs) * 1000
        if ($wallUsec -le 0) { continue }
        $mid = [long](($cur.tsMs + $prev.tsMs) / 2)
        if ($mid -lt $StartMs -or $mid -gt $EndMs) { continue }
        $intervalCpus += (($cur.cpuUsec - $prev.cpuUsec) / $wallUsec) * 100.0
    }
    $nearestStart = $selected | Sort-Object { [Math]::Abs($_.tsMs - $StartMs) } | Select-Object -First 1
    $nearestEnd = $selected | Sort-Object { [Math]::Abs($_.tsMs - $EndMs) } | Select-Object -First 1
    $avg = $null
    if ($nearestEnd.tsMs -gt $nearestStart.tsMs) {
        $avg = (($nearestEnd.cpuUsec - $nearestStart.cpuUsec) / (($nearestEnd.tsMs - $nearestStart.tsMs) * 1000.0)) * 100.0
    }
    $rssPeak = ($selected | Measure-Object memoryCurrentBytes -Maximum).Maximum
    $anonPeak = ($selected | Measure-Object anonBytes -Maximum).Maximum
    $peak = $null
    if ($intervalCpus.Count -gt 0) { $peak = [Math]::Round(($intervalCpus | Measure-Object -Maximum).Maximum, 2) }
    [pscustomobject]@{
        cpuPeakPercent = $peak
        cpuAveragePercent = if ($null -ne $avg) { [Math]::Round($avg, 2) } else { $null }
        cpuTicksAtOrAbove95 = @($intervalCpus | Where-Object { $_ -ge 95 }).Count
        rssPeakMiB = [Math]::Round($rssPeak / 1MB, 2)
        anonPeakMiB = [Math]::Round($anonPeak / 1MB, 2)
        sampleCount = $selected.Count
    }
}

function Start-SpikeServer {
    param([string]$Renderer, [int]$Width, [int]$Height)
    Remove-OwnContainer $serverName
    if ($Renderer -eq 'baseline') { $alias = 'baseline' } else { $alias = 'kasm' }
    if ($Renderer -eq 'baseline') { $entrypoint = '/opt/spike/scripts/entrypoint-baseline.sh' } else { $entrypoint = '/opt/spike/scripts/entrypoint-kasmvnc.sh' }
    $args = @(
        'run', '-d', '--rm', '--name', $serverName, '--label', $label,
        '--network', $networkName, '--network-alias', $alias,
        '--cpus=1', '--memory=1g', '--pids-limit=256', '--shm-size=256m',
        '-e', "WW_SCREEN_WIDTH=$Width", '-e', "WW_SCREEN_HEIGHT=$Height",
        $serverImage, $entrypoint
    )
    Invoke-Logged -Label "run-$Renderer-$($Width)x$Height" -File (Join-Path $artifactDir "$Renderer-$Width`x$Height-docker-run.log") -Arguments $args | Out-Null
    $deadline = (Get-Date).AddSeconds(60)
    while ((Get-Date) -lt $deadline) {
        $status = docker inspect -f '{{.State.Status}}' $serverName 2>$null
        if ($status -ne 'running') { throw "spike server $serverName exited" }
        if ($Renderer -eq 'baseline') {
            docker exec $serverName sh -lc 'pgrep -x chromium >/dev/null && curl -fsS http://127.0.0.1:6080/vnc.html >/dev/null' 2>$null
        } else {
            docker exec $serverName sh -lc 'pgrep -x chromium >/dev/null && curl -kfsS https://127.0.0.1:3000/ >/dev/null' 2>$null
        }
        if ($LASTEXITCODE -eq 0) { return }
        Start-Sleep -Milliseconds 500
    }
    docker logs $serverName 2>&1 | Add-Content -LiteralPath $transcript -Encoding utf8
    throw "timed out waiting for $Renderer readiness"
}

if (-not $SkipBuild) {
    Invoke-Logged -Label 'build-server' -File (Join-Path $artifactDir 'build-server.log') -Arguments @('build', '-f', (Join-Path $root 'Dockerfile.server'), '-t', $serverImage, $root)
    Invoke-Logged -Label 'build-client' -File (Join-Path $artifactDir 'build-client.log') -Arguments @('build', '-f', (Join-Path $root 'Dockerfile.client'), '-t', $clientImage, $root)
}

docker network inspect $networkName *> $null
if ($LASTEXITCODE -ne 0) {
    Add-Content -LiteralPath $transcript -Value "COMMAND network-create`n docker network create --internal $networkName" -Encoding utf8
    docker network create --internal $networkName | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "failed to create internal network $networkName" }
}

Remove-OwnContainer $clientName
$clientRunArgs = @(
    'run', '-d', '--rm', '--name', $clientName, '--label', $label,
    '--network', $networkName, '--network-alias', 'client',
    '--cpus=2', '--memory=2g', '--shm-size=512m',
    '-v', "${artifactDir}:/out",
    '--entrypoint', 'sleep', $clientImage, 'infinity'
)
Invoke-Logged -Label 'run-client' -File (Join-Path $artifactDir 'client-docker-run.log') -Arguments $clientRunArgs | Out-Null

try {
    foreach ($renderer in $Renderers) {
        foreach ($width in $Widths) {
            if ($width -eq 2048) { $height = 900 } else { $height = 720 }
            Start-SpikeServer -Renderer $renderer -Width $width -Height $height

            if ($SmokeOnly) {
                $smoke = docker exec $clientName node -e "const fs=require('fs'); console.log(fs.readFileSync('/sys/class/net/eth0/statistics/rx_bytes','utf8').trim())"
                Add-Content -LiteralPath $transcript -Value "SMOKE $renderer ${width}x${height}: $smoke" -Encoding utf8
                continue
            }

            $sampleFile = Join-Path $artifactDir "cgroup-$renderer-$width`x$height.jsonl"
            Remove-Item -LiteralPath $sampleFile -Force -ErrorAction SilentlyContinue
            $remoteSample = "set -eu; ts=`$(date +%s%3N); cpu=`$(awk '/^usage_usec /{print `$2}' /sys/fs/cgroup/cpu.stat); mem=`$(cat /sys/fs/cgroup/memory.current); anon=`$(awk '/^anon /{print `$2}' /sys/fs/cgroup/memory.stat); printf '%s|%s|%s|%s\n' `"`$ts`" `"`$cpu`" `"`$mem`" `"`$anon`""
            $pollJob = Start-Job -ArgumentList $serverName, $sampleFile, $remoteSample -ScriptBlock {
                param($container, $file, $remote)
                while ($true) {
                    $line = & docker exec $container sh -lc $remote 2>$null
                    if ($LASTEXITCODE -eq 0 -and $line) {
                        $p = $line.Trim().Split('|')
                        [pscustomobject]@{ tsMs = [long]$p[0]; cpuUsec = [long]$p[1]; memoryCurrentBytes = [long]$p[2]; anonBytes = [long]$p[3] } |
                            ConvertTo-Json -Compress | Add-Content -LiteralPath $file
                    }
                    Start-Sleep -Seconds 1
                }
            }

            if ($renderer -eq 'baseline') {
                $url = 'http://baseline:6080/vnc.html?autoconnect=1&resize=scale&reconnect=0&show_dot=1'
            } else {
                $url = 'https://kasm:3000/vnc.html?autoconnect=1&resize=scale'
            }
            $clientLog = Join-Path $artifactDir "client-$renderer-$width`x$height.log"
            $measureArgs = @(
                'exec', $clientName, 'node', '/opt/harness/measure.js',
                '--renderer', $renderer, '--url', $url,
                '--width', $width, '--height', $height, '--out', '/out'
            )
            $measureCommand = "docker $($measureArgs -join ' ')"
            Add-Content -LiteralPath $transcript -Value "COMMAND measure-$renderer-$($width)x$height`n$measureCommand" -Encoding utf8
            docker @measureArgs 2>&1 | Tee-Object -FilePath $clientLog
            $measureExit = $LASTEXITCODE

            Stop-Job $pollJob -ErrorAction SilentlyContinue
            Remove-Job $pollJob -Force -ErrorAction SilentlyContinue
            if ($measureExit -ne 0) { throw "measurement failed for $renderer ${width}x${height}; see $clientLog" }

            $resultPath = Join-Path $artifactDir "$renderer-$width`x$height.json"
            $raw = Get-Content -LiteralPath $resultPath -Raw | ConvertFrom-Json
            $samples = @()
            if (Test-Path -LiteralPath $sampleFile) {
                $samples = @(Get-Content -LiteralPath $sampleFile | Where-Object { $_.Trim() } | ForEach-Object { $_ | ConvertFrom-Json })
            }
            $scenarios = foreach ($scenario in $raw.scenarios) {
                $metrics = Get-ScenarioMetrics -Samples $samples -StartMs ([long]$scenario.startMs) -EndMs ([long]$scenario.endMs)
                [pscustomobject]@{
                    name = $scenario.name
                    durationSeconds = [Math]::Round($scenario.durationMs / 1000.0, 3)
                    serverToClientBytes = [long]$scenario.serverToClientBytes
                    actionError = $scenario.actionError
                    cpuPeakPercent = $metrics.cpuPeakPercent
                    cpuAveragePercent = $metrics.cpuAveragePercent
                    cpuIntervalTicksAtOrAbove95Percent = $metrics.cpuTicksAtOrAbove95
                    rssPeakMiB = $metrics.rssPeakMiB
                    anonPeakMiB = $metrics.anonPeakMiB
                    cgroupSampleCount = $metrics.sampleCount
                }
            }
            $combined = [pscustomobject]@{
                renderer = $renderer
                width = $width
                height = $height
                limits = @('--cpus=1', '--memory=1g', '--pids-limit=256', 'no GPU')
                url = $url
                canvas = $raw.canvas
                screenshotPath = $raw.screenshotPath
                scenarios = @($scenarios)
                rawSamples = $sampleFile
            }
            $combinedPath = Join-Path $artifactDir "combined-$renderer-$width`x$height.json"
            $combined | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $combinedPath -Encoding utf8
            Add-Content -LiteralPath $transcript -Value "RESULT $combinedPath`n$($combined | ConvertTo-Json -Depth 8 -Compress)" -Encoding utf8
        }
    }
}
finally {
    Remove-OwnContainer $serverName
    Remove-OwnContainer $clientName
    Add-Content -LiteralPath $transcript -Value "[$(Get-Date -Format o)] finished" -Encoding utf8
}
