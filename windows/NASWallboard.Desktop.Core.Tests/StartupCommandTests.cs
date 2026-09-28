using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class StartupCommandTests
{
    [TestMethod]
    public void ForExecutableQuotesThePathAndAddsTheAutostartMarker()
    {
        Assert.AreEqual("\"C:\\Program Files\\NAS Wallboard\\NASWallboard.Desktop.exe\" --autostart", StartupCommand.ForExecutable("C:\\Program Files\\NAS Wallboard\\NASWallboard.Desktop.exe"));
        Assert.ThrowsExactly<ArgumentException>(() => StartupCommand.ForExecutable("C:\\bad\"path.exe"));
    }
}
