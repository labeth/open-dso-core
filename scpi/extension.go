package scpi

import (
	"errors"
	"strings"
)

// Extension is a scope-only header a board brings beside its dialect: its
// long-form pattern without "?" and the handlers of its command and query
// forms, nil where a form does not exist. Handlers never touch hardware;
// they queue work the board runs on its owner goroutine.
type Extension struct {
	Header  string
	Command func(args string) error
	Query   func(args string) ([]byte, error)
}

// Extensions are the commands of exts. A refused command or query is -200;
// a query's reply is written as one value.
func Extensions(exts []Extension) []Command {
	var out []Command
	for _, x := range exts {
		pattern := strings.TrimSuffix(x.Header, "?")
		if x.Command != nil {
			run := x.Command
			out = append(out, Command{pattern, func(c *Call) error {
				if err := run(c.Arg()); err != nil {
					return errors.Join(ErrFailed, err)
				}
				return nil
			}})
		}
		if x.Query != nil {
			run := x.Query
			out = append(out, Command{pattern + "?", func(c *Call) error {
				b, err := run(c.Arg())
				if err != nil {
					return errors.Join(ErrFailed, err)
				}
				c.Text(string(b))
				return nil
			}})
		}
	}
	return out
}
