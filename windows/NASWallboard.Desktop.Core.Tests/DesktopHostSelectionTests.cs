using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class DesktopHostSelectionTests
{
    [TestMethod]
    public void SelectUsesWorkerWindowImmediatelyBehindShellView()
    {
        var windows = new[]
        {
            new DesktopWindow(101, IsWorker: true, HasShellView: false),
            new DesktopWindow(202, IsWorker: false, HasShellView: true),
            new DesktopWindow(303, IsWorker: true, HasShellView: false),
            new DesktopWindow(404, IsWorker: true, HasShellView: false),
        };

        Assert.AreEqual((nint)303, DesktopHostSelection.Select(windows, programManager: 202));
    }

    [TestMethod]
    public void SelectFallsBackToProgramManagerWhenNoDesktopWorkerExists()
    {
        var windows = new[]
        {
            new DesktopWindow(202, IsWorker: false, HasShellView: true),
            new DesktopWindow(404, IsWorker: false, HasShellView: false),
        };

        Assert.AreEqual((nint)202, DesktopHostSelection.Select(windows, programManager: 202));
    }
}
