package man

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// GenerateManPage returns the standard roff/troff manpage for dajimux(1).
func GenerateManPage(version string) string {
	if version == "" {
		version = "0.1.5"
	}
	return fmt.Sprintf(`.TH DAJIMUX 1 "November 2026" "dajimux %s" "User Commands"
.SH NAME
dajimux \- modern terminal multiplexer and agent mesh
.SH SYNOPSIS
\fBdajimux\fR [\fISESSION\fR]
.br
\fBdajimux\fR \fBattach\fR [\fISESSION\fR]
.br
\fBdajimux\fR \fBlist\fR | \fBls\fR
.br
\fBdajimux\fR \fBdaemon\fR [\fB--socket\fR \fIPATH\fR]
.br
\fBdajimux\fR \fBstop\fR | \fBkill-server\fR
.br
\fBdajimux\fR \fBrestore\fR [\fISESSION\fR]
.br
\fBdajimux\fR \fBservice\fR [\fBinstall\fR | \fBstatus\fR | \fBuninstall\fR]
.br
\fBdajimux\fR \fBmcp\fR [\fB--socket\fR \fIPATH\fR]
.br
\fBdajimux\fR \fBconfig\fR [\fBinit\fR | \fBpath\fR | \fBshow\fR]
.br
\fBdajimux\fR \fBcompletion\fR [\fBbash\fR | \fBzsh\fR | \fBfish\fR]
.br
\fBdajimux\fR \fBagent status\fR [\fB--state\fR \fISTATE\fR] [\fB--badge\fR \fITEXT\fR] [\fB--progress\fR \fIPCT\fR]
.br
\fBdajimux\fR \fBmsg send\fR [\fB--target\fR \fIPANE\fR] [\fB--channel\fR \fICHAN\fR] \fIMESSAGE\fR
.br
\fBdajimux\fR \fBpane capture\fR [\fB--pane\fR \fIPANE\fR] [\fB--lines\fR \fICOUNT\fR]
.br
\fBdajimux\fR \fBpeer listen\fR [\fB--port\fR \fIPORT\fR]
.br
\fBdajimux\fR \fBpeer connect\fR \fIADDR\fR
.br
\fBdajimux\fR \fBman\fR [\fB--raw\fR]
.br
\fBdajimux\fR \fBversion\fR

.SH DESCRIPTION
\fBdajimux\fR is a lightweight, zero-dependency, binary-distributed terminal multiplexer and agent mesh written in Go.
It delivers Vim-modal controls (leaderless Space+Space escape), automatic Binary Space Partitioning (BSP) window tiling, full SGR 1006 mouse interaction, robust session recovery, native Model Context Protocol (MCP) tooling for AI agents, and inter-pane peer messaging.

.SH COMMANDS
.TP
\fBdajimux\fR [\fISESSION\fR]
Attach to the specified session, or create it if it does not exist. Defaults to session "main".
.TP
\fBattach\fR [\fISESSION\fR]
Explicitly attach to a session by name.
.TP
\fBlist\fR, \fBls\fR
List active sessions, their creation time, and open pane counts.
.TP
\fBdaemon\fR [\fB--socket\fR \fIPATH\fR]
Run the background multiplexer daemon process directly.
.TP
\fBstop\fR, \fBkill-server\fR
Gracefully stop the running background daemon and persist session state.
.TP
\fBkill-session\fR \fISESSION\fR
Terminate a specific session and close all child processes.
.TP
\fBrename-session\fR \fIOLD_NAME\fR \fINEW_NAME\fR
Rename an active session.
.TP
\fBsession\fR \fBrename\fR | \fBkill\fR | \fBlist\fR
Manage sessions directly from the command line.
.TP
\fBrestore\fR [\fISESSION\fR]
Restore session layout and working directories from persisted state.
.TP
\fBservice\fR \fBinstall\fR | \fBstatus\fR | \fBuninstall\fR
Manage the background daemon as an OS-level user service (macOS launchd or Linux systemd user units).
.TP
\fBmcp\fR [\fB--socket\fR \fIPATH\fR]
Launch a JSON-RPC 2.0 Model Context Protocol (MCP) server over standard input and output for integration with AI assistants (Claude Desktop, Antigravity, Cursor, etc.).
.TP
\fBconfig\fR \fBinit\fR | \fBpath\fR | \fBshow\fR
Manage user configuration located at \fI~/.config/dajimux/config.yaml\fR.
.TP
\fBcompletion\fR \fBbash\fR | \fBzsh\fR | \fBfish\fR
Generate shell auto-completion script for the specified shell.
.TP
\fBagent status\fR
Report AI agent state, telemetry badge, and progress to the active pane statusline.
.TP
\fBmsg send\fR
Send a message or payload to another pane or broadcast channel.
.TP
\fBpane capture\fR
Capture scrollback lines from a designated pane for inspection or indexing.
.TP
\fBpeer\fR \fBlisten\fR | \fBconnect\fR
Enable distributed collaboration by sharing or connecting to a remote session.
.TP
\fBman\fR [\fB--raw\fR]
Display the manual page formatted via system man viewer or print raw roff source.
.TP
\fBversion\fR
Display current version, git commit hash, and build date.

.SH VIM MODAL CONTROLS
dajimux utilizes modal control paradigm inspired by Vim:
.TP
\fBSpace + Space\fR
Switch from INSERT mode to NORMAL mode.
.PP
In \fBNORMAL mode\fR:
.TP
\fBh, j, k, l\fR
Spatial navigation: move focus Left, Down, Up, or Right.
.TP
\fBw\fR
Swap active pane with the next pane in tree order.
.TP
\fBH, J, K, L\fR
Spatial swap: swap active pane with neighbour Left, Down, Up, or Right.
.TP
\fBs\fR
Split active pane horizontally (top / bottom).
.TP
\fBv\fR
Split active pane vertically (left / right).
.TP
\fBe\fR
Split active pane automatically (vertical if wide, horizontal if tall).
.TP
\fBm <1-9>\fR
Move active pane directly to workspace 1 through 9.
.TP
\fBx\fR
Close the focused pane.
.TP
\fBz\fR
Toggle monocle zoom mode (maximize focused pane full-screen).
.TP
\fBd\fR
Detach client from session (daemon keeps running).
.TP
\fB[\fR
Enter Vi-style scrollback navigation mode.
.TP
\fB:\fR or \fBSpace\fR
Open interactive command palette.
.TP
\fBr\fR
Enter pane resize mode.
.TP
\fB1 - 9\fR
Switch to workspace 1 through 9.
.TP
\fBi, a, Enter\fR
Return to INSERT mode (pass keys to running shell/process).

.SH MOUSE SUPPORT
dajimux supports standard extended mouse tracking (SGR 1006 protocol):
.IP \(bu 2
\fBLeft Click on Pane\fR: Focus the targeted pane.
.IP \(bu 2
\fBLeft Click on Header Tab\fR: Switch directly to workspace 1..9.
.IP \(bu 2
\fBMouse Wheel Up / Down\fR: Seamlessly enter Vi scrollback mode and scroll lines.
.IP \(bu 2
\fBMouse Border Drag\fR: Interactively resize adjacent panes by dragging borders.

.SH CONFIGURATION
Configuration is read from \fI~/.config/dajimux/config.yaml\fR (or path provided via \fB--config\fR).
Generate a default template with:
.PP
.nf
.RS
dajimux config init
.RE
.fi

.SH MODEL CONTEXT PROTOCOL (MCP)
dajimux exposes an MCP server for AI agent environments:
.PP
.nf
.RS
{
  "mcpServers": {
    "dajimux": {
      "command": "dajimux",
      "args": ["mcp"]
    }
  }
}
.RE
.fi
.PP
Available MCP tools:
.TP
\fBlist_panes\fR
List all panes, working directories, active commands, and agent telemetry.
.TP
\fBread_pane\fR
Capture scrollback buffer lines from any pane.
.TP
\fBsend_command\fR
Execute a shell command or keystroke sequence inside a pane.
.TP
\fBspawn_pane\fR
Create a new pane in a horizontal or vertical split with a specified command and working directory.
.TP
\fBset_agent_status\fR
Update the visual agent telemetry badge and progress bar in the pane status line.

.SH FILES
.TP
\fI~/.config/dajimux/config.yaml\fR
User configuration file.
.TP
\fI~/.local/share/dajimux/session_state.json\fR
Persisted daemon layout and session state across reboots.
.TP
\fI/tmp/dajimux.sock\fR
Default UNIX domain socket for daemon client-server IPC.

.SH AUTHORS
Written and maintained by Sujan Poudel <sujanpoudel1080@gmail.com>.
GitHub: https://github.com/SujanPoudel1080/dajimux

.SH REPORTING BUGS
Submit bug reports and feature requests at:
https://github.com/SujanPoudel1080/dajimux/issues
`, version)
}

