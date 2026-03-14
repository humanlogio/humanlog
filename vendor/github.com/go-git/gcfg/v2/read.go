package gcfg

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-git/gcfg/v2/scanner"
	"github.com/go-git/gcfg/v2/token"
)

var unescape = map[rune]rune{'\\': '\\', '"': '"', 'n': '\n', 't': '\t', 'b': '\b', '\n': '\n'}

// no error: invalid literals should be caught by scanner
func unquote(s string) (string, error) {
	u, q, esc := make([]rune, 0, len(s)), false, false
	for _, c := range s {
		if esc {
			uc, ok := unescape[c]
			switch {
			case ok:
				u = append(u, uc)
				fallthrough
			case !q && c == '\n':
				esc = false
				continue
			}
			return "", ErrMissingEscapeSequence
		}
		switch c {
		case '"':
			q = !q
		case '\\':
			esc = true
		default:
			u = append(u, c)
		}
	}
	if q {
		return "", ErrMissingEndQuote
	}
	if esc {
		return "", ErrMissingEscapeSequence
	}
	return string(u), nil
}

func read(callback func(string, string, string, string, bool) error,
	fset *token.FileSet, file *token.File, src []byte) error {
	//
	var s scanner.Scanner
	var errs scanner.ErrorList
	s.Init(file, src, func(p token.Position, m string) { errs.Add(p, m) }, 0)
	sect, sectsub := "", ""
	pos, tok, lit, err := s.Scan()
	errfn := func(msg string) error {
		return fmt.Errorf("%s: %s", fset.Position(pos), msg)
	}
	if err != nil {
		return err
	}
	var accErr error
	for {
		if errs.Len() > 0 {
			if err, fatal := joinNonFatal(accErr, errs.Err()); fatal {
				return err
			}
		}
		switch tok {
		case token.EOF:
			return nil
		case token.EOL, token.COMMENT:
			pos, tok, lit, err = s.Scan()
			if err != nil {
				return err
			}
		case token.LBRACK:
			pos, tok, lit, err = s.Scan()
			if err != nil {
				return err
			}
			if errs.Len() > 0 {
				if err, fatal := joinNonFatal(accErr, errs.Err()); fatal {
					return err
				}
			}
			if tok != token.IDENT {
				if err, fatal := joinNonFatal(accErr, errfn("expected section name")); fatal {
					return err
				}
			}
			sect, sectsub = lit, ""
			pos, tok, lit, err = s.Scan()
			if err != nil {
				return err
			}
			if errs.Len() > 0 {
				if err, fatal := joinNonFatal(accErr, errs.Err()); fatal {
					return err
				}
			}
			if tok == token.STRING {
				ss, err := unquote(lit)
				if err != nil {
					return err
				}

				sectsub = ss
				pos, tok, lit, err = s.Scan()
				if err != nil {
					return err
				}
				if errs.Len() > 0 {
					if err, fatal := joinNonFatal(accErr, errs.Err()); fatal {
						return err
					}
				}
			}
			if tok != token.RBRACK {
				if err, fatal := joinNonFatal(accErr, errfn("expected right bracket")); fatal {
					return err
				}
			}
			pos, tok, lit, err = s.Scan()
			if err != nil {
				return err
			}
			if tok != token.EOL && tok != token.EOF && tok != token.COMMENT {
				if err, fatal := joinNonFatal(accErr, errfn("expected EOL, EOF, or comment")); fatal {
					return err
				}
			}
			// If a section/subsection header was found, ensure a
			// container object is created, even if there are no
			// variables further down.
			err := callback(sect, sectsub, "", "", true)
			if err != nil {
				return err
			}
		case token.IDENT:
			if sect == "" {
				if err, fatal := joinNonFatal(accErr, errfn("expected section header")); fatal {
					return err
				}
			}
			n := lit
			pos, tok, lit, err = s.Scan()
			if err != nil {
				return err
			}
			if errs.Len() > 0 {
				return errs.Err()
			}
			blank, v := tok == token.EOF || tok == token.EOL || tok == token.COMMENT, ""
			if !blank {
				if tok != token.ASSIGN {
					if err, fatal := joinNonFatal(accErr, errfn("expected '='")); fatal {
						return err
					}
				}
				pos, tok, lit, err = s.Scan()
				if err != nil {
					return err
				}
				if errs.Len() > 0 {
					if err, fatal := joinNonFatal(accErr, errs.Err()); fatal {
						return err
					}
				}
				if tok != token.STRING {
					if err, fatal := joinNonFatal(accErr, errfn("expected value")); fatal {
						return err
					}
				}
				unq, err := unquote(lit)
				if err != nil {
					return err
				}

				v = unq
				pos, tok, lit, err = s.Scan()
				if err != nil {
					return err
				}
				if errs.Len() > 0 {
					if err, fatal := joinNonFatal(accErr, errs.Err()); fatal {
						return err
					}
				}
				if tok != token.EOL && tok != token.EOF && tok != token.COMMENT {
					if err, fatal := joinNonFatal(accErr, errfn("expected EOL, EOF, or comment")); fatal {
						return err
					}
				}
			}
			err := callback(sect, sectsub, n, v, blank)
			if err != nil {
				return err
			}
		default:
			if sect == "" {
				if err, fatal := joinNonFatal(accErr, errfn("expected section header")); fatal {
					return err
				}
			}
			if err, fatal := joinNonFatal(accErr, errfn("expected section header or variable declaration")); fatal {
				return err
			}
		}
	}
}

func readInto(config interface{}, fset *token.FileSet, file *token.File,
	src []byte) error {
	//
	firstPassCallback := func(s string, ss string, k string, v string, bv bool) error {
		return set(config, s, ss, k, v, bv, false)
	}
	err := read(firstPassCallback, fset, file, src)
	if err != nil {
		return err
	}
	secondPassCallback := func(s string, ss string, k string, v string, bv bool) error {
		return set(config, s, ss, k, v, bv, true)
	}
	return read(secondPassCallback, fset, file, src)
}

// ReadWithCallback reads gcfg formatted data from reader and calls
// callback with each section and option found.
//
// Callback is called with section, subsection, option key, option value
// and blank value flag as arguments.
//
// When a section is found, callback is called with nil subsection, option key
// and option value.
//
// When a subsection is found, callback is called with nil option key and
// option value.
//
// If blank value flag is true, it means that the value was not set for an option
// (as opposed to set to empty string).
//
// If callback returns an error, ReadWithCallback terminates with an error too.
func ReadWithCallback(reader io.Reader, callback func(string, string, string, string, bool) error) error {
	src, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	fset := token.NewFileSet()
	file, err := fset.AddFile("", fset.Base(), len(src))
	if err != nil {
		return err
	}

	return read(callback, fset, file, src)
}

// ReadInto reads gcfg formatted data from reader and sets the values into the
// corresponding fields in config.
func ReadInto(config interface{}, reader io.Reader) error {
	src, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := fset.AddFile("", fset.Base(), len(src))
	if err != nil {
		return err
	}
	return readInto(config, fset, file, src)
}

// ReadStringInto reads gcfg formatted data from str and sets the values into
// the corresponding fields in config.
func ReadStringInto(config interface{}, str string) error {
	r := strings.NewReader(str)
	return ReadInto(config, r)
}

// ReadFileInto reads gcfg formatted data from the file filename and sets the
// values into the corresponding fields in config.
func ReadFileInto(config interface{}, filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	src, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := fset.AddFile(filename, fset.Base(), len(src))
	if err != nil {
		return err
	}
	return readInto(config, fset, file, src)
}
