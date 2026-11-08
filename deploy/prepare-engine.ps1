$ErrorActionPreference = 'Stop'
$validator = Join-Path $PSScriptRoot 'verify-engine.mjs'
& node $validator
if ($LASTEXITCODE -ne 0) { throw '内核补丁或基线校验失败；未修改源码。' }
