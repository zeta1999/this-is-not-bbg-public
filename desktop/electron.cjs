const { app, BrowserWindow } = require("electron");
const fs = require("fs");
const path = require("path");

app.setName("NOTBBG Terminal");

let win;

function createWindow() {
  // Read desktop token from server.
  let token = "";
  try {
    token = fs.readFileSync("/tmp/notbbg-desktop.token", "utf-8").trim();
    console.log("[notbbg] desktop token loaded");
  } catch {
    console.log("[notbbg] no desktop token found — server may not be running yet");
  }

  win = new BrowserWindow({
    width: 1400,
    height: 900,
    title: "NOTBBG Terminal",
    backgroundColor: "#000000",
    icon: path.join(__dirname, "assets", process.platform === "darwin" ? "icon.icns" : "icon.png"),
    webPreferences: {
      nodeIntegration: false,
      contextIsolation: true,
    },
  });

  const devUrl = process.env.VITE_DEV_URL || "http://localhost:1420";
  const url = token ? `${devUrl}?token=${token}` : devUrl;
  win.loadURL(url);

  win.on("closed", () => { win = null; });
}

app.whenReady().then(() => {
  if (process.platform === "darwin") {
    const { nativeImage } = require("electron");
    const iconPath = path.join(__dirname, "assets", "icon.icns");
    try { app.dock.setIcon(nativeImage.createFromPath(iconPath)); } catch {}
  }
  createWindow();
});
app.on("window-all-closed", () => app.quit());
app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0) createWindow();
});
