package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/gh"
)

// Setting Argus up by answering three questions.
//
// `argus init` writes the annotated file and leaves the reader to it,
// which suits someone who wants to see every setting before choosing.
// This is for everyone else: it asks only what has no sensible default,
// checks each answer against GitHub while the person is still here to
// correct it, and writes the same file at the end.
//
// Nothing here is required. The file it writes is the file `argus init`
// writes, so the two paths converge and neither becomes the "real" one.

// errNoInput ends the wizard when the answers run out.
//
// Every prompt returns this rather than quietly taking its default. A
// half-answered wizard writes a configuration nobody asked for, and a
// reader that has reached the end returns io.EOF for ever after, which
// turns any "ask again" loop into a spin that fills the terminal as fast
// as it can print.
var errNoInput = errors.New(
	"standard input ended before setup finished. Run `argus init` and edit the file instead")

func setup() error {
	if !interactive() {
		return fmt.Errorf("`argus setup` asks questions, so it needs a terminal. " +
			"Use `argus init` and edit the file instead")
	}

	// One reader for the whole wizard. A prompt that makes its own
	// throws away whatever the previous one left in the buffer, and an
	// answer going missing two questions later is not something anyone
	// can diagnose from the outside.
	w := &wizard{in: bufio.NewReader(os.Stdin)}

	fmt.Printf("\n  Setting up Argus.\n" +
		"  Press enter to accept anything in [brackets].\n\n")

	path := config.ConfigFile()
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("  There is already a configuration at\n    %s\n\n", path)
		again, err := w.confirm("Go through setup again and overwrite it?", false)
		if err != nil {
			return err
		}
		if !again {
			fmt.Printf("\n  Left it alone. `argus doctor` will tell you what it thinks.\n\n")
			return nil
		}
		fmt.Println()
	}

	// Signing in first, so the account can be checked for real rather
	// than just written down and discovered to be wrong later.
	if !appSource().SignedIn() {
		fmt.Printf("  Argus signs in to GitHub with a device code. Nothing to create,\n" +
			"  no token to paste, and it can only ever read what you can read.\n\n")
		now, err := w.confirm("Sign in now?", true)
		if err != nil {
			return err
		}
		if now {
			if err := appSession(true); err != nil {
				return err
			}
		} else {
			fmt.Printf("\n  Fine - set ARGUS_GITHUB_TOKEN in the file instead, or run\n" +
				"  `argus login` later.\n\n")
		}
	} else {
		fmt.Printf("  Already signed in to GitHub.\n\n")
	}

	org, err := w.askAccount()
	if err != nil {
		return err
	}

	jira := map[string]string{}
	fmt.Printf("\n  The sprint report reads Jira. It is optional, and everything\n" +
		"  else works without it.\n\n")
	sprint, err := w.confirm("Set up the sprint report?", false)
	if err != nil {
		return err
	}
	if sprint {
		// None of these have a default that could be right for two
		// different Jira sites, so a blank answer is asked again rather
		// than written as an empty setting the sprint report then
		// refuses to start on.
		for _, q := range []struct{ key, prompt, def string }{
			{"ARGUS_JIRA_BASE_URL", "Jira site URL", "https://yourcompany.atlassian.net"},
			{"ARGUS_JIRA_EMAIL", "Jira account email", ""},
			{"ARGUS_JIRA_PROJECT", "Jira project key", ""},
		} {
			v, err := w.ask(q.prompt, q.def, "    The sprint report cannot work this one out for itself.")
			if err != nil {
				return err
			}
			jira[q.key] = v
		}
		fmt.Printf("\n  The Jira API token is a secret, so it does not go in the file.\n" +
			"  Set ARGUS_JIRA_TOKEN in your environment, or add it to the file\n" +
			"  yourself if you would rather keep it there.\n")
	}

	if err := writeSetup(path, org, jira); err != nil {
		return err
	}

	fmt.Printf("\n  Wrote %s\n\n", path)
	fmt.Printf("  Next:\n")
	fmt.Printf("      argus doctor            check it over\n")
	fmt.Printf("      argus                   run it\n")
	fmt.Printf("      argus service install   keep it running in the background\n\n")
	return nil
}

