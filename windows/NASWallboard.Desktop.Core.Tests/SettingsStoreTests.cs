using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class SettingsStoreTests
{
    [TestMethod]
    public void LoadUsesDefaultsWhenFileIsMissingOrCorrupt()
    {
        var directory = Path.Combine(Path.GetTempPath(), $"nas-wallboard-{Guid.NewGuid():N}");
        var path = Path.Combine(directory, "settings.json");
        var store = new SettingsStore(path);
        Assert.AreEqual(AppSettings.Default, store.Load());

        Directory.CreateDirectory(directory);
        File.WriteAllText(path, "{broken");
        Assert.AreEqual(AppSettings.Default, store.Load());
    }

    [TestMethod]
    public void SaveAtomicallyRoundTripsSettingsWithoutLeavingTemporaryFile()
    {
        var directory = Path.Combine(Path.GetTempPath(), $"nas-wallboard-{Guid.NewGuid():N}");
        var path = Path.Combine(directory, "settings.json");
        var store = new SettingsStore(path);
        var settings = new AppSettings("https://nas.local", 1440, 80, false, false);

        store.Save(settings);

        Assert.AreEqual(settings with { ServerUrl = "https://nas.local/" }, store.Load());
        Assert.IsFalse(File.Exists(path + ".tmp"));
    }
}
