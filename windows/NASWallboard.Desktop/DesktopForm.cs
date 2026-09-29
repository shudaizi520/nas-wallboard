using System.Diagnostics;
using System.Text.Json;
using Microsoft.Web.WebView2.Core;
using Microsoft.Web.WebView2.WinForms;
using Microsoft.Win32;
using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop;

internal sealed class DesktopForm : Form
{
    private static readonly TimeSpan RetryDelay = TimeSpan.FromMinutes(1);
    private readonly SettingsStore settingsStore;
    private readonly WebView2 browser = new() { Dock = DockStyle.Fill };
    private readonly System.Windows.Forms.Timer retryTimer = new() { Interval = (int)RetryDelay.TotalMilliseconds };
    private readonly System.Windows.Forms.Timer dragTimer = new() { Interval = 30 };
    private readonly DesktopDragSession dragSession = new();
    private TrayMenu? tray;
    private AppSettings settings;
    private Uri serverOrigin;
    private bool allowExit;
    private bool browserReady;
    private bool showingFallback;
    private bool initialPlacementComplete;
    private Size? lastCssSize;

    internal AppSettings Settings => settings;

    internal DesktopForm(SettingsStore settingsStore)
    {
        StartupLog.Write("desktop form constructor entered");
        this.settingsStore = settingsStore;
        settings = settingsStore.Load();
        if (!ServerAddress.TryNormalize(settings.ServerUrl, out serverOrigin!))
            serverOrigin = new Uri(AppSettings.Default.ServerUrl);

        AutoScaleMode = AutoScaleMode.Dpi;
        FormBorderStyle = FormBorderStyle.None;
        ShowInTaskbar = false;
        StartPosition = FormStartPosition.Manual;
        BackColor = Color.Magenta;
        TransparencyKey = Color.Magenta;
        ClientSize = new Size(420, 260);
        Controls.Add(browser);

        retryTimer.Tick += (_, _) => NavigateDashboard();
        dragTimer.Tick += (_, _) => ContinueDragging();
        Load += OnLoaded;
        Move += (_, _) =>
        {
            if (!dragSession.Active) PersistPosition();
        };
        DpiChanged += (_, _) => ReapplyCssSize();
        FormClosing += OnClosing;
        SystemEvents.DisplaySettingsChanged += DisplaySettingsChanged;
        StartupLog.Write("desktop form constructor completed");
    }

    protected override bool ShowWithoutActivation => true;

    protected override CreateParams CreateParams
    {
        get
        {
            var parameters = base.CreateParams;
            parameters.ExStyle |= (int)(NativeMethods.WS_EX_TOOLWINDOW | NativeMethods.WS_EX_NOACTIVATE | NativeMethods.WS_EX_TRANSPARENT);
            return parameters;
        }
    }

