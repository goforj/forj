package stacks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/goforj/goforj/project"
)

const completePrivateHeader = "# GoForj complete private stack snapshot\n"

const stateName = ".env.stack-state.local"

var validName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

// state records private working copies separately from definitions that owners may commit.
type state struct {
	Version     int               `json:"version"`
	Active      string            `json:"active"`
	Previous    map[string]string `json:"previous,omitempty"`
	HasPrevious bool              `json:"has_previous"`
	Applied     map[string]string `json:"applied,omitempty"`
}

// Session retains the configuration reviewed by the wizard until its final confirmation.
type Session struct {
	Root      string
	Current   map[string]string
	Resources []Resource
	Active    string
	config    *project.Config
	env       document
	files     map[string]file
	state     state
}

// Open reads configuration without creating files or loading values into the process environment.
func Open(root string) (*Session, error) {
	config, err := project.LoadProjectConfigAt(root)
	if err != nil {
		return nil, err
	}
	s := &Session{Root: root, config: config, files: map[string]file{}}
	for _, name := range []string{".env", ".env.example", stateName, ".gitignore"} {
		f, err := readFile(root, name)
		if err != nil {
			return nil, err
		}
		s.files[name] = f
	}
	if !s.files[".env"].exists {
		return nil, fmt.Errorf(".env is missing; run forj env:init first")
	}
	s.env, err = parseDocument(s.files[".env"].before)
	if err != nil {
		return nil, fmt.Errorf("read .env: %w", err)
	}
	s.Current = managedValues(config, s.env.values)
	inventory := clone(s.env.values)
	if s.files[".env.example"].exists {
		example, err := parseDocument(s.files[".env.example"].before)
		if err != nil {
			return nil, fmt.Errorf("read .env.example: %w", err)
		}
		for key, value := range example.values {
			if _, ok := inventory[key]; !ok {
				inventory[key] = value
			}
		}
	}
	s.Resources = resources(root, config, inventory)
	if s.files[stateName].exists {
		if err := json.Unmarshal(s.files[stateName].before, &s.state); err != nil {
			return nil, fmt.Errorf("read private stack state: %w", err)
		}
		if s.state.Version != 1 {
			return nil, fmt.Errorf("unsupported stack state version %d", s.state.Version)
		}
	}
	if s.state.Active != "" {
		if err := ValidateName(s.state.Active); err != nil {
			return nil, fmt.Errorf("invalid active stack in private state: %w", err)
		}
	}
	s.Active = s.state.Active
	return s, nil
}

// ValidateName keeps profile names distinct from private overrides and filesystem paths.
func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("stack name must start with a lowercase letter and contain up to 48 lowercase letters, digits, underscores, or hyphens")
	}
	return nil
}

// Names lists shareable definitions independently of local recovery state.
func (s *Session) Names() ([]string, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, entry := range entries {
		name, ok := strings.CutPrefix(entry.Name(), ".env.stack.")
		if !ok || strings.HasSuffix(name, ".local") {
			continue
		}
		if err := ValidateName(name); err != nil {
			return nil, fmt.Errorf("invalid stack file %s: %w", entry.Name(), err)
		}
		result = append(result, name)
	}
	return result, nil
}

// read retains a file's original contents for the later write preflight.
func (s *Session) read(name string) (file, error) {
	if f, ok := s.files[name]; ok {
		return f, nil
	}
	f, err := readFile(s.Root, name)
	if err == nil {
		s.files[name] = f
	}
	return f, err
}

// Load resolves a shareable definition and its optional private working copy.
func (s *Session) Load(name string, private bool) (map[string]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	names := []string{".env.stack." + name}
	if private {
		names = append(names, names[0]+".local")
	}
	values := map[string]string{}
	for i, path := range names {
		f, err := s.read(path)
		if err != nil {
			return nil, err
		}
		if !f.exists {
			if i == 0 {
				return nil, fmt.Errorf("stack %s does not exist", name)
			}
			continue
		}
		d, err := parseDocument(f.before)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if i > 0 && bytes.HasPrefix(f.before, []byte(completePrivateHeader)) {
			values = map[string]string{}
		}
		for key, value := range d.values {
			values[key] = value
		}
	}
	if err := s.Validate(values); err != nil {
		return nil, err
	}
	return values, nil
}

// Changed reports manual edits to the managed settings of an active stack.
func (s *Session) Changed() bool { return s.Active != "" && !equalValues(s.Current, s.state.Applied) }

// HasPrevious reports whether an unnamed configuration can be restored.
func (s *Session) HasPrevious() bool { return s.state.HasPrevious }

// Previous returns a copy so editing a restoration plan cannot mutate recovery state.
func (s *Session) Previous() map[string]string { return clone(s.state.Previous) }

// Save writes a deliberate shareable definition and private connection settings after confirmation.
func (s *Session) Save(name string, values map[string]string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := s.Validate(values); err != nil {
		return err
	}
	public := shareable(values)
	private := clone(values)
	for key := range public {
		delete(private, key)
	}
	return s.write(map[string][]byte{".env.stack." + name: encodeDocument(public), ".env.stack." + name + ".local": encodeDocument(private)})
}

