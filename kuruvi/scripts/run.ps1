# run.ps1 - Kuruvi Start Script for Windows

Write-Host "Starting Kuruvi Messenger..." -ForegroundColor Cyan
Set-Location backend
go run main.go
