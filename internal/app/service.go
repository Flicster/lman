package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/Flicster/lman/internal/domain"
)

// Service runs the language scenarios on one host.
// The scan result is cached until Refresh or a change made through the Service.
type Service struct {
	host    Host
	env     Environment
	catalog []domain.Definition

	mu      sync.Mutex
	cache   *snapshot
	pending map[string][]domain.Removal
}

type snapshot struct {
	env       map[string]string
	home      string
	links     map[string]string
	languages []domain.Language
}

// NewService manages the catalog languages on host.
func NewService(host Host, env Environment) *Service {
	return &Service{host: host, env: env, catalog: domain.Catalog()}
}

// Host names the computer the Service works on.
func (s *Service) Host() string {
	return s.host.Name()
}

// Languages lists the installed languages.
func (s *Service) Languages(ctx context.Context) ([]domain.Language, error) {
	snap, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return slices.Clone(snap.languages), nil
}

// Refresh scans the host again.
func (s *Service) Refresh(ctx context.Context) ([]domain.Language, error) {
	s.invalidate()
	return s.Languages(ctx)
}

// Language returns an installed language by id, name or alias.
func (s *Service) Language(ctx context.Context, id string) (domain.Language, error) {
	l, _, err := s.language(ctx, id)
	return l, err
}