// Activate preserves the previous configuration and optionally saves the active stack's manual edits privately.
func (s *Session) Activate(name string, values map[string]string, saveWorking bool) error {
	if name != "" {
		if err := ValidateName(name); err != nil {
			return err
		}
	}
	if err := s.Validate(values); err != nil {
		return err
	}
	next := state{Version: 1, Active: name, Previous: clone(s.Current), HasPrevious: true, Applied: clone(values)}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	updates := map[string][]byte{stateName: append(data, '\n'), ".env": s.env.replace(func(key string) bool { return managedKey(s.config, key) }, values)}
	if saveWorking && s.Active != "" {
		// A complete private copy preserves explicit empty values without editing the committed definition.
		updates[".env.stack."+s.Active+".local"] = append([]byte(completePrivateHeader), encodeDocument(s.Current)...)
	}
	return s.write(updates)
}

// write protects private files in Git and commits the reviewed updates with rollback.
func (s *Session) write(updates map[string][]byte) error {
	lock, err := os.OpenFile(filepath.Join(s.Root, ".env.stack-lock.local"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("another stack operation may be running (.env.stack-lock.local): %w", err)
	}
	_ = lock.Close()
	defer os.Remove(filepath.Join(s.Root, ".env.stack-lock.local"))
	for name, original := range s.files {
		current, err := readFile(s.Root, name)
		if err != nil {
			return err
		}
		if current.exists != original.exists || current.mode != original.mode || !bytes.Equal(current.before, original.before) {
			return fmt.Errorf("%s changed while the wizard was open; run forj stack again", name)
		}
	}
	ignore := s.files[".gitignore"]
	rules := "\n# GoForj stack definitions and private working copies\n!.env.stack.*\n.env.stack.*.local\n.env.stack-state.local\n.env.stack-lock.local\n.env.stack-tmp-*.local\n"
	if !bytes.HasSuffix(ignore.before, []byte(rules)) {
		updates[".gitignore"] = append(bytes.Clone(ignore.before), []byte(rules)...)
	}
	var files []file
	for _, name := range keys(updates) {
		f, err := s.read(name)
		if err != nil {
			return err
		}
		if strings.HasSuffix(name, ".local") {
			command := exec.Command("git", "ls-files", "--error-unmatch", "--", name)
			command.Dir = s.Root
			if err := command.Run(); err == nil {
				return fmt.Errorf("%s is tracked by Git; remove it from the index before storing private stack settings", name)
			}
		}
		f.after = updates[name]
		files = append(files, f)
	}
	return commit(s.Root, files, os.Rename)
}

// equalValues distinguishes missing settings from explicitly empty values.
func equalValues(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		other, ok := right[key]
		if !ok || other != value {
			return false
		}
	}
	return true
}

// BuildDefaults reads only the shareable definition and rejects private values before embedding.
func BuildDefaults(root, name string) (map[string]string, error) {
	config, err := project.LoadProjectConfigAt(root)
	if err != nil {
		return nil, err
	}
	s := &Session{Root: root, config: config, files: map[string]file{}}
	values, err := s.Load(name, false)
	if err != nil {
		return nil, err
	}
	if !equalValues(values, shareable(values)) {
		return nil, fmt.Errorf("stack %s contains connection settings; keep them in .env.stack.%s.local before building", name, name)
	}
	return values, nil
}

// DefinitionPath returns the project-relative filename shown in confirmations.
func DefinitionPath(name string) string { return filepath.Clean(".env.stack." + name) }

// AppDefaults folds a selected App's defaults into base keys before embedding, preserving runtime override precedence.
func AppDefaults(root string, values map[string]string, app string) (map[string]string, error) {
	config, err := project.LoadProjectConfigAt(root)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for key, value := range values {
		if resourceKey(config, key) == key {
			result[key] = value
		}
	}
	if app != "" && app != project.DefaultAppName {
		prefix := project.AppEnvironmentPrefix(app) + "_"
		for key, value := range values {
			if strings.HasPrefix(key, prefix) {
				result[strings.TrimPrefix(key, prefix)] = value
			}
		}
	}
	return result, nil
}

// Overrides lists other runtime layers containing resource settings without exposing their values.
func (s *Session) Overrides() ([]string, error) {
	var result []string
	for _, name := range []string{".env.local", ".env.staging", ".env.production", ".env.host"} {
		f, err := readFile(s.Root, name)
		if err != nil {
			return nil, err
		}
		if !f.exists {
			continue
		}
		d, err := parseDocument(f.before)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		managed := managedValues(s.config, d.values)
		if len(managed) > 0 {
			result = append(result, name+": "+strings.Join(keys(managed), ", "))
		}
	}
	var inherited []string
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && managedKey(s.config, key) {
			inherited = append(inherited, key)
		}
	}
	if len(inherited) > 0 {
		sort.Strings(inherited)
		result = append(result, "process environment: "+strings.Join(inherited, ", "))
	}
	return result, nil
}

// PrepareSave captures both destination files before a replacement confirmation is displayed.
func (s *Session) PrepareSave(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	for _, path := range []string{DefinitionPath(name), DefinitionPath(name) + ".local"} {
		if _, err := s.read(path); err != nil {
			return err
		}
	}
	return nil
}
