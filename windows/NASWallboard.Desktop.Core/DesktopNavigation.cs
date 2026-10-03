namespace NASWallboard.Desktop.Core;

public static class DesktopNavigation
{
    public static bool IsExternalManagementLink(string? requested, Uri serverOrigin, bool userInitiated) =>
        userInitiated && Uri.TryCreate(requested, UriKind.Absolute, out var destination) &&
        destination.Scheme.Equals(serverOrigin.Scheme, StringComparison.OrdinalIgnoreCase) &&
        destination.Host.Equals(serverOrigin.Host, StringComparison.OrdinalIgnoreCase) &&
        destination.Port == serverOrigin.Port &&
        destination.AbsolutePath is "/setup" or "/manage" or "/login";
}
