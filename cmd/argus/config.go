package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/rules"
	"github.com/Tzomily-Anvar/argus/internal/term"
)

// Seeing and changing settings without opening a file.
//
// Until now the only way to change a setting was to edit config.env by
// hand, and the only way to learn one existed was to read .env.example -
// which is not in a package-manager install at all. Worse, a value can
// come from three places, and the question people actually arrive with is
// never "what is this set to" but "why is it not what I set".
//
// So provenance is the point of this command. Everything else - writing,
// unsetting, validating - falls out of having somewhere that knows what
// the settings are.
//
// This is the one command that must not call config.Load first. Load
// copies the file into the environment, and after that nothing can tell
// the two apart; main.go leaves it alone for exactly that reason.

func configCommand(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}

	switch sub {
	case "list", "ls":
		return configList(args)
	case "get", "show":
		return configGet(args)
	case "set":
		return configSet(args)
	case "unset", "remove", "rm":
		return configUnset(args)
	case "edit":
		return configEdit()
	case "path":
		// Absolute, because this exists to be fed to something else and
		// the answer inside a checkout is the relative ./.env.
		path := config.ActiveFile()
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		fmt.Println(path)
		return nil
	case "help", "-h", "--help":
		configUsage(os.Stdout)
		return nil
	}

	// `argus config ARGUS_PORT` is what people type, and refusing it on a
	// technicality when the word is plainly a setting name helps nobody.
	if _, ok := settings().Find(sub); ok {
		return configGet(append([]string{sub}, args...))
	}

	configUsage(os.Stderr)
	return fmt.Errorf("unknown config command %q", sub)
}

// settings is every setting Argus reads.
//
// The core catalogue, plus one entry per rule and per rule parameter,
// derived from the rule registry rather than written down again. A rule
// registers itself and declares its own knobs, so a rule added tomorrow
// is listed here tomorrow with nobody updating a list - the same source
// the /api/rules endpoint and the dashboard's Rules panel read.
func settings() config.Catalogue {
	all := config.Core()
	for _, r := range rules.All() {
		d := rules.Describe(r)
		section := "Rule: " + str(d["title"])
		all = append(all, config.Setting{
			Key:     str(d["enabled_env"]),
			Section: section,
			Desc:    str(d["description"]),
			Kind:    config.KindBool,
			Default: strconv.FormatBool(r.Enabled),
			Rule:    r.ID,
		})
		params, _ := d["params"].([]map[string]any)
		for _, p := range params {
			kind, def := kindOf(p["default"])
			all = append(all, config.Setting{
				Key:     str(p["env"]),
				Section: section,
				Desc:    str(p["desc"]),
				Kind:    kind,
				Default: def,
				Rule:    r.ID,
			})
		}
	}
	return all
}

func str(v any) string { s, _ := v.(string); return s }

// kindOf reads a parameter's type off its default, which is how the rule
// registry itself decides how to parse an override.
func kindOf(def any) (config.Kind, string) {
	switch d := def.(type) {
	case int:
		return config.KindInt, strconv.Itoa(d)
	case bool:
		return config.KindBool, strconv.FormatBool(d)
	case []string:
		return config.KindList, strings.Join(d, ",")
	case string:
		return config.KindString, d
	}
	return config.KindString, fmt.Sprint(def)
}

// ---- list ------------------------------------------------------------

