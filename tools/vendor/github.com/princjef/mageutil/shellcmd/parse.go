package shellcmd

import (
	"fmt"
	"strings"
)

type (
	cmdParser struct {
		args    []string
		current strings.Builder
		quote   quoteType
		started bool
		escaped bool
	}

	quoteType int
)

const (
	notQuoted quoteType = iota // intentionally 0 to be default value
	singleQuoted
	doubleQuoted
)

func (p *cmdParser) parse(cmd string) ([]string, error) {
	// Clear out in case there was a previous run
	p.args = nil
	p.current = strings.Builder{}
	p.quote = notQuoted
	p.started = false
	p.escaped = false

	for _, r := range cmd {
		switch r {
		case '"':
			p.handleDoubleQuote(r)
		case '\'':
			p.handleSingleQuote(r)
		case ' ', '\t', '\n', '\r':
			p.handleWhitespace(r)
		case '\\':
			p.handleBackslash(r)
		default:
			p.handleNormal(r)
		}
	}

	// If we're still in a quote, then we have an unterminated quote
	if p.quote != notQuoted {
		return nil, fmt.Errorf("shellcmd: unterminated quote in command: %s", cmd)
	}

	// If there is still an escape, we need to write it (should this be an error?)
	if p.escaped {
		p.current.WriteRune('\\')
		p.started = true
	}

	// If we still have a started literal or there are no args, we need to
	// fill in the last arg
	if p.started || len(p.args) == 0 {
		p.args = append(p.args, p.current.String())
	}

	return p.args, nil
}

func (p *cmdParser) handleSingleQuote(r rune) {
	switch p.quote {
	case notQuoted:
		if p.escaped {
			p.current.WriteRune(r)
		} else {
			p.quote = singleQuoted
		}
	case singleQuoted:
		if p.escaped {
			p.current.WriteRune(r)
		} else {
			p.quote = notQuoted
		}
	case doubleQuoted:
		if p.escaped {
			p.current.WriteRune('\\')
		}

		p.current.WriteRune(r)
	}

	p.started = true
	p.escaped = false
}

func (p *cmdParser) handleDoubleQuote(r rune) {
	switch p.quote {
	case notQuoted:
		if p.escaped {
			p.current.WriteRune(r)
		} else {
			p.quote = doubleQuoted
		}
	case singleQuoted:
		if p.escaped {
			p.current.WriteRune('\\')
		}

		p.current.WriteRune(r)
	case doubleQuoted:
		if p.escaped {
			p.current.WriteRune(r)
		} else {
			p.quote = notQuoted
		}
	}

	p.started = true
	p.escaped = false
}

func (p *cmdParser) handleWhitespace(r rune) {
	switch p.quote {
	case notQuoted:
		if p.escaped {
			p.current.WriteRune(r)
			p.started = true
			break
		}

		// End the segment
		if p.started {
			p.args = append(p.args, p.current.String())
			p.current = strings.Builder{}
			p.quote = notQuoted
			p.started = false
		}
	case singleQuoted, doubleQuoted:
		if p.escaped {
			p.current.WriteRune('\\')
		}

		p.current.WriteRune(r)
		p.started = true
	}

	p.escaped = false
}

func (p *cmdParser) handleBackslash(r rune) {
	if p.escaped {
		p.current.WriteRune(r)
		p.escaped = false
	} else {
		p.escaped = true
	}

	p.started = true
}

func (p *cmdParser) handleNormal(r rune) {
	if p.escaped {
		p.current.WriteRune('\\')
	}

	p.current.WriteRune(r)

	p.escaped = false
	p.started = true
}
