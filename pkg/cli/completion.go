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

    local commands="start stop restart status hook unhook env login account id switch doctor audit usage setup config threshold migrate update uninstall feedback completion vault"
    local providers="claude codex antigravity gemini chatgpt api cursor"

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
        switch|account|id)
            if [[ ${cword} -eq 2 ]]; then
                local ids=$(amux account list 2>/dev/null | awk 'NR>2 {print $1}')
                local subcmds="list switch add remove logout on off auto threshold health"
                COMPREPLY=( $(compgen -W "${subcmds} ${ids}" -- "${cur}") )
            fi
            ;;
        hook|unhook)
            COMPREPLY=( $(compgen -W "--claude --cursor --codex --agy --all" -- "${cur}") )
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
        'status:Real-time dashboard of accounts and gateway state'
        'login:Authenticate account via OAuth or API key'
        'switch:Switch active account in Keychain & IDE configs'
        'account:Manage accounts, quotas, and failover status'
        'id:Alias for account management'
        'hook:Hook IDE settings to gateway proxy'
        'unhook:Restore IDE settings to direct upstream'
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
        'antigravity:Google Antigravity / Gemini'
        'gemini:Google Gemini API'
        'chatgpt:ChatGPT Web Session'
        'api:Generic OpenAI/Anthropic Compatible API'
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
                switch)
                    local -a ids
                    ids=(${(f)"$(amux account list 2>/dev/null | awk 'NR>2 {print $1}')"})
                    _describe -t ids 'account id' ids
                    ;;
                hook|unhook)
                    _values 'targets' \
                        '--claude[Hook Claude Code]' \
                        '--cursor[Hook Cursor IDE]' \
                        '--codex[Hook OpenAI Codex CLI]' \
                        '--agy[Hook Google Antigravity]' \
                        '--all[Hook all supported IDEs]'
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

set -l commands start stop restart status login switch account id hook unhook env doctor audit usage setup config threshold migrate vault completion update uninstall

complete -c amux -f
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a start -d "Start gateway daemon"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a stop -d "Stop gateway daemon"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a restart -d "Restart gateway daemon"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a status -d "Inspect accounts and gateway status"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a login -d "Authenticate an account"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a switch -d "Switch active account"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a account -d "Manage accounts and quotas"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a hook -d "Hook IDE settings to gateway"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a unhook -d "Restore IDE settings to direct"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a doctor -d "Run diagnostic checks"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a audit -d "Security posture audit"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a vault -d "Export or import vault"
complete -c amux -n "not __fish_seen_subcommand_from $commands" -a completion -d "Generate shell completion"

# Completion for login providers
complete -c amux -n "__fish_seen_subcommand_from login" -a "claude codex antigravity gemini chatgpt api"

# Completion for completion shells
complete -c amux -n "__fish_seen_subcommand_from completion" -a "bash zsh fish"

# Completion for vault
complete -c amux -n "__fish_seen_subcommand_from vault" -a "export import info"
`
