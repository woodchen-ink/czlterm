# czlterm

English | [简体中文](README.zh-CN.md)

A lightweight connection manager. SSH, RDP and VNC open in the clients you already have; czlterm manages the connections, pulls credentials from Vaultwarden, browses remote files, and exposes your servers to AI clients over MCP.

- **No built-in terminal or remote desktop**: SSH opens in your system terminal (Terminal, iTerm2, Ghostty, WezTerm, kitty, Windows Terminal, PowerShell); RDP uses Windows App on macOS and mstsc on Windows; VNC uses the built-in Screen Sharing on macOS and TigerVNC on Windows
- **Credentials from Vaultwarden**: passwords and SSH private keys are read through the official `bw` CLI and never written to disk
- **File manager**: browse, upload, download, rename and delete over SFTP; double-click a file to open it in VS Code (or your editor of choice), and it is uploaded back when you save
- **MCP**: AI clients can list your connections and, on servers you allow, read and write files and run commands
- **Sync**: each connection is one JSON file (no secrets), synced through git to your own private repository. Optionally set a username and key (an access token for HTTPS, a private key for SSH, stored in the system keychain); otherwise git's own authentication is used
- **Machine info**: after each SSH connection, OS, kernel, CPU, memory, disk and uptime are collected in the background and shown on the overview page; the connection list shows the matching OS logo
- Built with Go and Wails; the UI runs in the system WebView, no bundled browser engine

## Screenshots

![Main window: grouped connections, machine info and connection settings](docs/screenshots/overview.webp)

| | |
|---|---|
| ![SFTP file manager](docs/screenshots/files.webp) | ![SSH opens in your system terminal](docs/screenshots/ssh-terminal.webp) |
| SFTP file manager; double-click to edit, saved back automatically | Connect opens your system terminal; czlterm supplies the key or password |
| ![Edit connection: credentials reference Vaultwarden items](docs/screenshots/edit-connection.webp) | ![MCP settings](docs/screenshots/settings-mcp.webp) |
| Credentials only reference Vaultwarden items; no plaintext in the config | Tiered MCP permissions, multi-line scripts supported |
| ![Sync settings](docs/screenshots/settings-sync.webp) | ![About and updates](docs/screenshots/settings-about.webp) |
| Git sync to your own private repo, optional username and key | Built-in updates, verified against SHA-256 before installing |

(The UI is currently in Chinese.)

## Install

Download from [Releases](https://github.com/woodchen-ink/czlterm/releases):

- **Windows**: `czlterm-amd64-installer.exe`, installs per user into `%LOCALAPPDATA%\CZL\czlterm`, no admin rights needed
- **macOS**: `czlterm-darwin-universal.dmg`, universal for Intel and Apple Silicon. The app is not signed with an Apple developer certificate; if macOS says it is damaged on first launch, run once:
  ```bash
  xattr -dr com.apple.quarantine /Applications/czlterm.app
  ```

Introduction and discussion (Chinese): [SunAI forum thread](https://www.sunai.net/t/topic/1533)

## Requirements

- The `bw` CLI, logged in once from a terminal:
  ```bash
  npm i -g @bitwarden/cli
  bw config server https://your-vaultwarden
  bw login
  ```
  After that, unlock with your master password inside czlterm
- Store private keys as Bitwarden "SSH key" items. If a key has a passphrase, add a custom field named `passphrase` to the item
- OpenSSH 8.4 or newer (needed for `SSH_ASKPASS_REQUIRE`). Windows 10 ships OpenSSH 8.1; upgrade with `winget install Microsoft.OpenSSH.Preview`
- Sync needs git installed with access to your repository

## How credentials reach external programs

| Case | How |
|---|---|
| SSH private key | Decrypted into an in-process ssh-agent endpoint dedicated to this connection and handed to the system ssh through `SSH_AUTH_SOCK`. All keys of a jump chain share that endpoint; keys it doesn't hold are forwarded to your existing system agent |
| SSH password | ssh calls back into czlterm through `SSH_ASKPASS` and fetches the password with a one-time token. Other prompts (host key confirmation, 2FA codes) stay in the terminal for you to answer |
| RDP (Windows) | Written temporarily to Credential Manager as `TERMSRV/<host>` and restored after 2 minutes: a credential you had saved for that host is put back, otherwise it is deleted |
| RDP (macOS) | Windows App does not accept passwords from outside, so the password is copied to the clipboard and cleared after 45 seconds |
| VNC (macOS) | Screen Sharing is opened with a `vnc://` link. The password is not put in the link (command-line arguments are visible to other local users); it goes to the clipboard and is cleared after 45 seconds |
| VNC (Windows) | A `-passwd` file is generated for TigerVNC and deleted after 2 minutes; other viewers fall back to the clipboard |

Locking the vault (manually or after the idle timeout) or quitting clears the agent endpoints, in-process SSH connections and the bw session.

## MCP

Enable it in Settings → MCP and copy the config snippet:

- stdio clients (e.g. Claude Desktop): `czlterm mcp`
- HTTP clients (e.g. Claude Code): `http://127.0.0.1:47840/mcp` with a Bearer token

| Tool | What it does | Required permission |
|---|---|---|
| `list_connections` | Lists connections, no credentials | MCP enabled |
| `list_files` / `read_file` | Lists directories, reads files | "Allow AI to access servers" |
| `write_file` | Writes files | The above, plus "Allow AI to write files" |
| `run_command` | Runs a command or multi-line script (sent to `sh` over stdin; bash, zsh or powershell optional; working directory optional) | The above, plus "Allow AI to run commands" |

Credentials never leave the czlterm process.

## Updates

czlterm checks GitHub Releases 30 seconds after launch and every 6 hours after that. A new version is announced in the sidebar and can be installed from Settings → About. The installer is verified against `SHA256SUMS` before it runs.

## Data location

| OS | Path |
|---|---|
| macOS | `~/Library/Application Support/CZL/czlterm/` |
| Windows | `%LOCALAPPDATA%\CZL\czlterm\` |

Subfolders: `config/` local settings, `data/` connections (a git repository), `logs/`, `cache/`.

## Build

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
cd desktop && wails build
```

Windows installer: `.\build.ps1` (needs NSIS). Pushing a `v*` tag makes GitHub Actions build the Windows installer and macOS DMG and publish them to Releases.

## License

[AGPL-3.0](LICENSE)
