package forj

import (
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/goforj/console"
	"github.com/goforj/goforj/internal/stacks"
)

// StackCmd offers reversible resource configuration without requiring command arguments.
type StackCmd struct {
	root string
	ui   *console.Console
}

// Signature registers Stacks as a project authoring command.
func (*StackCmd) Signature() string {
	return `name:"stack" help:"Configure resources, save stacks, and switch the project environment"`
}

// Help explains the boundary between configuration changes and durable application data.
func (*StackCmd) Help() string {
	return "Interactively change .env, create reusable .env.stack.<name> definitions, or switch between saved stacks.\nPrivate connection settings and recovery snapshots stay in ignored .local files.\nActivation edits .env. A running dev watcher can rebuild and run configured startup tasks. Stop it before preparing a database transition. Run forj build after changing drivers."
}

// Run keeps every write behind a preview and explicit confirmation.
func (c *StackCmd) Run() error {
	root := c.root
	if root == "" {
		root = "."
	}
	ui := c.ui
	if ui == nil {
		ui = console.Default()
	}
	session, err := stacks.Open(root)
	if err != nil {
		return err
	}
	names, err := session.Names()
	if err != nil {
		return err
	}
	ui.Infof("Project stacks")
	if session.Active == "" {
		ui.Infof("Current configuration: .env (unsaved)")
	} else {
		ui.Infof("Active stack: %s", session.Active)
	}
	for _, name := range names {
		ui.Infof("  %s", name)
	}
	showStackDrivers(ui, session, session.Current)
	actions := []string{"Change current configuration", "Switch to a saved stack", "Save current configuration as a stack", "Create a stack", "Build a saved stack", "Restore previous configuration", "Cancel"}
	action, err := ui.ChooseIndex("What would you like to do?", actions, 0)
	if err != nil {
		return err
	}
	switch action {
	case 0:
		values, err := editStack(ui, session, session.Current)
		if err != nil {
			return err
		}
		return activateStack(ui, session, "", values)
	case 1:
		name, err := chooseStack(ui, names)
		if err != nil {
			return err
		}
		if name == "" {
			return nil
		}
		values, err := session.Load(name, true)
		if err != nil {
			return err
		}
		return activateStack(ui, session, name, values)
	case 2:
		return saveStack(ui, session, session.Current, false)
	case 3:
		bases := append([]string{"Portable defaults", "Current configuration"}, names...)
		index, err := ui.ChooseIndex("Start the stack from", bases, 0)
		if err != nil {
			return err
		}
		values := session.Current
		if index == 0 {
			values, err = session.Portable()
		} else if index >= 2 {
			values, err = session.Load(names[index-2], true)
		}
		if err != nil {
			return err
		}
		values, err = editStackDrivers(ui, session, values)
		if err != nil {
			return err
		}
		return saveStack(ui, session, values, true)
	case 4:
		name, err := chooseStack(ui, names)
		if err != nil {
			return err
		}
		if name == "" {
			return nil
		}
		values, err := stacks.BuildDefaults(root, name)
		if err != nil {
			return err
		}
		showStackDrivers(ui, session, values)
		confirmed, err := ui.Confirm("Build this stack's shareable defaults into the binary?", false)
		if err != nil || !confirmed {
			return err
		}
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		cmd := exec.Command(binary, "build", "--stack", name)
		cmd.Dir = root
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	case 5:
		if !session.HasPrevious() {
			ui.Infof("No previous configuration has been saved")
			return nil
		}
		return activateStack(ui, session, "", session.Previous())
	default:
		return nil
	}
}

// chooseStack leaves an empty inventory as a harmless wizard outcome.
func chooseStack(ui *console.Console, names []string) (string, error) {
	if len(names) == 0 {
		ui.Infof("No saved stacks yet. Create one or save the current configuration.")
		return "", nil
	}
	choices := append(slices.Clone(names), "Cancel")
	index, err := ui.ChooseIndex("Choose a stack", choices, 0)
	if err != nil {
		return "", err
	}
	if index == len(names) {
		return "", nil
	}
	return names[index], nil
}

// showStackDrivers reveals provider choices without printing private endpoints or credentials.
func showStackDrivers(ui *console.Console, s *stacks.Session, values map[string]string) {
	for _, resource := range s.Resources {
		driver := values[resource.Key]
		if driver == "" {
			driver = "(inherited/default)"
		}
		ui.Infof("  %s: %s", resource.Key, driver)
	}
	ui.Infof("  COMPOSE_PROFILES: %s", values["COMPOSE_PROFILES"])
}

// editStack offers the portable preset without requiring the owner to name a stack.
func editStack(ui *console.Console, s *stacks.Session, current map[string]string) (map[string]string, error) {
	index, err := ui.ChooseIndex("Start from", []string{"Portable defaults", "Current configuration"}, 0)
	if err != nil {
		return nil, err
	}
	values := current
	if index == 0 {
		values, err = s.Portable()
		if err != nil {
			return nil, err
		}
	}
	return editStackDrivers(ui, s, values)
}

