namespace NASWallboard.Desktop.Core;

public static class StartupCommand
{
    public static string ForExecutable(string executablePath)
    {
        if (string.IsNullOrWhiteSpace(executablePath) || executablePath.Contains('"'))
            throw new ArgumentException("Executable path is invalid.", nameof(executablePath));
        return $"\"{executablePath}\" --autostart";
    }
}
