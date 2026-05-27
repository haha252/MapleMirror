[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$extensions = @('.go', '.sql', '.yaml', '.yml', '.ps1', '.bat', '.sh', '.js', '.css', '.html')
$violations = @()

Get-ChildItem -LiteralPath $root -Recurse -File | ForEach-Object {
    $path = $_.FullName
    $relative = $path.Substring($root.Length).TrimStart('\', '/')
    if ($relative -match '^(\.git|\.cache|dist)[\\/]' -or
        $_.Extension -eq '.md' -or
        $extensions -notcontains $_.Extension.ToLowerInvariant()) {
        return
    }
    $lineCount = (Get-Content -LiteralPath $path -Encoding UTF8).Count
    if ($lineCount -gt 250) {
        $violations += "$relative ($lineCount lines)"
    }
}

if ($violations.Count -gt 0) {
    $message = ConvertFrom-Json '"\u4ee5\u4e0b\u6e90\u7801\u6216\u8d44\u6e90\u6587\u4ef6\u8d85\u8fc7 250 \u884c\uff0c\u8bf7\u6309\u804c\u8d23\u62c6\u5206\uff1a"'
    Write-Error ($message + "`n" + ($violations -join "`n"))
    exit 1
}

$success = ConvertFrom-Json '"\u6587\u4ef6\u884c\u6570\u68c0\u67e5\u901a\u8fc7\uff1a\u6e90\u7801\u548c\u524d\u7aef\u8d44\u6e90\u5747\u672a\u8d85\u8fc7 250 \u884c\u3002"'
Write-Host $success
