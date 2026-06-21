# Store Inventory Broker

Native current-user packaged-application inventory broker selected by ADR 0001.

The Go coordinator calls this executable only when `UPDATER_NATIVE_STORE_INVENTORY=1` or `UPDATER_NATIVE_STORE_INVENTORY_DUAL_RUN=1`.

Protocol:

- stdin: JSON `InventoryRequest`
- stdout: JSON `InventoryResponse`
- argument: `--inventory`

The broker uses `Windows.Management.Deployment.PackageManager.FindPackagesForUser(string.Empty)` so enumeration is current-user scoped. It must not call PowerShell or `Get-AppxPackage -AllUsers`.

Build on a Windows SDK/.NET machine:

```powershell
dotnet publish .\native\store-inventory-broker\WindowsUpdater.StoreInventoryBroker.csproj -c Release -r win-x64 --self-contained true -p:PublishSingleFile=true
```

The resulting executable should be shipped as `WindowsUpdater.StoreInventoryBroker.exe` beside `WindowsUpdaterWebUI.exe`, or its path can be supplied through `UPDATER_STORE_INVENTORY_BROKER`.
