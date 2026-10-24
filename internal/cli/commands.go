package cli

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"lman/internal/domain"
)

const dateLayout = "2006-01-02"

func (c *CLI) known(args []string) error {
	if len(args) != 0 {
		return usagef("known takes no arguments")
	}
	fmt.Fprintln(c.out, "Languages Lman can detect:")
	fmt.Fprintln(c.out)
	w := c.table()
	fmt.Fprintln(w, "  LANGUAGE\tID\tALSO")
	for _, d := range domain.Catalog() {
		fmt.Fprintf(w, "  %s\t%s\t%s\n", d.Name, d.ID, strings.Join(d.Aliases, ", "))
	}
	return w.Flush()
}

func (c *CLI) list(ctx context.Context, m Manager) error {
	langs, err := m.Languages(ctx)
	if err != nil {
		return err
	}
	if len(langs) == 0 {
		fmt.Fprintf(c.out, "No programming languages found on %s.\n", m.Host())
		return nil
	}
	fmt.Fprintf(c.out, "Languages on %s:\n\n", m.Host())
	w := c.table()
	fmt.Fprintln(w, "  LANGUAGE\tID\tACTIVE\tVERSIONS")
	for _, l := range langs {
		active := "-"
		if v, ok := l.Active(); ok {
			active = l.Label(v.Number)
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%d\n", l.Name, l.ID, active, len(l.Versions))
	}
	return w.Flush()
}

func (c *CLI) show(ctx context.Context, m Manager, id string) error {
	l, err := m.Language(ctx, id)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "%s on %s\n\nVersions (* runs in the terminal):\n", l.Name, m.Host())
	w := c.table()
	for _, v := range l.Versions {
		mark := " "
		if v.Active {
			mark = "*"
		}
		location := v.Path
		if v.RealPath != v.Path {
			location += " -> " + v.RealPath
		}
		fmt.Fprintf(w, "  %s %s\t%s\t%s\t%s\n", mark, l.Label(v.Number), v.Method, date(v), location)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprint(c.out, "\nTools:\n")
	w = c.table()
	for _, h := range l.Hints {
		fmt.Fprintf(w, "  %s\t%s\n", h.Tool, h.Purpose)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "\nMore: lman env %[1]s, lman manual %[1]s\n", l.ID)
	return nil
}

func (c *CLI) manual(ctx context.Context, m Manager, id string) error {
	manual, err := m.Manual(ctx, id)
	if err != nil {
		return err
	}
	fmt.Fprintln(c.out, strings.TrimRight(manual.Text, "\n"))
	fmt.Fprintf(c.out, "\nDocumentation: %s\n", manual.DocsURL)
	return nil
}

func (c *CLI) env(ctx context.Context, m Manager, id string) error {
	vars, err := m.Env(ctx, id)
	if err != nil {
		return err
	}
	w := c.table()
	fmt.Fprintln(w, "  NAME\tVALUE\tSOURCE")
	for _, v := range vars {
		value, source := v.Value, "terminal"
		if v.Default {
			source = "go env"
		}
		if value == "" {
			value, source = "-", "-"
		}
		if v.Managed {
			source = "lman"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\n", v.Name, value, source)
	}
	return w.Flush()
}

func (c *CLI) setEnv(ctx context.Context, m Manager, id, name, value string) error {
	if err := m.SetEnv(ctx, id, name, value); err != nil {
		return err
	}
	if value == "" {
		fmt.Fprintf(c.out, "%s is no longer set by lman. Open a new terminal to apply.\n", name)
		return nil
	}
	fmt.Fprintf(c.out, "%s=%s is set for new terminals.\n", name, value)
	return nil
}

func (c *CLI) use(ctx context.Context, m Manager, id, version string) error {
	a, err := m.Use(ctx, id, version)
	if err != nil {
		return err
	}
	l, err := m.Language(ctx, id)
	if err != nil {
		return err
	}
	label := l.Label(a.Version.Number)
	if a.NotInPath {
		return fmt.Errorf("%s %s is linked, but new terminals do not load lman: add ~/.lman/bin to PATH in your shell profile",
			l.Name, label)
	}
	if a.ShadowedBy != "" {
		return fmt.Errorf("%s %s is linked, but %s comes first in PATH: move it after lman in your shell profile",
			l.Name, label, a.ShadowedBy)
	}
	fmt.Fprintf(c.out, "%s %s is active. Open a new terminal to use it.\n", l.Name, label)
	return nil
}

func (c *CLI) remove(ctx context.Context, m Manager, args []string) error {
	yes := false
	var rest []string
	for _, a := range args {
		if a == "-y" || a == "--yes" {
			yes = true
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) == 0 || len(rest) > 2 {
		return usagef("remove needs a language and optionally a version")
	}
	l, err := m.Language(ctx, rest[0])
	if err != nil {
		return err
	}

	if len(rest) == 1 {
		question := fmt.Sprintf("Remove %s with all %d versions?", l.Name, len(l.Versions))
		if !yes && !c.confirm(question) {
			fmt.Fprintln(c.out, "Cancelled.")
			return nil
		}
		if err := m.RemoveLanguage(ctx, rest[0]); err != nil {
			return err
		}
		fmt.Fprintf(c.out, "%s is removed.\n", l.Name)
		return nil
	}

	v, err := l.Find(rest[1])
	if err != nil {
		return err
	}
	if v.Active {
		// The service refuses to remove the active version; ask it before asking the user.
		return m.RemoveVersion(ctx, rest[0], v.RealPath)
	}
	question := fmt.Sprintf("Remove %s %s at %s?", l.Name, l.Label(v.Number), v.Path)
	if !yes && !c.confirm(question) {
		fmt.Fprintln(c.out, "Cancelled.")
		return nil
	}
	if err := m.RemoveVersion(ctx, rest[0], v.RealPath); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "%s %s is removed.\n", l.Name, l.Label(v.Number))
	return nil
}

func (c *CLI) confirm(question string) bool {
	fmt.Fprintf(c.out, "%s [y/N] ", question)
	answer, err := c.in.ReadString('\n')
	if err != nil && answer == "" {
		fmt.Fprintln(c.out)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}

func (c *CLI) showStatus(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return usagef("status takes no arguments")
	}
	s, err := c.status.Get(ctx)
	if err != nil {
		return err
	}
	w := c.table()
	fmt.Fprintf(w, "Time\t%s\n", s.Time.Format("2006-01-02 15:04"))
	fmt.Fprintf(w, "Lman\t%s\n", s.Version)
	fmt.Fprintf(w, "Host\t%s\n", s.System.Hostname)
	fmt.Fprintf(w, "System\t%s %s (%s)\n", s.System.OS, s.System.OSVersion, s.System.Arch)
	fmt.Fprintf(w, "CPU\t%s, %d cores\n", s.System.CPU, s.System.Cores)
	fmt.Fprintf(w, "Memory\t%.1f GiB\n", float64(s.System.MemoryBytes)/(1<<30))
	return w.Flush()
}

func (c *CLI) setTheme(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usagef("theme needs light, dark or system")
	}
	theme, err := c.settings.SetTheme(ctx, args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Theme: %s. The desktop application applies it on the next start.\n", theme)
	return nil
}

func (c *CLI) table() *tabwriter.Writer {
	return tabwriter.NewWriter(c.out, 0, 0, 3, ' ', 0)
}

func date(v domain.Version) string {
	if v.InstalledAt.IsZero() {
		return "-"
	}
	return v.InstalledAt.Local().Format(dateLayout)
}
