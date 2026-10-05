package cli

import (
	"fmt"
	"strings"
)

// CmdCompletion generates shell autocompletion scripts for bash, zsh, or fish.
func CmdCompletion(args []string) {
	shell := "bash"
	if len(args) > 0 {
		shell = strings.ToLower(args[0])
	}

	switch shell {
	case "bash":
		fmt.Print(bashCompletionScript)
	case "zsh":
		fmt.Print(zshCompletionScript)
	case "fish":
		fmt.Print(fishCompletionScript)
	default:
		die("unsupported shell %q. Supported shells: bash, zsh, fish\nUsage: amux completion [bash|zsh|fish]", shell)
	}
}

const bashCompletionScript = `# bash completion for amux                                 -*- shell-script -*-

_amux_completions() {
    local cur prev words cword
    _init_completion || return

    local commands="start stop restart status off hook unhook pool env login account switch doctor audit usage setup config threshold migrate update uninstall feedback completion vault mcp"
    local providers="claude codex agy chatgpt gemini-web muse gemini groq kimi grok github cursor"

    if [[ ${cword} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
        return 0
    fi

    case "${words[1]}" in
        login)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "${providers}" -- "${cur}") )
            fi
            ;;
        switch|account|id|pool)
            local ids=$(amux account list 2>/dev/null | awk 'NR>1 && $1 ~ /:/ {print $1}')
            local subcmds=""
            if [[ ${cword} -eq 2 ]]; then
                case "${words[1]}" in
                    account|id) subcmds="list switch add remove on off threshold health" ;;
                    pool) subcmds="add remove" ;;
                esac
            fi
            COMPREPLY=( $(compgen -W "${subcmds} ${ids}" -- "${cur}") )
            ;;
        hook|unhook)
            COMPREPLY=( $(compgen -W "claude cursor codex agy all" -- "${cur}") )
            ;;
        mcp)
            COMPREPLY=( $(compgen -W "install uninstall status config tools" -- "${cur}") )
            ;;
        completion)
            COMPREPLY=( $(compgen -W "bash zsh fish" -- "${cur}") )
            ;;
        vault)
            COMPREPLY=( $(compgen -W "export import info" -- "${cur}") )
            ;;
        *)
            ;;
    esac
}

complete -F _amux_completions amux
`

const zshCompletionScript = `#compdef amux
# zsh completion for amux

_amux() {
    local -a commands
    commands=(
        'start:Start gateway daemon in background or foreground'
        'stop:Stop gateway background daemon'
        'restart:Restart gateway daemon'
        'status:Gateway, hooked tools and accounts'
        'off:Unhook every tool and stop the gateway'
        'login:Add an account'
        'switch:Make an account the active login (no re-login)'
        'account:List, turn on/off or remove accounts'
        'pool:Accounts amux may switch between automatically'
        'hook:Point a tool at the gateway'
        'unhook:Undo hook'
        'mcp:Serve or install the amux MCP server'
        'env:Output shell export variables'
        'doctor:Run system and credential diagnostics'
        'audit:Verify vault encryption and security posture'
        'usage:Token usage and analytics'
        'setup:Run guided setup wizard'
        'config:Inspect or modify configuration properties'
        'threshold:Get or set failover threshold'
        'migrate:Non-destructive migration to flat Identity model'
        'vault:Export or import encrypted credential vaults'
        'completion:Generate shell autocompletion script'
        'update:Update amux to latest release'
        'uninstall:Uninstall amux and clean up hooks'
    )

    local -a providers
    providers=(
        'claude:Anthropic Claude Code OAuth'
        'codex:OpenAI Codex CLI'
        'agy:Google Antigravity'
        'gemini:Google Gemini API'
        'chatgpt:ChatGPT Web Session'
        'muse:Meta Muse web'
        'gemini-web:Gemini web session'
        'groq:Groq API key'
    )

    _arguments -C \
        '1: :->command' \
        '*:: :->args'

    case $state in
        command)
            _describe -t commands 'amux command' commands
            ;;
        args)
            case $line[1] in
                login)
                    _describe -t providers 'provider' providers
                    ;;
                switch|pool|account)
                    local -a ids
                    ids=(${(f)"$(amux account list 2>/dev/null | awk 'NR>1 && $1 ~ /:/ {print $1}')"})
                    _describe -t ids 'account id' ids
                    ;;
                hook|unhook)
                    _values 'targets' 'claude' 'cursor' 'codex' 'agy' 'all'
                    ;;
                mcp)
                    _values 'subcommands' 'install' 'uninstall' 'status' 'config' 'tools'
                    ;;
                completion)
                    _values 'shells' 'bash' 'zsh' 'fish'
                    ;;
                vault)
                    _values 'subcommands' 'export' 'import' 'info'
                    ;;
            esac
            ;;
    esac
}

_amux "$@"
`

const fishCompletionScript = `# fish completion for amux

set -l commands start stop restart status off login switch account id pool hook unhook env doctor audit usage setup config threshold migrate vault completion update uninstall mcp

complete -c amux -f
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a start -d "Start gateway daemon"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a stop -d "Stop gateway daemon"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a restart -d "Restart gateway daemon"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a status -d "Inspect accounts and gateway status"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a login -d "Authenticate an account"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a switch -d "Switch active account"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a account -d "Manage accounts and quotas"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a off -d "Unhook everything and stop the gateway"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a pool -d "Accounts amux may switch between automatically"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a hook -d "Hook IDE settings to gateway"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a unhook -d "Restore IDE settings to direct"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a mcp -d "Serve or install the amux MCP server"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a doctor -d "Run diagnostic checks"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a audit -d "Security posture audit"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a vault -d "Export or import vault"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a completion -d "Generate shell completion"

# Completion for login providers
complete -c amux -n "__fish_seen_subcommand_from login" -a "claude codex agy chatgpt gemini-web muse gemini groq kimi grok github cursor"
complete -c amux -n "__fish_seen_subcommand_from hook unhook" -a "claude cursor codex agy all"
complete -c amux -n "__fish_seen_subcommand_from pool" -a "add remove"

# Completion for completion shells
complete -c amux -n "__fish_seen_subcommand_from completion" -a "bash zsh fish"

# Completion for vault
complete -c amux -n "__fish_seen_subcommand_from vault" -a "export import info"
`
