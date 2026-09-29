namespace NASWallboard.Desktop.Core;

public enum MoveModeAction
{
    Toggle,
    DragCompleted,
}

public static class MoveModeTransition
{
    public static bool NextLocked(bool locked, MoveModeAction action) => action switch
    {
        MoveModeAction.Toggle => !locked,
        MoveModeAction.DragCompleted => locked,
        _ => throw new ArgumentOutOfRangeException(nameof(action)),
    };
}