// wizard holds the one reader every prompt shares.
type wizard struct{ in *bufio.Reader }

// askAccount keeps asking until GitHub recognises the answer, because a
// name that does not exist produces a dashboard that is empty for a
// reason nobody can see.
//
// Either kind of account is accepted. Plenty of people keep their
// repositories under their own profile rather than in an organisation,
// and asking them for an organisation they do not have - then refusing
// their username - is the wizard failing at the one question it exists
// to ask.
func (w *wizard) askAccount() (string, error) {
	for {
		answer, err := w.ask("GitHub organisation or username", "",
			"    Argus needs one to sweep. It is the name in\n"+
				"    github.com/<name>, whether that is an organisation or you.")
		if err != nil {
			return "", err
		}
		name, ok := accountFromAnswer(answer)
		if !ok {
			fmt.Printf("    ✗ that does not look like a GitHub name. It is the bare name in\n" +
				"      github.com/<name>: letters, digits and hyphens.\n")
			continue
		}
		if name != answer {
			// Said out loud rather than done quietly. Pasting the
			// address instead of the name is the common answer here, but
			// a value taken out of it is still a guess at what was meant.
			fmt.Printf("    Taking %q from that.\n", name)
		}
		kind, result := checkAccount(name)
		switch result {
		case accountOK:
			// Naming what was found is the confirmation. Someone who
			// meant their organisation and typed their username sees
			// the difference here, while it still costs nothing to fix.
			fmt.Printf("    ✓ found, a %s\n", kind.Label())
			return name, nil
		case accountNoAuth:
			fmt.Printf("    ? not signed in, so this cannot be checked now - taking it as given\n")
			return name, nil
		default:
			fmt.Printf("    ✗ GitHub does not show an account of that name to you.\n" +
				"      Check the spelling, and that you can see it while signed in.\n")
			anyway, err := w.confirm("Use it anyway?", false)
			if err != nil {
				return "", err
			}
			if anyway {
				return name, nil
			}
		}
	}
}

// accountFromAnswer pulls the account name out of whatever was typed.
//
// The answer is often the browser's address bar rather than the name,
// because that is where someone looks when asked which account they
// mean. Anything that is still not a name after that is refused:
// ARGUS_GITHUB_ORG goes straight into an API path, so a wrong value here
// buys an empty dashboard and no explanation of why.
func accountFromAnswer(answer string) (string, bool) {
	s := strings.TrimSpace(answer)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	s, _, _ = strings.Cut(s, "?")
	s, _, _ = strings.Cut(s, "#")

	for _, part := range strings.Split(s, "/") {
		switch {
		case part == "":
			continue
		case strings.Contains(part, "."):
			continue // the host of a pasted address
		case part == "orgs" || part == "enterprises":
			continue // what github.com puts in front of the name
		}
		if !validLogin(part) {
			return "", false
		}
		return part, true
	}
	return "", false
}