func configList(args []string) error {
	var pattern string
	changedOnly := false
	for _, a := range args {
		switch a {
		case "--changed", "-c":
			changedOnly = true
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("`argus config list` takes a pattern and --changed, not %q", a)
			}
			pattern = strings.ToUpper(a)
		}
	}

	path := config.ActiveFile()
	file, err := config.ReadFileValues(path)
	if err != nil {
		return err
	}
	all := settings()

	dim, off := dimming(os.Stdout)
	fmt.Println()
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("  Settings, and where each value comes from.\n  File: %s\n", path)
	} else {
		fmt.Printf("  Settings, and where each value comes from.\n"+
			"  No configuration file yet - `argus init` writes one at\n  %s\n", path)
	}

	// One width for the whole list, so the columns line up across the
	// section headings rather than restarting under each of them.
	width := 0
	var shown []config.Resolution
	for _, s := range all {
		if pattern != "" && !strings.Contains(strings.ToUpper(s.Key), pattern) {
			continue
		}
		r := s.Resolve(file)
		if changedOnly && r.Origin == config.FromDefault {
			continue
		}
		if len(s.Key) > width {
			width = len(s.Key)
		}
		shown = append(shown, r)
	}
	if len(shown) == 0 {
		fmt.Printf("\n  Nothing matches %q. `argus config list` shows the lot.\n\n", pattern)
		return nil
	}

	changed := 0
	section := ""
	for _, r := range shown {
		if r.Setting.Section != section {
			section = r.Setting.Section
			fmt.Printf("\n%s\n", section)
		}
		value := r.Setting.Display(r.Value)
		if value == "" {
			value = dim + "(not set)" + off
		}
		if r.Origin != config.FromDefault {
			changed++
		}
		line := fmt.Sprintf("  %-*s  %-30s %s%-12s%s",
			width, r.Setting.Key, ellipsis(value, 30), dim, r.Origin, note(r))
		fmt.Println(strings.TrimRight(line, " ") + off)
	}

	fmt.Printf("\n  %d settings, %d of them changed from the default.\n", len(shown), changed)
	fmt.Printf("  %s`argus config get KEY` explains one; `argus config set KEY VALUE` changes it.%s\n\n",
		dim, off)
	return nil
}

// note is the trailing half-sentence that turns a table into an answer:
// what the default was, or which layer is quietly winning.
func note(r config.Resolution) string {
	switch {
	case r.Shadowed:
		return fmt.Sprintf("the file says %q, which is ignored",
			r.Setting.Display(r.InFile))
	case r.Origin == config.FromDefault:
		return ""
	case r.Setting.Default == "":
		return "no default"
	default:
		return "default " + r.Setting.Default
	}
}

// ---- get -------------------------------------------------------------

func configGet(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: argus config get KEY")
	}
	s, err := lookup(args[0])
	if err != nil {
		return err
	}
	file, err := config.ReadFileValues(config.ActiveFile())
	if err != nil {
		return err
	}
	r := s.Resolve(file)

	dim, off := dimming(os.Stdout)
	fmt.Printf("\n  %s\n", s.Key)
	if s.Desc != "" {
		fmt.Printf("  %s%s%s\n", dim, s.Desc, off)
	}
	fmt.Println()

	value := s.Display(r.Value)
	if value == "" {
		value = "(not set, and it has no default)"
	}
	fmt.Printf("    %-10s %s\n", "value", value)

	switch r.Origin {
	case config.FromEnv:
		fmt.Printf("    %-10s the environment\n", "from")
	case config.FromFile:
		fmt.Printf("    %-10s %s\n", "from", config.ActiveFile())
	default:
		fmt.Printf("    %-10s the built-in default\n", "from")
	}
	if r.Shadowed {
		fmt.Printf("    %-10s %s, which is ignored while the environment sets this\n",
			"in file", s.Display(r.InFile))
	}
	if s.Default != "" {
		fmt.Printf("    %-10s %s\n", "default", s.Default)
	}
	kind := string(s.Kind)
	if s.Kind == config.KindEnum {
		kind = strings.Join(s.Values, ", ")
	}
	fmt.Printf("    %-10s %s\n", "type", kind)
	if s.Secret {
		fmt.Printf("    %-10s a credential, so it is shown masked\n", "note")
	}
	if s.EnvOnly {
		fmt.Printf("    %-10s read before the configuration file is found, so it only works\n"+
			"    %-10s as an environment variable\n", "note", "")
	}
	fmt.Println()
	return nil
}

// ---- set -------------------------------------------------------------

