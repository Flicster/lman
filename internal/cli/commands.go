package cli

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/Flicster/lman/internal/domain"
)

const dateLayout = "2006-01-02"

func (c *CLI) known(args []string) error {
	if len(args) != 0 {
		return usagef("known takes no arguments")
	}
	var p writes
	p.fprintln(c.out, "Languages Lman can detect:")
	p.fprintln(c.out)
	w := c.table()
	p.fprintln(w, "  LANGUAGE\tID\tALSO")
	for _, d := range domain.Catalog() {
		p.fprintf(w, "  %s\t%s\t%s\n", d.Name, d.ID, strings.Join(d.Aliases, ", "))
	}
	if p.err != nil {
		return p.err
	}
	return w.Flush()
}

func (c *CLI) list(ctx context.Context, m Manager) error {
	langs, err := m.Languages(ctx)
	if err != nil {
		return err
	}
	var p writes
	if len(langs) == 0 {
		p.fprintf(c.out, "No programming languages found on %s.\n", m.Host())
		return p.err
	}
	p.fprintf(c.out, "Languages on %s:\n\n", m.Host())
	w := c.table()
	p.fprintln(w, "  LANGUAGE\tID\tACTIVE\tVERSIONS")
	for _, l := range langs {
		active := "-"
		if v, ok := l.Active(); ok {
			active = l.Label(v.Number)
		}
		p.fprintf(w, "  %s\t%s\t%s\t%d\n", l.Name, l.ID, active, len(l.Versions))
	}
	if p.err != nil {
		return p.err
	}
	return w.Flush()
}

func (c *CLI) show(ctx context.Context, m Manager, id string) error {
	l, err := m.Language(ctx, id)
	if err != nil {
		return err
	}
	var p writes
	p.fprintf(c.out, "%s on %s\n\nVersions (* runs in the terminal):\n", l.Name, m.Host())
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
		p.fprintf(w, "  %s %s\t%s\t%s\t%s\n", mark, l.Label(v.Number), v.Method, date(v), location)
	}
	if p.err != nil {
		return p.err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	p.fprint(c.out, "\nTools:\n")
	w = c.table()
	for _, h := range l.Hints {
		p.fprintf(w, "  %s\t%s\n", h.Tool, h.Purpose)
	}
	if p.err != nil {
		return p.err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	p.fprintf(c.out, "\nMore: lman env %[1]s, lman manual %[1]s\n", l.ID)
	return p.err
}

func (c *CLI) manual(ctx context.Context, m Manager, id string) error {
	manual, err := m.Manual(ctx, id)
	if err != nil {
		return err
	}
	var p writes
	p.fprintln(c.out, strings.TrimRight(manual.Text, "\n"))
	p.fprintf(c.out, "\nDocumentation: %s\n", manual.DocsURL)
	return p.err
}

func (c *CLI) env(ctx context.Context, m Manager, id string) error {
	vars, err := m.Env(ctx, id)
	if err != nil {
		return err
	}
	var p writes
	w := c.table()
	p.fprintln(w, "  NAME\tVALUE\tSOURCE")
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
		p.fprintf(w, "  %s\t%s\t%s\n", v.Name, value, source)
	}
	if p.err != nil {
		return p.err
	}
	return w.Flush()
}

func (c *CLI) setEnv(ctx context.Context, m Manager, id, name, value string) error {
	if err := m.SetEnv(ctx, id, name, value); err != nil {
		return err
	}
	var p writes
	if value == "" {
		p.fprintf(c.out, "%s is no longer set by lman. Open a new terminal to apply.\n", name)
		return p.err
	}
	p.fprintf(c.out, "%s=%s is set for new terminals.\n", name, value)
	return p.err
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
	var p writes
	p.fprintf(c.out, "%s %s is active. Open a new terminal to use it.\n", l.Name, label)
	return p.err
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
		plan, err := m.Plan(ctx, rest[0], "")
		if err != nil {
			return err
		}
		question := fmt.Sprintf("Remove %s with all %d versions?", l.Name, len(l.Versions))
		ok, err := c.confirmPlan(question, plan, yes)
		if err != nil {
			return err
		}
		if !ok {
			var p writes
			p.fprintln(c.out, "Canceled.")
			return p.err
		}
		if err := m.RemoveLanguage(ctx, rest[0]); err != nil {
			return err
		}
		var p writes
		p.fprintf(c.out, "%s is removed.\n", l.Name)
		return p.err
	}

	v, err := l.Find(rest[1])
	if err != nil {
		return err
	}
	if v.Active {
		// The service refuses to remove the active version; ask it before asking the user.
		return m.RemoveVersion(ctx, rest[0], v.RealPath)
	}
	plan, err := m.Plan(ctx, rest[0], v.RealPath)
	if err != nil {
		return err
	}
	question := fmt.Sprintf("Remove %s %s at %s?", l.Name, l.Label(v.Number), v.Path)
	ok, err := c.confirmPlan(question, plan, yes)
	if err != nil {
		return err
	}
	if !ok {
		var p writes
		p.fprintln(c.out, "Canceled.")
		return p.err
	}
	if err := m.RemoveVersion(ctx, rest[0], v.RealPath); err != nil {
		return err
	}
	var p writes
	p.fprintf(c.out, "%s %s is removed.\n", l.Name, l.Label(v.Number))
	return p.err
}

// confirmPlan prints what a removal does, then asks. With -y the plan is still printed,
// but nothing is asked.
func (c *CLI) confirmPlan(question string, plan []string, yes bool) (bool, error) {
	var p writes
	p.fprintln(c.out, "This will:")
	for _, line := range plan {
		p.fprintf(c.out, "  - %s\n", line)
	}
	if p.err != nil {
		return false, p.err
	}
	if yes {
		return true, nil
	}
	return c.confirm(question)
}

func (c *CLI) confirm(question string) (bool, error) {
	var p writes
	p.fprintf(c.out, "%s [y/N] ", question)
	if p.err != nil {
		return false, p.err
	}
	answer, err := c.in.ReadString('\n')
	if err != nil && answer == "" {
		p.fprintln(c.out)
		return false, p.err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

func (c *CLI) showStatus(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return usagef("status takes no arguments")
	}
	s, err := c.status.Get(ctx)
	if err != nil {
		return err
	}
	var p writes
	w := c.table()
	p.fprintf(w, "Time\t%s\n", s.Time.Format("2006-01-02 15:04"))
	p.fprintf(w, "Lman\t%s\n", s.Version)
	p.fprintf(w, "Host\t%s\n", s.System.Hostname)
	p.fprintf(w, "System\t%s %s (%s)\n", s.System.OS, s.System.OSVersion, s.System.Arch)
	p.fprintf(w, "CPU\t%s, %d cores\n", s.System.CPU, s.System.Cores)
	p.fprintf(w, "Memory\t%.1f GiB\n", float64(s.System.MemoryBytes)/(1<<30))
	if p.err != nil {
		return p.err
	}
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
	var p writes
	p.fprintf(c.out, "Theme: %s. The desktop application applies it on the next start.\n", theme)
	return p.err
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
