using System.Reflection;
using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop;

internal static class DesktopClientVersion
{
    internal static string Current { get; } = ReleaseVersion.Normalize(
        typeof(DesktopClientVersion).Assembly.GetCustomAttribute<AssemblyInformationalVersionAttribute>()?.InformationalVersion) ?? "未知版本";
}
