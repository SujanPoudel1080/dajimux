package completion

import (
	"fmt"
	"strings"
)

// SupportedShells lists the shells supported by dajimux completion.
var SupportedShells = []string{"bash", "zsh", "fish"}

// Generate generates the auto-completion script for the requested shell.
func Generate(shell string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "bash":
		return GenerateBashCompletion(), nil
	case "zsh":
		return GenerateZshCompletion(), nil
	case "fish":
		return GenerateFishCompletion(), nil
	default:
		return "", fmt.Errorf("unsupported shell %q. Supported shells: %s", shell, strings.Join(SupportedShells, ", "))
	}
}

// GenerateBashCompletion generates the bash completion script.
func GenerateBashCompletion() string {
	return `#!/usr/bin/env bash
# Bash completion for dajimux

_dajimux_sessions() {
    dajimux list 2>/dev/null | awk -F'│' 'NR>3 && NF>=4 {gsub(/^[ \t]+|[ \t]+$/, "", $2); if ($2 != "") print $2}'
}

_dajimux() {
    local cur prev words cword
    _init_completion || return

    local commands="attach list ls daemon stop kill-server kill-session restore service mcp config layout workspace agent msg pane peer completion man version help"

    if [[ $cword -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
        return 0
    fi

    local cmd="${words[1]}"

    case "${cmd}" in
        attach|restore|kill-session)
            local sessions=$(_dajimux_sessions)
            COMPREPLY=( $(compgen -W "${sessions}" -- "${cur}") )
            ;;
        layout)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "cycle bsp master grid" -- "${cur}") )
            fi
            ;;
        workspace)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "rename switch" -- "${cur}") )
            fi
            ;;
        service)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "install status uninstall" -- "${cur}") )
            fi
            ;;
        config)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "init path show" -- "${cur}") )
            fi
            ;;
        completion)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "bash zsh fish" -- "${cur}") )
            fi
            ;;
        agent)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "status" -- "${cur}") )
            elif [[ "${words[2]}" == "status" ]]; then
                COMPREPLY=( $(compgen -W "--state --badge --progress --pane" -- "${cur}") )
            fi
            ;;
        msg)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "send broadcast" -- "${cur}") )
            elif [[ "${words[2]}" == "send" ]]; then
                COMPREPLY=( $(compgen -W "--target --channel" -- "${cur}") )
            fi
            ;;
        pane)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "capture" -- "${cur}") )
            elif [[ "${words[2]}" == "capture" ]]; then
                COMPREPLY=( $(compgen -W "--pane --lines" -- "${cur}") )
            fi
            ;;
        peer)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "listen connect" -- "${cur}") )
            fi
            ;;
        mcp)
            COMPREPLY=( $(compgen -W "--socket" -- "${cur}") )
            ;;
        daemon)
            COMPREPLY=( $(compgen -W "--socket" -- "${cur}") )
            ;;
        *)
            ;;
    esac
}

complete -F _dajimux dajimux
`
}