// editStackDrivers lets owners select providers and enter connection settings without echoing secrets.
func editStackDrivers(ui *console.Console, s *stacks.Session, values map[string]string) (map[string]string, error) {
	values = copyStackValues(values)
	for {
		showStackDrivers(ui, s, values)
		labels := []string{"Done"}
		for _, resource := range s.Resources {
			labels = append(labels, resource.Key)
		}
		labels = append(labels, "Compose profiles", "Connection or resource setting")
		index, err := ui.ChooseIndex("Edit stack", labels, 0)
		if err != nil {
			return nil, err
		}
		if index == 0 {
			return values, s.Validate(values)
		}
		if index == len(s.Resources)+1 {
			profiles, err := ui.Ask("Compose profiles (comma-separated; blank disables dependencies)")
			if err != nil {
				return nil, err
			}
			values["COMPOSE_PROFILES"] = profiles
			continue
		}
		if index == len(s.Resources)+2 {
			key, err := ui.Ask("Environment key (blank cancels)")
			if err != nil {
				return nil, err
			}
			if key == "" {
				continue
			}
			value, err := ui.AskSecret("Value for " + key + " (stored privately)")
			if err != nil {
				return nil, err
			}
			candidate := copyStackValues(values)
			candidate[key] = value
			if err := s.Validate(candidate); err != nil {
				ui.Errorf("%v", err)
				continue
			}
			values = candidate
			continue
		}
		resource := s.Resources[index-1]
		drivers := resource.Definition.Drivers
		choices := make([]string, len(drivers))
		selected := 0
		for i, driver := range drivers {
			label := driver.Name
			if driver.Name == resource.Definition.DefaultDriver {
				label += " (portable default)"
			} else if driver.Service != "" {
				label += " (external service)"
			}
			choices[i] = label
			if driver.Name == values[resource.Key] {
				selected = i
			}
		}
		choice, err := ui.ChooseIndex("Driver for "+resource.Key, choices, selected)
		if err != nil {
			return nil, err
		}
		if err := stacks.SetDriver(values, resource, drivers[choice].Name); err != nil {
			return nil, err
		}
		if drivers[choice].Service != "" {
			ui.Infof("Configure this driver's endpoint using Connection or resource setting, and enable its Compose profile if using a local container.")
		}
	}
}

// saveStack confirms the definition path and keeps optional activation a separate reviewed action.
func saveStack(ui *console.Console, s *stacks.Session, values map[string]string, offerActivate bool) error {
	name, err := ui.Ask("Stack name (blank cancels)")
	if err != nil || name == "" {
		return err
	}
	if err := stacks.ValidateName(name); err != nil {
		return err
	}
	if err := s.PrepareSave(name); err != nil {
		return err
	}
	// Loading an existing definition validates it before asking to replace it.
	names, err := s.Names()
	if err != nil {
		return err
	}
	if slices.Contains(names, name) {
		if _, err := s.Load(name, true); err != nil {
			return err
		}
	}
	showStackDrivers(ui, s, values)
	prompt := "Save " + stacks.DefinitionPath(name) + " and its private connection settings?"
	if slices.Contains(names, name) {
		prompt = "Replace existing stack " + name + " and its private connection settings?"
	}
	confirmed, err := ui.Confirm(prompt, false)
	if err != nil || !confirmed {
		return err
	}
	if err := s.Save(name, values); err != nil {
		return err
	}
	ui.Successf("Saved stack %s", name)
	if !offerActivate {
		return nil
	}
	fresh, err := stacks.Open(s.Root)
	if err != nil {
		return err
	}
	return activateStack(ui, fresh, name, values)
}

// activateStack previews provider changes and preserves edited working copies only with an explicit choice.
func activateStack(ui *console.Console, s *stacks.Session, name string, values map[string]string) error {
	if err := s.Validate(values); err != nil {
		return err
	}
	overrides, err := s.Overrides()
	if err != nil {
		return err
	}
	for _, layer := range overrides {
		ui.Infof("Other runtime layer contains resource settings (%s). Its existing precedence still applies.", layer)
	}
	ui.Infof("Changes to .env")
	all := copyStackValues(s.Current)
	for key, value := range values {
		all[key] = value
	}
	keys := make([]string, 0, len(all))
	for key := range all {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		before, had := s.Current[key]
		after, has := values[key]
		if had == has && before == after {
			continue
		}
		if strings.HasSuffix(key, "_DRIVER") || strings.HasSuffix(key, "_SUPPORTED_DRIVERS") || key == "COMPOSE_PROFILES" {
			if !had {
				before = "(unset)"
			}
			if !has {
				after = "(unset)"
			}
			ui.Infof("  %s: %s -> %s", key, before, after)
		} else {
			ui.Infof("  %s: private setting changes", key)
		}
	}
	ui.Infof("Database contents stay in place. Check target-dialect migrations before starting the App; activation does not translate SQL or transfer data.")
	ui.Infof("Memory and inproc providers are process-local. The wizard does not stop existing containers.")
	ui.Infof("A running forj dev session can react to .env changes and run configured startup tasks. Stop it before preparing a database transition.")
	saveWorking := s.Active != ""
	if s.Changed() {
		choice, err := ui.ChooseIndex("The active stack has manual edits", []string{"Keep edits in its private working copy", "Discard edits when leaving this stack", "Cancel"}, 0)
		if err != nil {
			return err
		}
		if choice == 2 {
			return nil
		}
		saveWorking = choice == 0
	}
	confirmed, err := ui.Confirm("Apply these settings to .env and save a recovery snapshot?", false)
	if err != nil || !confirmed {
		return err
	}
	if err := s.Activate(name, values, saveWorking); err != nil {
		return err
	}
	ui.Successf("Updated .env")
	ui.Infof("Run forj build to regenerate driver support, then restart the App. Use forj down if you also want to stop existing containers.")
	return nil
}

// copyStackValues keeps wizard edits separate from the original recovery snapshot.
func copyStackValues(values map[string]string) map[string]string {
	copy := map[string]string{}
	for key, value := range values {
		copy[key] = value
	}
	return copy
}