// Manual returns the built-in help of the active version, or of the newest one.
func (s *Service) Manual(ctx context.Context, id string) (domain.Manual, error) {
	l, snap, err := s.language(ctx, id)
	if err != nil {
		return domain.Manual{}, err
	}
	v, ok := l.Active()
	if !ok {
		v = l.Versions[0]
	}
	if strings.HasSuffix(strings.ToLower(v.Path), ".dll") {
		return domain.Manual{
			Text:    l.Name + " compiler ships inside the .NET SDK.\n",
			DocsURL: l.DocsURL,
		}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := s.host.Run(ctx, Command{Path: v.Path, Args: l.ManualArgs, Env: probeEnv(snap.env)})
	if err != nil && len(out) == 0 {
		return domain.Manual{}, fmt.Errorf("run %s %s: %w", v.Path, strings.Join(l.ManualArgs, " "), err)
	}
	return domain.Manual{Text: string(out), DocsURL: l.DocsURL}, nil
}

// Env lists the language variables. A toolchain that prints its own environment,
// such as go env, supplies the effective values, including defaults it fills in.
func (s *Service) Env(ctx context.Context, id string) ([]domain.EnvVar, error) {
	l, snap, err := s.language(ctx, id)
	if err != nil {
		return nil, err
	}
	managed, err := s.env.Vars(ctx)
	if err != nil {
		return nil, fmt.Errorf("read variables: %w", err)
	}
	reported, err := s.toolEnv(ctx, l, snap.env)
	if err != nil {
		return nil, err
	}
	names := l.EnvVars
	if len(reported) > 0 {
		names = slices.Sorted(maps.Keys(reported))
	}
	names = slices.DeleteFunc(names, domain.LoaderVar)
	vars := make([]domain.EnvVar, 0, len(names))
	for _, name := range names {
		v := domain.EnvVar{Name: name, Value: snap.env[name]}
		if reported != nil {
			v.Value = reported[name]
			_, inEnv := snap.env[name]
			_, isManaged := managed[name]
			v.Default = v.Value != "" && !inEnv && !isManaged
		}
		if value, ok := managed[name]; ok {
			v.Value = value
			v.Managed = true
			v.Default = false
		}
		vars = append(vars, v)
	}
	return vars, nil
}

// toolEnv runs EnvArgs and reads a JSON object. A toolchain that does not answer
// leaves the caller with the static variable list.
func (s *Service) toolEnv(ctx context.Context, l domain.Language, env map[string]string) (map[string]string, error) {
	if len(l.EnvArgs) == 0 {
		return nil, nil
	}
	v, ok := l.Active()
	if !ok {
		if len(l.Versions) == 0 {
			return nil, nil
		}
		v = l.Versions[0]
	}
	if strings.HasSuffix(strings.ToLower(v.Path), ".dll") {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := s.host.Run(ctx, Command{Path: v.Path, Args: l.EnvArgs, Env: toolchainEnv(env, l)})
	if err != nil && len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	var raw map[string]string
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("read %s environment: %w", l.Name, err)
	}
	for name := range raw {
		if !l.ReportsVar(name) {
			delete(raw, name)
		}
	}
	return raw, nil
}

// SetEnv sets a language variable for new terminals; an empty value stops setting it.
func (s *Service) SetEnv(ctx context.Context, id, name, value string) error {
	l, _, err := s.language(ctx, id)
	if err != nil {
		return err
	}
	if err := domain.ValidateVarName(name); err != nil {
		return err
	}
	if domain.LoaderVar(name) {
		return fmt.Errorf("%w: %s", domain.ErrLoaderVar, name)
	}
	if !l.ReportsVar(name) {
		return fmt.Errorf("%w: %s is not a %s variable, use one of %s",
			domain.ErrForeignVar, name, l.Name, strings.Join(l.EnvVars, ", "))
	}
	defer s.invalidate()
	if err := s.env.SetVar(ctx, name, value); err != nil {
		return fmt.Errorf("set variable: %w", err)
	}
	return nil
}

// Use makes a version active: its executables come first in PATH of new terminals.
func (s *Service) Use(ctx context.Context, id, version string) (Activation, error) {
	l, snap, err := s.language(ctx, id)
	if err != nil {
		return Activation{}, err
	}
	v, err := l.Find(version)
	if err != nil {
		return Activation{}, err
	}
	if strings.HasSuffix(strings.ToLower(v.RealPath), ".dll") {
		return Activation{}, fmt.Errorf("%s %s ships inside the .NET SDK and follows the C# toolchain", l.Name, l.Label(v.Number))
	}
	links, err := s.toolLinks(ctx, l, v)
	if err != nil {
		return Activation{}, err
	}
	var stale []string
	for _, name := range tools(l.Definition) {
		if _, linked := snap.links[name]; linked {
			if _, keep := links[name]; !keep {
				stale = append(stale, name)
			}
		}
	}

	s.invalidate()
	if err := s.env.Unlink(ctx, stale); err != nil {
		return Activation{}, fmt.Errorf("unlink tools: %w", err)
	}
	if err := s.env.Link(ctx, links); err != nil {
		return Activation{}, fmt.Errorf("link tools: %w", err)
	}

	fresh, err := s.Language(ctx, id)
	if err != nil {
		return Activation{}, err
	}
	active, ok := fresh.Active()
	switch {
	case !ok:
		return Activation{Version: v, NotInPath: true}, nil
	case active.RealPath != v.RealPath:
		return Activation{Version: v, ShadowedBy: active.Path}, nil
	}
	return Activation{Version: active}, nil
}

// toolLinks maps every language tool present next to the version executable.
func (s *Service) toolLinks(ctx context.Context, l domain.Language, v domain.Version) (map[string]string, error) {
	exts := []string{""}
	if s.host.Platform() == domain.Windows {
		exts = windowsExts
	}
	names := tools(l.Definition)
	var patterns []string
	for _, name := range names {
		for _, ext := range exts {
			patterns = append(patterns, v.BinDir()+"/"+name+ext)
		}
	}
	found, err := s.host.Glob(ctx, patterns)
	if err != nil {
		return nil, fmt.Errorf("find tools: %w", err)
	}
	links := make(map[string]string)
	for i, files := range found {
		name := names[i/len(exts)]
		if _, ok := links[name]; !ok && len(files) > 0 {
			links[name] = files[0]
		}
	}
	if primary := l.Primaries[0]; links[primary] == "" {
		links[primary] = v.RealPath
	}
	return links, nil
}

// Plan describes what removing one version, or the whole language when version is empty,
// would do. It is computed from a fresh scan, the same one the removal starts from.
func (s *Service) Plan(ctx context.Context, id, version string) ([]string, error) {
	s.invalidate()
	defer s.invalidate()
	l, snap, err := s.language(ctx, id)
	if err != nil {
		return nil, err
	}
	if version == "" {
		var lines []string
		var plans []domain.Removal
		for _, v := range l.Versions {
			label := l.Label(v.Number)
			for _, line := range s.planLines(v, snap) {
				lines = append(lines, label+": "+line)
			}
			if plan, ok := executableRemoval(v, s.host.Platform(), snap.home); ok {
				plans = append(plans, plan)
			}
		}
		for _, name := range tools(l.Definition) {
			if target, ok := snap.links[name]; ok {
				lines = append(lines, fmt.Sprintf("Unlink %s → %s", s.env.ShimDir()+"/"+name, target))
			}
		}
		s.storePlan(id, "", plans)
		return lines, nil
	}
	v, err := l.Find(version)
	if err != nil {
		return nil, err
	}
	if v.Active {
		return nil, fmt.Errorf("%w: make another %s version active first, or remove the language", domain.ErrActiveVersion, l.Name)
	}
	lines := s.planLines(v, snap)
	var plans []domain.Removal
	if plan, ok := executableRemoval(v, s.host.Platform(), snap.home); ok {
		plans = append(plans, plan)
	}
	for name, target := range snap.links {
		if path.Dir(target) == v.BinDir() {
			lines = append(lines, fmt.Sprintf("Unlink %s → %s", s.env.ShimDir()+"/"+name, target))
		}
	}
	slices.Sort(lines)
	s.storePlan(id, version, plans)
	return lines, nil
}

func (s *Service) planLines(v domain.Version, snap *snapshot) []string {
	plan, err := domain.PlanRemoval(v, s.host.Platform(), snap.home)
	switch {
	case err != nil:
		var manual *domain.ManualRemovalError
		if errors.As(err, &manual) && manual.Hint != "" {
			return []string{"Not removed by lman, run yourself: " + manual.Hint}
		}
		return []string{"Not removed by lman"}
	case plan.Dir != "":
		return []string{"Delete directory: " + plan.Dir}
	}
	return []string{"Run: " + strings.Join(plan.Command, " ")}
}

// RemoveVersion deletes one installed version. The active version stays: switch first.
// The removal starts from a fresh scan, so a directory that changed since the last look
// is not deleted on the strength of an old plan.
func (s *Service) RemoveVersion(ctx context.Context, id, version string) error {
	s.invalidate()
	defer s.invalidate()
	l, snap, err := s.language(ctx, id)
	if err != nil {
		return err
	}
	v, err := l.Find(version)
	if err != nil {
		return err
	}
	if v.Active {
		return fmt.Errorf("%w: make another %s version active first, or remove the language", domain.ErrActiveVersion, l.Name)
	}
	plan, err := domain.PlanRemoval(v, s.host.Platform(), snap.home)
	if err != nil {
		return err
	}
	if shown, ok := s.shownPlan(id, version); ok && !planShown(shown, plan) {
		return fmt.Errorf("%w: %s %s changed since it was shown", domain.ErrRemovalNotAllowed, l.Name, l.Label(v.Number))
	}
	if err := s.execute(ctx, plan, snap.env); err != nil {
		return err
	}
	return s.unlinkInto(ctx, snap.links, v.BinDir())
}

// RemoveLanguage deletes every version Lman can delete and the language shims.
// Versions that need manual removal are reported in the joined error.
func (s *Service) RemoveLanguage(ctx context.Context, id string) error {
	s.invalidate()
	defer s.invalidate()
	l, snap, err := s.language(ctx, id)
	if err != nil {
		return err
	}
	shown, limited := s.shownPlan(id, "")
	var errs []error
	for _, v := range l.Versions {
		plan, err := domain.PlanRemoval(v, s.host.Platform(), snap.home)
		if err == nil && limited && !planShown(shown, plan) {
			err = fmt.Errorf("%w: it was not part of the removal that was shown", domain.ErrRemovalNotAllowed)
		}
		if err == nil {
			err = s.execute(ctx, plan, snap.env)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", l.Name, l.Label(v.Number), err))
		}
	}
	var linked []string
	for _, name := range tools(l.Definition) {
		if _, ok := snap.links[name]; ok {
			linked = append(linked, name)
		}
	}
	if err := s.env.Unlink(ctx, linked); err != nil {
		errs = append(errs, fmt.Errorf("unlink tools: %w", err))
	}
	return errors.Join(errs...)
}

func (s *Service) execute(ctx context.Context, plan domain.Removal, env map[string]string) error {
	if plan.Dir != "" {
		if err := s.host.RemoveAll(ctx, plan.Dir); err != nil {
			return fmt.Errorf("remove %s: %w", plan.Dir, err)
		}
		return nil
	}
	out, err := s.host.Run(ctx, Command{Path: plan.Command[0], Args: plan.Command[1:], Env: probeEnv(env)})
	if err != nil {
		return fmt.Errorf("run %s: %w: %s", strings.Join(plan.Command, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *Service) unlinkInto(ctx context.Context, links map[string]string, dir string) error {
	var names []string
	for name, target := range links {
		if path.Dir(target) == dir {
			names = append(names, name)
		}
	}
	if err := s.env.Unlink(ctx, names); err != nil {
		return fmt.Errorf("unlink tools: %w", err)
	}
	return nil
}

func (s *Service) language(ctx context.Context, id string) (domain.Language, *snapshot, error) {
	i := slices.IndexFunc(s.catalog, func(d domain.Definition) bool { return d.Matches(id) })
	if i < 0 {
		return domain.Language{}, nil, fmt.Errorf("%w: %q", domain.ErrUnknownLanguage, id)
	}
	snap, err := s.snapshot(ctx)
	if err != nil {
		return domain.Language{}, nil, err
	}
	for _, l := range snap.languages {
		if l.ID == s.catalog[i].ID {
			return l, snap, nil
		}
	}
	return domain.Language{}, nil, fmt.Errorf("%w: %s", domain.ErrNotInstalled, s.catalog[i].Name)
}

func (s *Service) snapshot(ctx context.Context) (*snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache != nil {
		return s.cache, nil
	}
	snap, err := s.scan(ctx)
	if err != nil {
		return nil, err
	}
	s.cache = snap
	return snap, nil
}

func (s *Service) invalidate() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
}

func (s *Service) storePlan(id, version string, plans []domain.Removal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		s.pending = map[string][]domain.Removal{}
	}
	s.pending[id+"\x00"+version] = slices.Clone(plans)
}

func (s *Service) shownPlan(id, version string) ([]domain.Removal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plans, ok := s.pending[id+"\x00"+version]
	return plans, ok
}

func executableRemoval(v domain.Version, p domain.Platform, home string) (domain.Removal, bool) {
	plan, err := domain.PlanRemoval(v, p, home)
	if err != nil {
		return domain.Removal{}, false
	}
	return plan, true
}

func planShown(shown []domain.Removal, plan domain.Removal) bool {
	for _, item := range shown {
		if plan.Dir != "" {
			if item.Dir == plan.Dir {
				return true
			}
			continue
		}
		if len(item.Command) > 0 && slices.Equal(item.Command, plan.Command) {
			return true
		}
	}
	return false
}

// tools are the executables linked for a language: primaries first, without duplicates.
func tools(d domain.Definition) []string {
	var out []string
	for _, name := range slices.Concat(d.Primaries, d.Tools) {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}
