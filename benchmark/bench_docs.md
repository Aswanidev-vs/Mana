# Benchmark Folder

This folder centralizes benchmark documentation and runner commands for the Mana framework.

## Why The Go Benchmark Files Are Not Moved Here

Go benchmark and test files must live next to the packages they test.

For this repository, that means benchmark source files remain in:

- [app_benchmark_test.go](/g:/Mana/app_benchmark_test.go)
- [load_profile_test.go](/g:/Mana/load_profile_test.go)
- [websocket_e2e_benchmark_test.go](/g:/Mana/websocket_e2e_benchmark_test.go)
- [manager_benchmark_test.go](/g:/Mana/room/manager_benchmark_test.go)
- [hub_benchmark_test.go](/g:/Mana/signaling/hub_benchmark_test.go)
- [manager_benchmark_test.go](/g:/Mana/rtc/manager_benchmark_test.go)
- [router_benchmark_test.go](/g:/Mana/rtc/router_benchmark_test.go)

If these files were moved into this folder, many of the package-local and unexported benchmark targets would no longer compile or test correctly.

## Files In This Folder

- [benchmark.md](/g:/Mana/benchmark/benchmark.md)
  Full benchmark report and measured results.
- [run.ps1](/g:/Mana/benchmark/run.ps1)
  Convenience script for validation, benchmark runs, and load-profile execution.

## Typical Usage

```powershell
powershell -ExecutionPolicy Bypass -File .\benchmark\run.ps1
```
