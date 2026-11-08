param(
    [string]$ExecutablePath = (Join-Path $PSScriptRoot '..\bin\collector.exe'),
    [string]$DataDirectory = (Join-Path $env:LOCALAPPDATA 'LiveTap\data')
)
$ErrorActionPreference = 'Stop'
if (!(Test-Path -LiteralPath $ExecutablePath)) { throw '请先按 docs/development.md 构建 bin\collector.exe。' }
function Read-ProcessSecret([string]$Name, [string]$Prompt) {
    if (![Environment]::GetEnvironmentVariable($Name, 'Process')) {
        $secure = Read-Host $Prompt -AsSecureString
        $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
        try { [Environment]::SetEnvironmentVariable($Name, [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr), 'Process') }
        finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr) }
    }
}
Read-ProcessSecret 'LIVETAP_SECRET_KEY' '输入保存在密码管理器中的 Base64 主密钥（32 字节，后续启动必须相同）'
if (!(Test-Path -LiteralPath (Join-Path $DataDirectory 'collector.db'))) {
    Read-ProcessSecret 'LIVETAP_ADMIN_INITIAL_PASSWORD' '设置管理员初始密码（至少 12 位，首次登录后须修改）'
}
$env:LIVETAP_DATA_DIR = [IO.Path]::GetFullPath($DataDirectory)
if (!$env:LIVETAP_LISTEN) { $env:LIVETAP_LISTEN = '127.0.0.1:8090' }
try { & $ExecutablePath; if ($LASTEXITCODE -ne 0) { throw "采集服务退出：$LASTEXITCODE" } }
finally { Remove-Item Env:LIVETAP_ADMIN_INITIAL_PASSWORD -ErrorAction SilentlyContinue }
