using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop;

internal static class Program
{
    private const string MutexName = "Local\\NASWallboard.Desktop.SingleInstance";

    [STAThread]
    private static void Main()
    {
        StartupLog.Write("process entry");
        Application.SetUnhandledExceptionMode(UnhandledExceptionMode.CatchException);
        Application.ThreadException += (_, args) => StartupLog.Report(args.Exception);
        AppDomain.CurrentDomain.UnhandledException += (_, args) =>
        {
            if (args.ExceptionObject is Exception exception) StartupLog.Report(exception);
            else StartupLog.Write($"FATAL non-exception object: {args.ExceptionObject}");
        };

        try
        {
            RunApplication();
        }
        catch (Exception exception)
        {
            StartupLog.Report(exception);
        }
    }

    private static void RunApplication()
    {
        using var mutex = new Mutex(true, MutexName, out var ownsMutex);
        if (!ownsMutex)
        {
            StartupLog.Write("another instance owns the mutex");
            return;
        }

        ApplicationConfiguration.Initialize();
        var settingsDirectory = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
            "NASWallboard");
        var settingsPath = Path.Combine(settingsDirectory, "settings.json");
        StartupLog.Write($"settings path ready: {settingsPath}");
        SeedSettings(settingsPath);

        var settingsStore = new SettingsStore(settingsPath);
        StartupLog.Write("creating desktop form");
        Application.Run(new DesktopForm(settingsStore));
        StartupLog.Write("message loop ended");
    }

    private static void SeedSettings(string destination)
    {
        if (File.Exists(destination)) return;
        var seed = Path.Combine(AppContext.BaseDirectory, "settings.json");
        if (!File.Exists(seed)) return;

        try
        {
            var settings = new SettingsStore(seed).Load();
            new SettingsStore(destination).Save(settings);
        }
        catch (IOException)
        {
            // The writable per-user settings file will be created on first change.
        }
        catch (UnauthorizedAccessException)
        {
            // The application remains usable with built-in defaults.
        }
    }
}