// GenerateZshCompletion generates the zsh completion script.
func GenerateZshCompletion() string {
	return `#compdef dajimux
# Zsh completion for dajimux

_dajimux_sessions() {
    local -a sessions
    sessions=($(dajimux list 2>/dev/null | awk -F'│' 'NR>3 && NF>=4 {gsub(/^[ \t]+|[ \t]+$/, "", $2); if ($2 != "") print $2}'))
    if [[ ${#sessions[@]} -gt 0 ]]; then
        _describe -t sessions 'active sessions' sessions
    fi
}

_dajimux() {
    local curcontext="$curcontext" state line
    typeset -A opt_args

    _arguments -C \
        '1: :->command' \
        '*: :->args'

    case $state in
        command)
            local -a commands
            commands=(
                'attach:Attach to or create a session'
                'list:List active sessions and pane counts'
                'ls:List active sessions and pane counts'
                'daemon:Run the background session daemon'
                'stop:Gracefully stop the background daemon'
                'kill-server:Gracefully stop the background daemon'
                'kill-session:Terminate a specific active session'
                'restore:Restore session from persisted state'
                'service:Manage daemon background service'
                'mcp:Run Model Context Protocol (MCP) server over stdio'
                'config:Manage user configuration file'
                'layout:Switch or cycle layout presets'
                'workspace:Manage workspaces (rename, switch)'
                'agent:Report agent state & telemetry badge'
                'msg:Send message to another pane or channel'
                'pane:Capture output lines from sibling pane'
                'peer:Remote peer session networking'
                'completion:Generate shell auto-completion script'
                'man:Display manual page'
                'version:Print version information'
                'help:Print help information'
            )
            _describe -t commands 'dajimux commands' commands
            ;;
        args)
            case $words[1] in
                attach|restore|kill-session)
                    _dajimux_sessions
                    ;;
                service)
                    local -a service_cmds
                    service_cmds=(
                        'install:Register and start daemon service on boot'
                        'status:Check daemon service status'
                        'uninstall:Stop and unregister daemon service'
                    )
                    _describe -t service_cmds 'service actions' service_cmds
                    ;;
                config)
                    local -a config_cmds
                    config_cmds=(
                        'init:Generate default ~/.config/dajimux/config.yaml'
                        'path:Print path to configuration file'
                        'show:Display active loaded configuration'
                    )
                    _describe -t config_cmds 'config actions' config_cmds
                    ;;
                layout)
                    local -a layout_cmds
                    layout_cmds=('cycle:Cycle next layout preset' 'bsp:Default BSP layout' 'master:Master-Stack MonadTall layout' 'grid:Equal Grid layout')
                    _describe -t layout_cmds 'layout presets' layout_cmds
                    ;;
                workspace)
                    local -a ws_cmds
                    ws_cmds=('rename:Rename active or target workspace' 'switch:Switch active workspace')
                    _describe -t ws_cmds 'workspace actions' ws_cmds
                    ;;
                completion)
                    local -a shells
                    shells=('bash:Generate Bash completion script' 'zsh:Generate Zsh completion script' 'fish:Generate Fish completion script')
                    _describe -t shells 'shells' shells
                    ;;
                agent)
                    _values 'agent actions' 'status'
                    ;;
                pane)
                    _values 'pane actions' 'capture'
                    ;;
                peer)
                    _values 'peer actions' 'listen' 'connect'
                    ;;
            esac
            ;;
    esac
}

_dajimux "$@"
`
}

// GenerateFishCompletion generates the fish completion script.
func GenerateFishCompletion() string {
	return `# Fish completion for dajimux

function __fish_dajimux_needs_command
    set cmd (commandline -opc)
    if [ (count $cmd) -eq 1 ]
        return 0
    end
    return 1
end

function __fish_dajimux_using_command
    set cmd (commandline -opc)
    if [ (count $cmd) -gt 1 ]
        if [ $argv[1] = $cmd[2] ]
            return 0
        end
    end
    return 1
end

function __fish_dajimux_sessions
    dajimux list 2>/dev/null | awk -F'│' 'NR>3 && NF>=4 {gsub(/^[ \t]+|[ \t]+$/, "", $2); if ($2 != "") print $2}'
end

# Commands
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a attach -d 'Attach to or create a session'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a list -d 'List active sessions'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a ls -d 'List active sessions'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a daemon -d 'Run background session daemon'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a stop -d 'Stop background daemon'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a kill-session -d 'Terminate an active session'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a restore -d 'Restore session from persisted state'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a service -d 'Manage background system service'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a mcp -d 'Run Model Context Protocol server over stdio'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a config -d 'Manage configuration file'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a layout -d 'Switch or cycle layout presets'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a workspace -d 'Manage workspaces (rename, switch)'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a agent -d 'Report agent status badge'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a pane -d 'Capture sibling pane lines'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a msg -d 'Send agent message'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a peer -d 'Peer session connections'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a completion -d 'Generate shell completions'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a man -d 'Display manual page'
complete -f -c dajimux -n '__fish_dajimux_needs_command' -a version -d 'Print version'

# Subcommand completions
complete -f -c dajimux -n '__fish_dajimux_using_command attach' -a '(__fish_dajimux_sessions)' -d 'Session'
complete -f -c dajimux -n '__fish_dajimux_using_command restore' -a '(__fish_dajimux_sessions)' -d 'Session'
complete -f -c dajimux -n '__fish_dajimux_using_command kill-session' -a '(__fish_dajimux_sessions)' -d 'Session'
complete -f -c dajimux -n '__fish_dajimux_using_command service' -a 'install status uninstall'
complete -f -c dajimux -n '__fish_dajimux_using_command config' -a 'init path show'
complete -f -c dajimux -n '__fish_dajimux_using_command completion' -a 'bash zsh fish'
complete -f -c dajimux -n '__fish_dajimux_using_command peer' -a 'listen connect'
`
}
