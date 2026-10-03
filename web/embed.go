package web

import "embed"

// FS contains the dependency-free wallboard frontend.
//
//go:embed index.html styles.css app.js poller.js view.js desktop.js versions.js manage.html manage.css manage-overview.css manage-desktop.css manage-integrations.css manage-settings.css manage.js manage-ui.js manage-api.js integrations.js layout.js overview.js settings.js setup.html setup.js login.html login.js admin.css icon.svg wallpaper.png downloads/*
var FS embed.FS