func configSet(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: argus config set KEY VALUE")
	}
	s, err := lookup(args[0])
	if err != nil {
		return err
	}
	if s.EnvOnly {
		return fmt.Errorf(
			"%s says where configuration is read from, so it cannot be set inside it. "+
				"Export it in your environment instead", s.Key)
	}

	var value string
	if len(args) > 1 {
		// Joined rather than insisted upon in quotes: `argus config set
		// ARGUS_JIRA_POINTS_FIELD_NAME Story Points` is what gets typed.
		value = strings.Join(args[1:], " ")
		if s.Secret {
			fmt.Fprintf(os.Stderr,
				"\n  ! A value typed here is in your shell history. For a credential, pipe it in\n"+
					"    instead:  something-that-prints-it | argus config set %s\n", s.Key)
		}
	} else {
		if value, err = readValue(s); err != nil {
			return err
		}
	}

	value = strings.TrimSpace(value)
	if err := s.Validate(value); err != nil {
		return err
	}

	path := config.ActiveFile()
	ch, err := config.SetInFile(path, s, value)
	if err != nil {
		return err
	}

	fmt.Printf("\n  %s=%s\n\n", s.Key, s.Display(value))
	if ch.Created {
		fmt.Printf("  Created %s\n", path)
	}
	switch {
	case ch.Replaced && ch.Previous == value:
		fmt.Printf("  %s was already that. Line %d is unchanged.\n", s.Key, ch.Line)
	case ch.Replaced:
		fmt.Printf("  Replaced line %d of %s, which said %s.\n",
			ch.Line, path, s.Display(ch.Previous))
	case ch.Uncommented:
		fmt.Printf("  Uncommented line %d of %s, so the notes above it still apply.\n", ch.Line, path)
	default:
		fmt.Printf("  Added to %s.\n", path)
	}
	if ch.Duplicates == 1 {
		fmt.Printf("  A second line also set %s and was never read; it is commented out now.\n", s.Key)
	} else if ch.Duplicates > 1 {
		fmt.Printf("  %d further lines also set %s and were never read; they are commented out now.\n",
			ch.Duplicates, s.Key)
	}

	// The change appearing to do nothing is the failure this warning
	// exists to prevent: the file is the weaker layer, and someone who
	// exported the variable a fortnight ago has long since forgotten.
	if v := strings.TrimSpace(os.Getenv(s.Key)); v != "" && v != value {
		fmt.Printf("\n  ! %s is also set in your environment, to %s.\n"+
			"    That wins, so this will not take effect until you unset it there.\n",
			s.Key, s.Display(v))
	}
	if s.Secret {
		fmt.Printf("\n  %s now holds a credential. It is written with owner-only permissions,\n"+
			"    but the environment is the safer home for one.\n", filepath.Base(path))
	}
	fmt.Printf("\n  Argus reads its configuration at startup, so restart it if it is running.\n\n")
	return nil
}

// readValue takes the value from standard input.
//
// This is how a credential gets in without passing through the shell's
// history, and it is offered for everything rather than only for secrets
// so that there is one rule to remember.
func readValue(s config.Setting) (string, error) {
	if interactive() {
		fmt.Printf("\n  Reading %s from standard input. Type or paste it and press enter.\n", s.Key)
		if s.Secret {
			fmt.Printf("  It will be visible as you type; pipe it in instead to avoid that.\n")
		}
		fmt.Print("\n  > ")
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("no value given. Either `argus config set %s VALUE`, "+
			"or pipe the value in", s.Key)
	}
	return v, nil
}

// ---- unset -----------------------------------------------------------

func configUnset(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: argus config unset KEY")
	}
	s, err := lookup(args[0])
	if err != nil {
		return err
	}
	path := config.ActiveFile()
	ch, err := config.UnsetInFile(path, s)
	if err != nil {
		return err
	}

	fmt.Println()
	if !ch.Had {
		fmt.Printf("  %s was not set in %s, so there was nothing to remove.\n\n", s.Key, path)
		return nil
	}
	back := s.Default
	if back == "" {
		back = "unset, which is its default"
	}
	fmt.Printf("  %s is back to %s.\n", s.Key, back)
	fmt.Printf("  Line %d of %s is commented out rather than deleted, so you can still\n"+
		"  see what it said: %s\n", ch.Line, path, s.Display(ch.Previous))
	if ch.Duplicates > 0 {
		fmt.Printf("  %d other line(s) setting it were commented out too.\n", ch.Duplicates)
	}
	if v := strings.TrimSpace(os.Getenv(s.Key)); v != "" {
		fmt.Printf("\n  ! %s is still set in your environment, to %s, and that is what\n"+
			"    Argus will use.\n", s.Key, s.Display(v))
	}
	fmt.Println()
	return nil
}

// ---- edit ------------------------------------------------------------

