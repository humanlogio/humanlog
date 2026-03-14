package gcfg

import (
	"errors"
	"fmt"
)

var (
	ErrSyntaxWarning = errors.New("syntax warning")

	ErrMissingEscapeSequence = errors.New("missing escape sequence")
	ErrMissingEndQuote       = errors.New("missing end quote")
)

// FatalOnly filters the results of a Read*Into invocation and returns only
// fatal errors. That is, errors (warnings) indicating data for unknown
// sections / variables is ignored. Example invocation:
//
//	err := gcfg.FatalOnly(gcfg.ReadFileInto(&cfg, configFile))
//	if err != nil {
//	    ...
func FatalOnly(err error) error {
	for {
		if err == nil {
			return nil
		}
		err = errors.Unwrap(err)
		if !errors.Is(err, ErrSyntaxWarning) {
			return err
		}
	}
}

func newSyntaxWarning(sec, sub, variable string) error {
	msg := fmt.Sprintf("can't store data in section %q", sec)
	if sub != "" {
		msg += fmt.Sprintf(", subsection %q", sub)
	}
	if variable != "" {
		msg += fmt.Sprintf(", variable %q", variable)
	}
	return fmt.Errorf("%w: %s", ErrSyntaxWarning, msg)
}

func joinNonFatal(prev, cur error) (error, bool) {
	if !errors.Is(cur, ErrSyntaxWarning) {
		return cur, true
	}
	return errors.Join(prev, cur), false
}
