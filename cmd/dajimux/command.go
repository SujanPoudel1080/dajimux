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