// View displays the man page using the system's man viewer or less pager,
// falling back to raw stdout if no pager is available.
func View(version string, raw bool, stdout io.Writer) error {
	content := GenerateManPage(version)
	if raw {
		_, err := stdout.Write([]byte(content))
		return err
	}

	// Try using man with stdin
	if manPath, err := exec.LookPath("man"); err == nil {
		cmd := exec.Command(manPath, "-l", "-")
		cmd.Stdin = strings.NewReader(content)
		cmd.Stdout = stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Try groff
	if groffPath, err := exec.LookPath("groff"); err == nil {
		var buf bytes.Buffer
		groffCmd := exec.Command(groffPath, "-man", "-Tutf8")
		groffCmd.Stdin = strings.NewReader(content)
		groffCmd.Stdout = &buf
		if err := groffCmd.Run(); err == nil {
			if pagerPath, err := exec.LookPath("less"); err == nil {
				pager := exec.Command(pagerPath, "-R")
				pager.Stdin = &buf
				pager.Stdout = stdout
				pager.Stderr = os.Stderr
				if err := pager.Run(); err == nil {
					return nil
				}
			}
			_, err := stdout.Write(buf.Bytes())
			return err
		}
	}

	// Fallback to writing directly
	_, err := stdout.Write([]byte(content))
	return err
}