    private async void OnLoaded(object? sender, EventArgs e)
    {
        StartupLog.Write("form load entered");
        StartupLog.Write(NativeMethods.TryAttachToDesktop(Handle)
            ? "attached to desktop host"
            : "desktop host attachment failed; continuing as a normal window");
        PlaceInitialWindow();
        initialPlacementComplete = true;
        tray = new TrayMenu(this);
        StartupLog.Write("tray icon created");
        ApplyLockedStyle();
        UpdateDragMonitoring();
        try
        {
            var dataDirectory = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
                "NASWallboard", "WebView2");
            var environment = await CoreWebView2Environment.CreateAsync(null, dataDirectory);
            StartupLog.Write("WebView2 environment created");
            await browser.EnsureCoreWebView2Async(environment);
            browser.DefaultBackgroundColor = Color.Transparent;
            browser.CoreWebView2.Settings.AreDefaultContextMenusEnabled = false;
            browser.CoreWebView2.Settings.AreDevToolsEnabled = false;
            browser.CoreWebView2.Settings.IsStatusBarEnabled = false;
            browser.CoreWebView2.Settings.IsZoomControlEnabled = false;
            browser.CoreWebView2.NavigationStarting += NavigationStarting;
            browser.CoreWebView2.NavigationCompleted += NavigationCompleted;
            browser.CoreWebView2.WebMessageReceived += WebMessageReceived;
            browserReady = true;
            StartupLog.Write("WebView2 controller ready");
            NavigateDashboard();
        }
        catch (WebView2RuntimeNotFoundException)
        {
            StartupLog.Write("WebView2 Runtime was not found");
            ShowRuntimeMissing();
        }
        catch (Exception exception)
        {
            StartupLog.Report(exception);
            ShowStartupFailure(exception);
        }
    }

    private void NavigationStarting(object? sender, CoreWebView2NavigationStartingEventArgs e)
    {
        if (!Uri.TryCreate(e.Uri, UriKind.Absolute, out var requested))
        {
            e.Cancel = true;
            return;
        }

        if (showingFallback && requested.Scheme.Equals("about", StringComparison.OrdinalIgnoreCase)) return;
        if (!SameOrigin(requested, serverOrigin)) e.Cancel = true;
    }

    private void NavigationCompleted(object? sender, CoreWebView2NavigationCompletedEventArgs e)
    {
        if (showingFallback) return;
        if (e.IsSuccess)
        {
            retryTimer.Stop();
            UpdateMovableMode();
            return;
        }
        ShowFallback();
    }

    private void WebMessageReceived(object? sender, CoreWebView2WebMessageReceivedEventArgs e)
    {
        if (!BrowserMessage.TryParse(e.WebMessageAsJson, out var command)) return;
        switch (command)
        {
            case ResizeCommand resize:
                ResizeAndClamp(resize.Width, resize.Height);
                break;
        }
    }

    internal void ToggleLocked()
    {
        FinishDragging();
        SetLocked(MoveModeTransition.NextLocked(settings.Locked, MoveModeAction.Toggle));
    }

    private void SetLocked(bool locked)
    {
        if (settings.Locked == locked) return;
        settings = settings with { Locked = locked };
        SaveSettings();
        ApplyLockedStyle();
        tray?.UpdateLocked(settings.Locked);
        UpdateMovableMode();
        UpdateDragMonitoring();
    }

    internal void SetAutoStart(bool enabled)
    {
        settings = settings with { AutoStart = enabled };
        SaveSettings();
    }

    internal void OpenManagement()
    {
        var address = new Uri(serverOrigin, "manage").AbsoluteUri;
        Process.Start(new ProcessStartInfo(address) { UseShellExecute = true });
    }

    internal void RefreshDashboard()
    {
        if (browserReady && !showingFallback) browser.Reload();
        else NavigateDashboard();
    }

    internal void ChangeServerAddress()
    {
        using var dialog = new ServerAddressDialog(serverOrigin.AbsoluteUri);
        if (dialog.ShowDialog() != DialogResult.OK) return;
        serverOrigin = new Uri(dialog.ServerUrl);
        settings = settings with { ServerUrl = serverOrigin.AbsoluteUri };
        SaveSettings();
        NavigateDashboard();
    }

    internal void ExitApplication()
    {
        allowExit = true;
        Close();
    }

    private void NavigateDashboard()
    {
        if (!browserReady) return;
        showingFallback = false;
        browser.CoreWebView2.Navigate(new UriBuilder(serverOrigin) { Query = "desktop=1" }.Uri.AbsoluteUri);
    }

    private void ShowFallback()
    {
        if (!browserReady || showingFallback) return;
        showingFallback = true;
        browser.NavigateToString(FallbackPage.Create(serverOrigin.AbsoluteUri));
        ResizeAndClamp(388, 116);
        retryTimer.Start();
    }

    private void ShowRuntimeMissing()
    {
        browser.Visible = false;
        BackColor = Color.FromArgb(12, 23, 40);
        TransparencyKey = Color.Empty;
        ClientSize = new Size(390, 130);
        var message = new Label
        {
            Dock = DockStyle.Fill,
            Padding = new Padding(22),
            ForeColor = Color.FromArgb(220, 231, 245),
            Font = new Font("Microsoft YaHei UI", 10F),
            Text = "缺少 Microsoft Edge WebView2 Runtime\r\n安装后重新打开 NAS 桌面组件。",
        };
        Controls.Add(message);
        message.BringToFront();
        ClampToVisibleArea();
    }

    private void ShowStartupFailure(Exception exception)
    {
        browser.Visible = false;
        BackColor = Color.FromArgb(12, 23, 40);
        TransparencyKey = Color.Empty;
        ClientSize = new Size(430, 150);
        var message = new Label
        {
            Dock = DockStyle.Fill,
            Padding = new Padding(22),
            ForeColor = Color.FromArgb(220, 231, 245),
            Font = new Font("Microsoft YaHei UI", 10F),
            Text = $"启动失败：{exception.GetType().Name}\r\n错误已经写入启动日志。",
        };
        Controls.Add(message);
        message.BringToFront();
        ClampToVisibleArea();
    }

    private void ApplyLockedStyle()
    {
        if (!IsHandleCreated) return;
        var style = NativeMethods.GetWindowLongPtr(Handle, NativeMethods.GWL_EXSTYLE);
        style |= NativeMethods.WS_EX_TOOLWINDOW | NativeMethods.WS_EX_NOACTIVATE;
        if (settings.Locked) style |= NativeMethods.WS_EX_TRANSPARENT;
        else style &= ~NativeMethods.WS_EX_TRANSPARENT;
        NativeMethods.SetWindowLongPtr(Handle, NativeMethods.GWL_EXSTYLE, style);
        NativeMethods.SetWindowPos(Handle, 0, 0, 0, 0, 0,
            NativeMethods.SWP_NOMOVE | NativeMethods.SWP_NOSIZE | NativeMethods.SWP_NOZORDER |
            NativeMethods.SWP_NOACTIVATE | NativeMethods.SWP_FRAMECHANGED);
    }

    private void ContinueDragging()
    {
        if (settings.Locked)
        {
            dragTimer.Stop();
            return;
        }

        var leftButtonDown = NativeMethods.IsLeftMouseButtonDown();
        if (!leftButtonDown)
        {
            FinishDragging();
            SetLocked(MoveModeTransition.NextLocked(settings.Locked, MoveModeAction.DragCompleted));
            return;
        }
        if (!NativeMethods.TryGetCursorPosition(out var pointer)) return;
        if (!dragSession.Active)
        {
            if (!NativeMethods.TryGetWindowBounds(Handle, out var bounds) ||
                !dragSession.TryStart(bounds, pointer, leftButtonDown)) return;
        }
        if (dragSession.TryMoveTo(pointer, out var location))
            NativeMethods.TryMoveWindowToScreenPosition(Handle, location);
    }

    private void FinishDragging()
    {
        if (!dragSession.Active) return;
        ClampToVisibleArea();
        if (!dragSession.Stop()) return;
        PersistPosition();
    }

    private void UpdateDragMonitoring()
    {
        if (settings.Locked) dragTimer.Stop();
        else dragTimer.Start();
    }

    private void UpdateMovableMode()
    {
        if (!browserReady || showingFallback) return;
        browser.CoreWebView2.PostWebMessageAsJson(JsonSerializer.Serialize(new
        {
            type = "movable",
            enabled = !settings.Locked,
        }));
    }

    private void PlaceInitialWindow()
    {
        if (settings.X != -1 || settings.Y != -1)
        {
            if (!NativeMethods.TryMoveWindowToScreenPosition(Handle, new PixelPoint(settings.X, settings.Y)))
                Location = new Point(settings.X, settings.Y);
        }
        else
        {
            var area = Screen.PrimaryScreen?.WorkingArea ?? new Rectangle(0, 0, 1920, 1080);
            var placement = WindowPlacement.TopRight(
                new ScreenRect(area.X, area.Y, area.Width, area.Height),
                Width,
                Height);
            var location = new PixelPoint(placement.X, placement.Y);
            if (!NativeMethods.TryMoveWindowToScreenPosition(Handle, location))
                Location = new Point(location.X, location.Y);
        }
        ClampToVisibleArea();
    }

    private void ResizeAndClamp(int width, int height)
    {
        lastCssSize = new Size(width, height);
        var scale = DeviceDpi / 96d;
        var pixels = CssPixelSize.ToRawPixels(width, height, scale);
        var current = CurrentBounds();
        var resized = WindowPlacement.Resize(current, pixels.Width, pixels.Height, WorkAreas());
        if (!NativeMethods.TrySetWindowBoundsFromScreen(Handle, resized))
            Bounds = new Rectangle(resized.X, resized.Y, resized.Width, resized.Height);
        PersistPosition();
    }

    private void ReapplyCssSize()
    {
        if (lastCssSize is { } size) ResizeAndClamp(size.Width, size.Height);
    }

    private void DisplaySettingsChanged(object? sender, EventArgs e) => ClampToVisibleArea();

    private void ClampToVisibleArea()
    {
        var clamped = WindowPlacement.Clamp(CurrentBounds(), WorkAreas());
        if (!NativeMethods.TrySetWindowBoundsFromScreen(Handle, clamped))
            Bounds = new Rectangle(clamped.X, clamped.Y, clamped.Width, clamped.Height);
    }

    private ScreenRect CurrentBounds() =>
        NativeMethods.TryGetWindowBounds(Handle, out var bounds)
            ? new ScreenRect(bounds.X, bounds.Y, bounds.Width, bounds.Height)
            : new ScreenRect(Left, Top, Width, Height);

    private static ScreenRect[] WorkAreas() => Screen.AllScreens
        .Select(screen => new ScreenRect(
            screen.WorkingArea.X,
            screen.WorkingArea.Y,
            screen.WorkingArea.Width,
            screen.WorkingArea.Height))
        .ToArray();

    private void PersistPosition()
    {
        if (WindowState != FormWindowState.Normal) return;
        var location = NativeMethods.TryGetWindowBounds(Handle, out var bounds)
            ? new PixelPoint(bounds.X, bounds.Y)
            : new PixelPoint(Left, Top);
        if (!WindowPlacement.ShouldPersist(initialPlacementComplete, location, new PixelPoint(settings.X, settings.Y))) return;
        settings = settings with { X = location.X, Y = location.Y };
        SaveSettings();
    }

    private void SaveSettings()
    {
        try
        {
            settingsStore.Save(settings);
        }
        catch (IOException)
        {
            // Keep running; the next explicit setting change will retry the save.
        }
        catch (UnauthorizedAccessException)
        {
            // Keep the in-memory setting for this session.
        }
    }

    private void OnClosing(object? sender, FormClosingEventArgs e)
    {
        if (!allowExit && e.CloseReason == CloseReason.UserClosing)
        {
            e.Cancel = true;
            return;
        }
        retryTimer.Stop();
        dragTimer.Stop();
        SystemEvents.DisplaySettingsChanged -= DisplaySettingsChanged;
        tray?.Dispose();
        browser.Dispose();
        dragTimer.Dispose();
        retryTimer.Dispose();
    }

    private static bool SameOrigin(Uri left, Uri right) =>
        left.Scheme.Equals(right.Scheme, StringComparison.OrdinalIgnoreCase) &&
        left.Host.Equals(right.Host, StringComparison.OrdinalIgnoreCase) &&
        left.Port == right.Port;
}
