package cli

import (
	"amux-accounts/pkg/monitor"
	"amux-accounts/pkg/ui"
)

// CommandHandler represents a function handling a CLI subcommand execution.
type CommandHandler func(args []string)

// HelpHandler represents a function displaying command-specific help documentation.
type HelpHandler func()

// CommandRoute groups execution logic, help information, and domain metadata for a command.
type CommandRoute struct {
	Domain      string
	Handler     CommandHandler
	HelpHandler HelpHandler
}

// Router dispatches CLI arguments to domain-based command handlers.
type Router struct {
	routes map[string]CommandRoute
}

// NewRouter constructs and registers all domain-based CLI command handlers.
func NewRouter() *Router {
	r := &Router{
		routes: make(map[string]CommandRoute),
	}
	r.registerIdentityRoutes()
	r.registerGatewayRoutes()
	r.registerDiagnosticRoutes()
	return r
}

// Register adds a new command route to the router.
func (r *Router) Register(name string, route CommandRoute) {
	r.routes[name] = route
}

// GetRoute returns the route definition for a given command name if found.
func (r *Router) GetRoute(name string) (CommandRoute, bool) {
	route, ok := r.routes[name]
	return route, ok
}

// registerIdentityRoutes registers commands related to identity, accounts, and credentials.
func (r *Router) registerIdentityRoutes() {
	domain := "identity"

	r.Register("login", CommandRoute{
		Domain:      domain,
		Handler:     ui.CmdLogin,
		HelpHandler: helpLogin,
	})

	accountRoute := CommandRoute{
		Domain:      domain,
		Handler:     CmdAccount,
		HelpHandler: helpAccount,
	}
	r.Register("account", accountRoute)
	r.Register("accounts", accountRoute)
	r.Register("id", accountRoute)

	r.Register("switch", CommandRoute{
		Domain:      domain,
		Handler:     cmdIDSelect,
		HelpHandler: helpSwitch,
	})

	r.Register("migrate", CommandRoute{
		Domain:  domain,
		Handler: CmdMigrate,
	})

	r.Register("vault", CommandRoute{
		Domain:      domain,
		Handler:     CmdVault,
		HelpHandler: cmdVaultHelp,
	})

	r.Register("use", CommandRoute{
		Domain:  domain,
		Handler: CmdUse,
	})

	r.Register("project", CommandRoute{
		Domain:  domain,
		Handler: CmdProject,
	})
}

// registerGatewayRoutes registers commands related to daemon lifecycle, proxy, and IDE hooks.
func (r *Router) registerGatewayRoutes() {
	domain := "gateway"

	r.Register("start", CommandRoute{
		Domain:      domain,
		Handler:     cmdGatewayStart,
		HelpHandler: helpStart,
	})

	r.Register("stop", CommandRoute{
		Domain:      domain,
		Handler:     cmdGatewayStop,
		HelpHandler: helpStop,
	})

	r.Register("restart", CommandRoute{
		Domain:      domain,
		Handler:     cmdGatewayRestart,
		HelpHandler: helpRestart,
	})

	r.Register("hook", CommandRoute{
		Domain:      domain,
		Handler:     cmdGatewayHook,
		HelpHandler: helpHook,
	})

	r.Register("unhook", CommandRoute{
		Domain:      domain,
		Handler:     cmdGatewayUnhook,
		HelpHandler: helpUnhook,
	})

	r.Register("env", CommandRoute{
		Domain:      domain,
		Handler:     CmdEnv,
		HelpHandler: helpEnv,
	})

	r.Register("gateway", CommandRoute{
		Domain:  domain,
		Handler: CmdGateway,
	})
}

// registerDiagnosticRoutes registers commands related to diagnostics, configuration, telemetry, and system.
func (r *Router) registerDiagnosticRoutes() {
	domain := "diagnostics"

	r.Register("status", CommandRoute{
		Domain:      domain,
		Handler:     CmdStatus,
		HelpHandler: helpStatus,
	})

	r.Register("doctor", CommandRoute{
		Domain:      domain,
		Handler:     CmdDoctor,
		HelpHandler: helpDoctor,
	})

	r.Register("audit", CommandRoute{
		Domain:  domain,
		Handler: CmdAudit,
	})

	r.Register("usage", CommandRoute{
		Domain:      domain,
		Handler:     CmdUsage,
		HelpHandler: helpUsage,
	})

	r.Register("setup", CommandRoute{
		Domain:  domain,
		Handler: CmdSetup,
	})

	r.Register("config", CommandRoute{
		Domain:  domain,
		Handler: CmdConfig,
	})

	r.Register("threshold", CommandRoute{
		Domain: domain,
		Handler: func(args []string) {
			CmdConfig(append([]string{"threshold"}, args...))
		},
	})

	r.Register("update", CommandRoute{
		Domain:  domain,
		Handler: CmdUpdate,
	})

	r.Register("uninstall", CommandRoute{
		Domain:  domain,
		Handler: CmdUninstall,
	})

	r.Register("feedback", CommandRoute{
		Domain:  domain,
		Handler: CmdFeedback,
	})

	r.Register("completion", CommandRoute{
		Domain:  domain,
		Handler: CmdCompletion,
	})

	r.Register("statusline", CommandRoute{
		Domain: domain,
		Handler: func(_ []string) {
			ui.CmdStatusline()
		},
	})
}

// Dispatch executes routing for incoming command-line arguments.
func (r *Router) Dispatch(rawArgs []string) {
	if len(rawArgs) < 2 {
		usageHelp()
		return
	}
	monitor.EnableTermSink()
	autoMigrateCheck()

	cmd := rawArgs[1]
	args := rawArgs[2:]

	if cmd == "help" || cmd == "-h" || cmd == "--help" {
		usageHelp()
		return
	}

	route, exists := r.routes[cmd]
	if !exists {
		die("unknown command: %s (run 'amux help' for usage)", cmd)
		return
	}

	if hasHelp(args) {
		if route.HelpHandler != nil {
			route.HelpHandler()
			return
		}
		usageHelp()
		return
	}

	if route.Handler != nil {
		route.Handler(args)
	}
}
