package main

// Command represents an executable CLI command.
type Command struct {
	Name        string
	Aliases     []string
	Usage       string
	Description string
	Run         func(args []string) error
}

// Registry manages the collection of CLI commands and alias lookups.
type Registry struct {
	commands []*Command
	byName   map[string]*Command
}

// NewRegistry initializes an empty command registry.
func NewRegistry() *Registry {
	return &Registry{
		commands: make([]*Command, 0),
		byName:   make(map[string]*Command),
	}
}

// Register adds a command and registers all its aliases.
func (r *Registry) Register(cmd *Command) {
	if cmd == nil || cmd.Name == "" {
		return
	}
	r.commands = append(r.commands, cmd)
	r.byName[cmd.Name] = cmd
	for _, alias := range cmd.Aliases {
		r.byName[alias] = cmd
	}
}

// Find resolves a command by its primary name or any of its aliases.
func (r *Registry) Find(name string) (*Command, bool) {
	cmd, ok := r.byName[name]
	return cmd, ok
}

// Commands returns all registered commands in registration order.
func (r *Registry) Commands() []*Command {
	return r.commands
}

// BuildDefaultRegistry registers all standard dajimux commands.
func BuildDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(newAttachCommand())
	r.Register(newListCommand())
	r.Register(newDaemonCommand())
	r.Register(newStopServerCommand())
	r.Register(newKillSessionCommand())
	r.Register(newRenameSessionCommand())
	r.Register(newSessionCommand())
	r.Register(newRestoreCommand())
	r.Register(newServiceCommand())
	r.Register(newMCPCommand())
	r.Register(newConfigCommand())
	r.Register(newCompletionCommand())
	r.Register(newManCommand())
	r.Register(newAgentCommand())
	r.Register(newMsgCommand())
	r.Register(newPaneCommand())
	r.Register(newPeerCommand())
	r.Register(newLayoutCommand())
	r.Register(newWorkspaceCommand())
	r.Register(newVersionCommand())
	r.Register(newHelpCommand())
	return r
}
