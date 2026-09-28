using System.Net;

namespace NASWallboard.Desktop;

internal static class FallbackPage
{
    internal static string Create(string serverUrl) => $$"""
        <!doctype html>
        <html lang="zh-CN">
        <meta charset="utf-8">
        <meta name="color-scheme" content="dark">
        <style>
          * { box-sizing: border-box; }
          html, body { width: 100%; height: 100%; margin: 0; overflow: hidden; background: transparent; }
          body { display: grid; place-items: center; font: 14px/1.5 "Segoe UI", "Microsoft YaHei UI", sans-serif; color: #dce7f5; }
          main { width: 340px; padding: 22px 24px; border: 1px solid rgba(151, 181, 218, .22); border-radius: 24px;
                 background: linear-gradient(145deg, rgba(21, 34, 57, .88), rgba(7, 17, 31, .92));
                 box-shadow: 0 18px 42px rgba(0, 0, 0, .28); backdrop-filter: blur(22px); }
          .state { display: flex; align-items: center; gap: 12px; font-size: 16px; font-weight: 650; }
          .dot { width: 9px; height: 9px; border-radius: 50%; background: #ffb55d; box-shadow: 0 0 14px rgba(255, 181, 93, .7); }
          p { margin: 9px 0 0 21px; color: #91a1b8; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        </style>
        <body><main><div class="state"><span class="dot"></span>NAS 未连接</div><p>{{WebUtility.HtmlEncode(serverUrl)}}</p></main></body>
        </html>
        """;
}
