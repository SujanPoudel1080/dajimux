# dajimux

> Modern terminal multiplexer & window manager with vim-like modal interface, BSP tiling, Kitty graphics support, and an agent mesh protocol.

## Features

- **Daemon-Client Architecture**: Sessions stay alive across disconnects and terminal restarts.
- **BSP Tiling Engine**: Binary space partitioning layouts with automated split detection, ratio adjustment, and monocle zoom.
- **Vim-like Modal Ergonomics**:
  - `INSERT`: Seamless, zero-latency passthrough to your shell/process.
  - Double-tap `Space` in insert mode to instantly enter `NORMAL` mode.
  - `NORMAL`: Vim navigation (`h`, `j`, `k`, `l`), splits (`s`, `v`), monocle zoom (`z`), and workspace jumps (`1`-`9`).
  - `RESIZE`: Fine-grained border resizing.
  - `COMMAND PALETTE`: Floating fuzzy finder for instant action dispatch.
- **Agent Mesh Protocol**:
  - Live agent status badges (e.g. Claude Code, Gemini CLI, Aider) rendered in pane headers.
  - Inter-pane message bus (publish/subscribe and point-to-point communication between agents).
  - Safe pane capture and automation hooks.
- **Kitty Graphics Protocol**:
  - Intercepts and translates `\x1b_G` graphic sequences so inline images render correctly within split panes.
- **Cross-Machine Peering**:
  - Secure QUIC/mTLS daemon mesh to access sessions on remote devboxes and servers.

## Quickstart

```bash
# Launch dajimux (spawns daemon in background if not running)
dajimux

# Launch a named session
dajimux attach my-project

# Send status update from inside an agent pane
dajimux agent status --state "testing" --badge "🟡 Running Pytest"

# Send a message to an agent in another pane
dajimux msg send --target pane-2 "Schema migration completed"
```

## Modal Navigation

- **Double-Tap `Space`**: Switch from `INSERT` mode to `NORMAL` mode.
- In `NORMAL` mode:
  - `h` / `j` / `k` / `l` : Focus left / down / up / right pane
  - `s` : Split pane horizontally
  - `v` : Split pane vertically
  - `x` : Close active pane
  - `z` : Zoom / toggle monocle (maximize pane)
  - `1` - `9` : Switch workspace
  - `r` : Enter resize mode
  - `i` / `a` / `Enter` : Return to `INSERT` mode
  - `:` or `Space` : Open Command Palette

## Architecture

```
dajimux/
├── cmd/dajimux/          # CLI entry point (Cobra)
├── internal/
│   ├── bsp/              # Binary Space Partitioning window tree
│   ├── client/           # Terminal client, raw mode, modal input handler
│   ├── daemon/           # Daemon server, session & workspace manager
│   ├── pty/              # POSIX pseudoterminal allocation & signals
│   ├── protocol/         # IPC message framing & types
│   ├── agent/            # Agent Mesh broker & client hooks
│   └── kitty/            # Kitty graphics protocol translation & clipping
```

## License

MIT License. See [LICENSE](LICENSE) for details.
