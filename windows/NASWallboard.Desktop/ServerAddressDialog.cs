using NASWallboard.Desktop.Core;

namespace NASWallboard.Desktop;

internal sealed class ServerAddressDialog : Form
{
    private readonly TextBox address = new();

    internal string ServerUrl { get; private set; }

    internal ServerAddressDialog(string currentUrl)
    {
        ServerUrl = currentUrl;
        Text = "设置 NAS 地址";
        Font = new Font("Microsoft YaHei UI", 10F);
        FormBorderStyle = FormBorderStyle.FixedDialog;
        MaximizeBox = false;
        MinimizeBox = false;
        ShowInTaskbar = false;
        StartPosition = FormStartPosition.CenterScreen;
        ClientSize = new Size(430, 142);

        var label = new Label { Text = "NAS 地址", AutoSize = true, Location = new Point(20, 19) };
        address.SetBounds(20, 46, 390, 30);
        address.Text = currentUrl;
        address.SelectAll();

        var cancel = new Button { Text = "取消", DialogResult = DialogResult.Cancel };
        cancel.SetBounds(238, 94, 82, 30);
        var save = new Button { Text = "保存" };
        save.SetBounds(328, 94, 82, 30);
        save.Click += SaveClick;

        Controls.AddRange([label, address, cancel, save]);
        AcceptButton = save;
        CancelButton = cancel;
    }

    private void SaveClick(object? sender, EventArgs e)
    {
        if (!ServerAddress.TryNormalize(address.Text, out var server))
        {
            MessageBox.Show(this, "请输入以 http:// 或 https:// 开头的 NAS 首页地址。", "地址无效",
                MessageBoxButtons.OK, MessageBoxIcon.Warning);
            address.Focus();
            return;
        }

        ServerUrl = server.AbsoluteUri;
        DialogResult = DialogResult.OK;
        Close();
    }
}