// configEdit opens the file in whatever editor the person has chosen.
//
// $VISUAL before $EDITOR, which is the long-standing convention: EDITOR
// may well be a line editor for a dumb terminal, and VISUAL is the one
// you want in front of you. Neither set, the fallbacks are tried in turn
// and, if none exists, nothing is opened - launching a stranger's editor
// at them is not a helpful surprise.
func configEdit() error {
	path := config.ActiveFile()
	created := false
	if _, err := os.Stat(path); err != nil {
		if err := config.WriteStarter(path); err != nil {
			return err
		}
		created = true
	}

	name, args := editor()
	if name == "" {
		fmt.Printf("\n  No editor found. Set $EDITOR, or open this yourself:\n\n      %s\n\n", path)
		return nil
	}
	if created {
		fmt.Printf("\n  Created %s\n", path)
	}

	cmd := exec.Command(name, append(args, path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	fmt.Printf("\n  Edited %s\n"+
		"  `argus config list` will show what Argus now reads, and `argus doctor`\n"+
		"  whether it is enough to start.\n\n", path)
	return nil
}

func editor() (string, []string) {
	for _, v := range []string{os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if fields := strings.Fields(v); len(fields) > 0 {
			return fields[0], fields[1:]
		}
	}
	fallbacks := []string{"editor", "nano", "vi"}
	if runtime.GOOS == "windows" {
		fallbacks = []string{"notepad.exe"}
	}
	for _, name := range fallbacks {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", nil
}

// ---- shared ----------------------------------------------------------

// lookup finds a setting, or explains what was probably meant.
//
// Rejecting the key is the whole job: ARGUS_GITHB_ORG written to the file
// is a configuration that looks correct, reads correct and does nothing,
// and there is no later moment at which anyone finds out.
func lookup(key string) (config.Setting, error) {
	all := settings()
	if s, ok := all.Find(key); ok {
		return s, nil
	}
	near := all.Suggest(key)
	switch len(near) {
	case 0:
		return config.Setting{}, fmt.Errorf(
			"there is no setting called %s. `argus config list` shows every one there is", key)
	case 1:
		return config.Setting{}, fmt.Errorf("there is no setting called %s. Did you mean %s?",
			key, near[0])
	default:
		return config.Setting{}, fmt.Errorf("there is no setting called %s. Did you mean %s or %s?",
			key, strings.Join(near[:len(near)-1], ", "), near[len(near)-1])
	}
}

// dimming returns the escape codes for secondary text, and nothing at all
// when the output is not a terminal, so that piping this anywhere leaves
// plain text behind.
func dimming(f *os.File) (string, string) {
	if os.Getenv("NO_COLOR") != "" {
		return "", ""
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return "", ""
	}
	return "\033[2m", "\033[0m"
}

func ellipsis(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func configUsage(w io.Writer) {
	fmt.Fprintf(w, `argus config - see and change any setting, without opening a file.

  argus config                 every setting, its value, and where that value came from
  argus config list PATTERN    the same, narrowed to keys containing PATTERN
  argus config list --changed  only the settings that are not at their default
  argus config get KEY         one setting in full: value, origin, default and type
  argus config set KEY VALUE   write it to the configuration file
  argus config unset KEY       comment it out, returning it to its default
  argus config edit            open the file in $VISUAL, or $EDITOR
  argus config path            print the path to the file, and nothing else

A value is looked for in the environment first, then the configuration
file, then the built-in default. An environment variable therefore beats
anything written to the file - which is why every command here says where
the value in force came from, and why `+"`set`"+` warns when what it just wrote
is being overridden.

Examples:

  argus config set ARGUS_GITHUB_ORG your-org    the one required setting
  argus config list jira                        everything the sprint report reads
  argus config get ARGUS_PORT                   why is it not the port I set?
  argus config list --changed                   what have I changed from stock?

A credential typed on the command line is in your shell history, so pipe
it in instead and no value is given on the line:

  read-it-from-somewhere | argus config set ARGUS_JIRA_TOKEN

Keys are checked against the settings Argus actually reads, rules
included, so a misspelling is refused rather than written. Comments and
layout in the file are left alone: a setting already there is changed
where it stands, and one that is present but commented out is uncommented
rather than added a second time.

The configuration file is
    %s

Docs: %s
`, config.ActiveFile(), term.Link(w, "https://github.com/Tzomily-Anvar/argus"))
}
