package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// What Jira declared, kept beside the configuration.
//
// A value read from Jira is configuration too - somebody set it there on
// purpose - and a value that cannot be printed with its source is a magic
// number. So the sprint service records each read here, and `argus
// config list` answers "why does it think this" with the read it came
// from and how old that reading is.
//
// The registry is written to a small file in the data directory, for two
// reasons. `argus config list` runs in its own process and would
// otherwise have nothing to show. And when a read fails, the last value
// read is what a report is built on - dated and marked stale, never a
// built-in default, because a default that looks detected cannot be
// told from a real one.

// Declaration is one value Jira declared, and when.
type Declaration struct {
	Key   string `json:"key"`
	Value string `json:"value"`

	// Source names the read: "status categories", "board estimation".
	Source string `json:"source"`

	// At is when the read last succeeded.
	At time.Time `json:"at"`

	// Stale reports that the most recent attempt failed, so Value is the
	// previous reading rather than the current state of Jira.
	Stale bool `json:"stale,omitempty"`
}

var registry struct {
	sync.Mutex
	path   string
	loaded bool
	items  map[string]Declaration
}

// DeclaredFile is where the registry is kept, in the data directory
// rather than beside config.env: the configuration directory may be a
// checkout's .env, and this is written by the server, not by a person.
func DeclaredFile() string { return filepath.Join(DataDir(), "declared.json") }

// UseDeclaredFile points the registry at a file. `argus config` calls it
// because it does not Load the configuration, so the data directory the
// file names is not in its environment.
func UseDeclaredFile(path string) {
	registry.Lock()
	defer registry.Unlock()
	registry.path = path
	registry.loaded = false
	registry.items = nil
}

// load reads the file once. Called with the lock held.
func load() {
	if registry.loaded {
		return
	}
	registry.loaded = true
	registry.items = map[string]Declaration{}
	if registry.path == "" {
		registry.path = DeclaredFile()
	}
	b, err := os.ReadFile(registry.path)
	if err != nil {
		return
	}
	var items []Declaration
	if json.Unmarshal(b, &items) != nil {
		return
	}
	for _, d := range items {
		registry.items[d.Key] = d
	}
}

// save writes the file whole. Called with the lock held. A failure is
// not returned: the value is in memory and in force either way, and the
// only thing lost is what a later `argus config list` can show.
func save() {
	items := make([]Declaration, 0, len(registry.items))
	for _, d := range registry.items {
		items = append(items, d)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(registry.path)
	if os.MkdirAll(dir, 0o750) != nil {
		return
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".declared.%d.json", os.Getpid()))
	if os.WriteFile(tmp, b, 0o600) != nil {
		return
	}
	if os.Rename(tmp, registry.path) != nil {
		_ = os.Remove(tmp)
	}
}

// Declared records that Jira declared a setting's value, read from
// source at the given time.
func Declared(key, value, source string, at time.Time) {
	registry.Lock()
	defer registry.Unlock()
	load()
	registry.items[key] = Declaration{Key: key, Value: value, Source: source, At: at.UTC()}
	save()
}

// DeclaredStale marks a declared value as one the last read failed to
// refresh. The value and its date are kept: they are what is in force.
func DeclaredStale(key string) {
	registry.Lock()
	defer registry.Unlock()
	load()
	d, ok := registry.items[key]
	if !ok {
		return
	}
	d.Stale = true
	registry.items[key] = d
	save()
}

// Undeclare withdraws a key from the registry, for a value that is no
// longer in force from Jira - a board that changed its field and was
// held back, an override that now names the statuses itself.
func Undeclare(key string) {
	registry.Lock()
	defer registry.Unlock()
	load()
	if _, ok := registry.items[key]; !ok {
		return
	}
	delete(registry.items, key)
	save()
}

// Declaration returns what Jira declared for a key, if anything.
func (s Setting) Declaration() (Declaration, bool) {
	registry.Lock()
	defer registry.Unlock()
	load()
	d, ok := registry.items[s.Key]
	return d, ok
}

// ResolveInForce is Resolve for a process that has already run Load.
//
// After Load the file's values are in the environment, so Resolve alone
// would report every one of them as coming from the environment. The
// file is read again here to tell the two apart: a value the file holds
// and the environment agrees with is the file's.
func (s Setting) ResolveInForce() Resolution {
	file := map[string]string{}
	if Loaded() != "" {
		if values, err := ReadFileValues(Loaded()); err == nil {
			file = values
		}
	}
	r := s.Resolve(file)
	if r.Origin == FromEnv && r.InFile == r.InEnv {
		r.Origin = FromFile
	}
	return r
}

// FormerNames reads a setting under any name it used to have, when the
// current name is unset, and says so.
//
// Rename a setting without this and every configuration file silently
// reverts that value to its default - which for the delivered statuses
// means a team's whole terminal list quietly becoming one status. The
// notice names both keys so the file can be brought up to date.
func FormerNames(cat Catalogue) []string {
	var notices []string
	for _, s := range cat {
		if len(s.Was) == 0 || String(s.Key, "") != "" {
			continue
		}
		for _, old := range s.Was {
			v := String(old, "")
			if v == "" {
				continue
			}
			_ = os.Setenv(s.Key, v)
			notices = append(notices, fmt.Sprintf("%s is now called %s; the old name still works, but rename it in your configuration", old, s.Key))
			break
		}
	}
	return notices
}