// validLogin follows GitHub's own rule for a login: letters, digits and
// hyphens, none at either end, and no more than 39 characters. It is the
// same rule for an organisation and for a person.
//
// Checked here as well as against the API so that a typo is still caught
// when nobody is signed in and the name cannot be looked up.
func validLogin(s string) bool {
	if s == "" || len(s) > 39 || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

type accountResult int

const (
	accountBad accountResult = iota
	accountOK
	accountNoAuth
)

// checkAccount asks GitHub what the name is.
//
// This used to ask /orgs/{name}, which answers 404 for a person - so a
// valid personal account was rejected as a typo. /users/{name} answers
// for both kinds and says which, so the same one request now validates
// the name and decides how the rest of Argus will read it.
func checkAccount(name string) (gh.AccountKind, accountResult) {
	client, err := gh.NewFromEnv()
	if err != nil {
		return gh.AccountUnknown, accountNoAuth
	}
	kind, err := client.AccountKindOf(name)
	if err != nil {
		return gh.AccountUnknown, accountBad
	}
	return kind, accountOK
}

// writeSetup starts from the same annotated template `argus init` uses,
// so someone who opens the file afterwards finds every other setting
// documented rather than a bare three lines.
func writeSetup(path, org string, extra map[string]string) error {
	// The template is only available by writing it, so it is written
	// beside the real file and read back. Going through a temporary file
	// also means a failure part way through leaves the configuration
	// that is already there, rather than deleting it and then not
	// managing to replace it.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".config.setup.%d.env", os.Getpid()))
	_ = os.Remove(tmp)
	defer os.Remove(tmp)

	if err := config.WriteStarter(tmp); err != nil {
		return err
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	out := strings.Replace(string(b),
		"ARGUS_GITHUB_ORG=your-org-here", "ARGUS_GITHUB_ORG="+org, 1)

	var add []string
	for _, k := range []string{"ARGUS_JIRA_BASE_URL", "ARGUS_JIRA_EMAIL", "ARGUS_JIRA_PROJECT"} {
		if v := extra[k]; v != "" {
			add = append(add, k+"="+v)
		}
	}
	if len(add) > 0 {
		// Answering the Jira questions is how someone asks for the
		// sprint report, but the tool is off unless ARGUS_TOOLS names
		// it. Writing the credentials alone leaves it configured and not
		// running, which looks from the dashboard like nothing happened.
		out = strings.Replace(out, "# ARGUS_TOOLS=pr", "ARGUS_TOOLS=pr,sprint", 1)
		out += "\n# Added by `argus setup`.\n" + strings.Join(add, "\n") + "\n"
	}

	if err := os.WriteFile(tmp, []byte(out), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func interactive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	// /dev/null is a character device too, so the mode on its own says
	// "terminal" for a process started with no terminal at all - launchd,
	// cron, a container run without -i. That is the case this check
	// exists for, and answering every prompt with its default there is
	// how an unattended run ends up sitting on a device code nobody will
	// ever read.
	if null, err := os.Stat(os.DevNull); err == nil && os.SameFile(fi, null) {
		return false
	}
	return true
}

// ask puts one question. A blank answer takes the default; where there
// is no default it asks again, with nudge to explain what it wants.
func (w *wizard) ask(prompt, def, nudge string) (string, error) {
	for {
		if def != "" {
			fmt.Printf("  %s [%s]: ", prompt, def)
		} else {
			fmt.Printf("  %s: ", prompt)
		}
		line, err := w.in.ReadString('\n')
		line = strings.TrimSpace(line)
		// A final line with no newline is still an answer. It is only a
		// read that returns nothing at all that means the input has run
		// out and no amount of asking again will produce more.
		if err != nil && line == "" {
			fmt.Println()
			return "", errNoInput
		}
		if line == "" {
			if def != "" {
				return def, nil
			}
			if nudge != "" {
				fmt.Println(nudge)
			}
			continue
		}
		return line, nil
	}
}

func (w *wizard) confirm(prompt string, def bool) (bool, error) {
	hint, plain := "y/N", "no"
	if def {
		hint, plain = "Y/n", "yes"
	}
	for {
		fmt.Printf("  %s [%s]: ", prompt, hint)
		line, err := w.in.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if err != nil && answer == "" {
			fmt.Println()
			return false, errNoInput
		}
		switch answer {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		case "":
			return def, nil
		}
		// Anything else used to be read as the default, so a typo signed
		// someone in, or overwrote their configuration, without ever
		// looking like an answer that had been misunderstood.
		fmt.Printf("    Answer y or n, or press enter for %s.\n", plain)
	}
}
