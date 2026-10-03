using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class DesktopNavigationTests
{
    [TestMethod]
    [DataRow("http://nas.local/setup", true, true)]
    [DataRow("http://nas.local/manage?tab=overview", true, true)]
    [DataRow("http://nas.local/login?next=%2Fmanage", true, true)]
    [DataRow("http://nas.local/setup", false, false)]
    [DataRow("http://evil.local/setup", true, false)]
    [DataRow("http://nas.local:9000/setup", true, false)]
    [DataRow("http://nas.local/api/setup", true, false)]
    [DataRow("http://nas.local/?desktop=1&desktop_attempt=2", true, false)]
    [DataRow("http://nas.local/setup/evil", true, false)]
    [DataRow("about:blank", true, false)]
    public void OnlyUserInitiatedSameOriginManagementRoutesOpenExternally(string requested, bool userInitiated, bool expected)
    {
        Assert.AreEqual(expected, DesktopNavigation.IsExternalManagementLink(requested, new Uri("http://nas.local/"), userInitiated));
    }
}
