using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop.Core.Tests;

[TestClass]
public sealed class MoveModeTransitionTests
{
    [TestMethod]
    public void DragCompletionKeepsMoveModeEnabledUntilUserLocksWidget()
    {
        var locked = true;

        locked = MoveModeTransition.NextLocked(locked, MoveModeAction.Toggle);
        Assert.IsFalse(locked);

        locked = MoveModeTransition.NextLocked(locked, MoveModeAction.DragCompleted);
        Assert.IsFalse(locked);

        locked = MoveModeTransition.NextLocked(locked, MoveModeAction.Toggle);
        Assert.IsTrue(locked);
    }
}
