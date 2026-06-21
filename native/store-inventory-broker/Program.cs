using System.Security.Principal;
using System.Text.Json;
using System.Text.Json.Serialization;
using Windows.ApplicationModel;
using Windows.Management.Deployment;

if (args.Length != 1 || args[0] != "--inventory")
{
    Console.Error.WriteLine("Usage: WindowsUpdater.StoreInventoryBroker.exe --inventory");
    Environment.Exit(2);
}

var options = new JsonSerializerOptions
{
    PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
    DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
    WriteIndented = false
};

try
{
    var request = JsonSerializer.Deserialize<InventoryRequest>(Console.In.ReadToEnd(), options)
        ?? throw new InvalidOperationException("Missing inventory request.");
    if (request.ProtocolVersion != 1)
    {
        throw new InvalidOperationException($"Unsupported protocol version {request.ProtocolVersion}.");
    }

    var currentSid = WindowsIdentity.GetCurrent().User?.Value ?? "";
    if (!string.Equals(currentSid, request.UserSid, StringComparison.OrdinalIgnoreCase))
    {
        throw new InvalidOperationException("Broker user SID does not match request user SID.");
    }

    var started = DateTimeOffset.UtcNow;
    var manager = new PackageManager();
    var records = new List<PackageRecord>();
    foreach (var package in manager.FindPackagesForUser(string.Empty))
    {
        records.Add(PackageRecord.FromPackage(currentSid, package));
    }

    var response = new InventoryResponse(
        ProtocolVersion: 1,
        ScanId: request.ScanId,
        UserSid: currentSid,
        StartedAt: started.ToString("O"),
        CompletedAt: DateTimeOffset.UtcNow.ToString("O"),
        Complete: true,
        Partial: false,
        Error: null,
        Records: records);
    Console.WriteLine(JsonSerializer.Serialize(response, options));
}
catch (Exception ex)
{
    var fallbackRequest = TryReadRequestFromArgs();
    var response = new InventoryResponse(
        ProtocolVersion: 1,
        ScanId: fallbackRequest.ScanId,
        UserSid: fallbackRequest.UserSid,
        StartedAt: DateTimeOffset.UtcNow.ToString("O"),
        CompletedAt: DateTimeOffset.UtcNow.ToString("O"),
        Complete: false,
        Partial: true,
        Error: ex.Message,
        Records: Array.Empty<PackageRecord>());
    Console.WriteLine(JsonSerializer.Serialize(response, options));
    Environment.Exit(1);
}

static InventoryRequest TryReadRequestFromArgs()
{
    return new InventoryRequest(1, "", WindowsIdentity.GetCurrent().User?.Value ?? "");
}

public sealed record InventoryRequest(int ProtocolVersion, string ScanId, string UserSid);

public sealed record InventoryResponse(
    int ProtocolVersion,
    string ScanId,
    string UserSid,
    string StartedAt,
    string CompletedAt,
    bool Complete,
    bool Partial,
    string? Error,
    IReadOnlyList<PackageRecord> Records);

public sealed record PackageRecord(
    string UserSid,
    string PackageFamilyName,
    string PackageFullName,
    string IdentityName,
    string Publisher,
    string PublisherId,
    PackageVersionRecord Version,
    string ProcessorArchitecture,
    string InstallLocation,
    string PackageType,
    string Classification,
    bool IsFramework,
    bool IsResourcePackage,
    bool IsOptional,
    bool IsBundle,
    bool IsDevelopmentMode,
    bool IsStaged,
    PackageStatusRecord Status,
    string DisplayName)
{
    public static PackageRecord FromPackage(string userSid, Package package)
    {
        var id = package.Id;
        var isResource = package.IsResourcePackage;
        var isFramework = package.IsFramework;
        var isOptional = package.IsOptional;
        var isBundle = package.IsBundle;
        var classification = isResource ? "resource" :
            isFramework ? "framework" :
            isOptional ? "optional" :
            isBundle ? "bundle" :
            "main";

        return new PackageRecord(
            UserSid: userSid,
            PackageFamilyName: id.FamilyName,
            PackageFullName: id.FullName,
            IdentityName: id.Name,
            Publisher: id.Publisher,
            PublisherId: id.PublisherId,
            Version: new PackageVersionRecord(id.Version.Major, id.Version.Minor, id.Version.Build, id.Version.Revision),
            ProcessorArchitecture: id.Architecture.ToString(),
            InstallLocation: package.InstalledLocation?.Path ?? "",
            PackageType: package.GetType().FullName ?? "Windows.ApplicationModel.Package",
            Classification: classification,
            IsFramework: isFramework,
            IsResourcePackage: isResource,
            IsOptional: isOptional,
            IsBundle: isBundle,
            IsDevelopmentMode: package.IsDevelopmentMode,
            IsStaged: package.Status.IsPartiallyStaged,
            Status: PackageStatusRecord.FromStatus(package.Status),
            DisplayName: package.DisplayName ?? "");
    }
}

public sealed record PackageVersionRecord(ushort Major, ushort Minor, ushort Build, ushort Revision);

public sealed record PackageStatusRecord(
    bool Ok,
    string Raw,
    string? VerifyError,
    bool DataOffline,
    bool DependencyIssue,
    bool DeploymentInProgress,
    bool Disabled,
    bool IsPartiallyStaged,
    bool LicenseIssue,
    bool Modified,
    bool NeedsRemediation,
    bool NotAvailable,
    bool PackageOffline,
    bool Servicing,
    bool Tampered)
{
    public static PackageStatusRecord FromStatus(PackageStatus status)
    {
        var verifyError = default(string);
        var ok = false;
        try
        {
            ok = status.VerifyIsOK();
        }
        catch (Exception ex)
        {
            verifyError = ex.Message;
        }
        return new PackageStatusRecord(
            Ok: ok,
            Raw: status.ToString(),
            VerifyError: verifyError,
            DataOffline: status.DataOffline,
            DependencyIssue: status.DependencyIssue,
            DeploymentInProgress: status.DeploymentInProgress,
            Disabled: status.Disabled,
            IsPartiallyStaged: status.IsPartiallyStaged,
            LicenseIssue: status.LicenseIssue,
            Modified: status.Modified,
            NeedsRemediation: status.NeedsRemediation,
            NotAvailable: status.NotAvailable,
            PackageOffline: status.PackageOffline,
            Servicing: status.Servicing,
            Tampered: status.Tampered);
    }
}
