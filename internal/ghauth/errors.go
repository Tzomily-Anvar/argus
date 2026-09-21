package ghauth

import "errors"

// errorsAs is errors.As under a shorter name, used by the keyring
// backends to tell "the tool said no such item" from "the tool broke".
func errorsAs(err error, target any) bool { return errors.As(err, target) }
