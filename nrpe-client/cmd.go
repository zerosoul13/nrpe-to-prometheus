package main

import "strings"

// Command represents command name and argument list
type Command struct {
	Name string
	Args []string
}

// NewCommand creates Command object with the given name and optional argument list
func NewCommand(name string, args ...string) Command {
	return Command{
		Name: name,
		Args: args,
	}
}

// toStatusLine convers Command content to single status line string
func (c Command) toStatusLine() string {
	if c.Args != nil && len(c.Args) > 0 {
		args := strings.Join(c.Args, "!")
		return c.Name + "!" + args
	}

	return c.Name
}
