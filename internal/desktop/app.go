// Package desktop is the window of Lman: methods of App are called from the web interface.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"lman/internal/app"
	"lman/internal/domain"
)

const (
	scanTimeout   = 2 * time.Minute
	changeTimeout = 10 * time.Minute
	shortTimeout  = 30 * time.Second
)

// Manager is the language service of one host.
type Manager interface {
	Host() string
	Languages(ctx context.Context) ([]domain.Language, error)
	Refresh(ctx context.Context) ([]domain.Language, error)
	Language(ctx context.Context, id string) (domain.Language, error)
	Manual(ctx context.Context, id string) (domain.Manual, error)
	Env(ctx context.Context, id string) ([]domain.EnvVar, error)
	SetEnv(ctx context.Context, id, name, value string) error
	Use(ctx context.Context, id, version string) (app.Activation, error)
	RemoveVersion(ctx context.Context, id, version string) error
	RemoveLanguage(ctx context.Context, id string) error
}

// Opener connects to the local computer for an empty server, otherwise to the server.
type Opener func(ctx context.Context, server string) (Manager, func() error, error)

// Settings keeps preferences between runs.
type Settings interface {
	Preferences(ctx context.Context) (app.Preferences, error)
	SetTheme(ctx context.Context, name string) (domain.Theme, error)
	RememberServer(ctx context.Context, server string) error
}

// Status reports the application context.
type Status interface {
	Get(ctx context.Context) (app.StatusInfo, error)
}

// App is bound to the web interface. It works with one host at a time.
type App struct {
	open     Opener
	settings Settings
	status   Status

	mu      sync.Mutex
	manager Manager
	closeFn func() error
	server  string
}

// New starts with the local computer, connected on first use.
func New(open Opener, settings Settings, status Status) *App {
	return &App{open: open, settings: settings, status: status}
}

// Overview returns the host, preferences and system characteristics.
func (a *App) Overview() (Overview, error) {
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()
	m, err := a.current(ctx)
	if err != nil {
		return Overview{}, err
	}
	prefs, err := a.settings.Preferences(ctx)
	if err != nil {
		return Overview{}, err
	}
	st, err := a.status.Get(ctx)
	if err != nil {
		return Overview{}, err
	}
	a.mu.Lock()
	remote := a.server != ""
	a.mu.Unlock()
	return Overview{
		Host:    m.Host(),
		Remote:  remote,
		Servers: prefs.Servers,
		Theme:   string(prefs.Theme),
		Version: st.Version,
		System: SystemDTO{
			Hostname:  st.System.Hostname,
			OS:        st.System.OS,
			OSVersion: st.System.OSVersion,
			Arch:      st.System.Arch,
			CPU:       st.System.CPU,
			Cores:     st.System.Cores,
			MemoryGiB: float64(st.System.MemoryBytes) / (1 << 30),
		},
	}, nil
}

// Connect switches to server over SSH; an empty server returns to the local computer.
func (a *App) Connect(server string) (Overview, error) {
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()
	m, closeFn, err := a.open(ctx, server)
	if err != nil {
		return Overview{}, err
	}
	if err := a.settings.RememberServer(ctx, server); err != nil {
		return Overview{}, errors.Join(err, closeFn())
	}
	a.mu.Lock()
	previous := a.closeFn
	a.manager, a.closeFn, a.server = m, closeFn, server
	a.mu.Unlock()
	if previous != nil {
		if err := previous(); err != nil {
			log.Printf("close previous connection: %v", err)
		}
	}
	return a.Overview()
}

// Known lists every language Lman can detect. The list does not depend on what is installed.
func (a *App) Known() []KnownLanguage {
	cat := domain.Catalog()
	out := make([]KnownLanguage, len(cat))
	for i, d := range cat {
		aliases := d.Aliases
		if aliases == nil {
			aliases = []string{}
		}
		out[i] = KnownLanguage{ID: string(d.ID), Name: d.Name, Aliases: aliases}
	}
	return out
}

// Languages lists installed languages; refresh scans the host again.
func (a *App) Languages(refresh bool) ([]LanguageCard, error) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()
	m, err := a.current(ctx)
	if err != nil {
		return nil, err
	}
	list := m.Languages
	if refresh {
		list = m.Refresh
	}
	langs, err := list(ctx)
	if err != nil {
		return nil, err
	}
	cards := make([]LanguageCard, 0, len(langs))
	for _, l := range langs {
		card := LanguageCard{ID: string(l.ID), Name: l.Name, Versions: len(l.Versions)}
		if v, ok := l.Active(); ok {
			card.Active = l.Label(v.Number)
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// Language returns versions, hints and variables of a language.
func (a *App) Language(id string) (LanguageDetail, error) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()
	m, err := a.current(ctx)
	if err != nil {
		return LanguageDetail{}, err
	}
	l, err := m.Language(ctx, id)
	if err != nil {
		return LanguageDetail{}, err
	}
	vars, err := m.Env(ctx, id)
	if err != nil {
		return LanguageDetail{}, err
	}
	d := LanguageDetail{ID: string(l.ID), Name: l.Name, DocsURL: l.DocsURL, Env: envDTOs(vars)}
	for _, v := range l.Versions {
		dto := VersionDTO{
			Label:    l.Label(v.Number),
			Path:     v.Path,
			RealPath: v.RealPath,
			Method:   v.Method.String(),
			Active:   v.Active,
		}
		if !v.InstalledAt.IsZero() {
			dto.Installed = v.InstalledAt.Format(time.DateOnly)
		}
		d.Versions = append(d.Versions, dto)
	}
	for _, h := range l.Hints {
		d.Hints = append(d.Hints, HintDTO{Tool: h.Tool, Purpose: h.Purpose})
	}
	return d, nil
}

// Manual returns the built-in help of the language.
func (a *App) Manual(id string) (ManualDTO, error) {
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()
	m, err := a.current(ctx)
	if err != nil {
		return ManualDTO{}, err
	}
	manual, err := m.Manual(ctx, id)
	if err != nil {
		return ManualDTO{}, err
	}
	return ManualDTO{Text: manual.Text, DocsURL: manual.DocsURL}, nil
}

// SetEnv sets a language variable for new terminals and returns the updated list.
func (a *App) SetEnv(id, name, value string) ([]EnvVarDTO, error) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()
	m, err := a.current(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.SetEnv(ctx, id, name, value); err != nil {
		return nil, err
	}
	vars, err := m.Env(ctx, id)
	if err != nil {
		return nil, err
	}
	return envDTOs(vars), nil
}

// Use makes the version at path active in new terminals.
func (a *App) Use(id, path string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()
	m, err := a.current(ctx)
	if err != nil {
		return Result{}, err
	}
	act, err := m.Use(ctx, id, path)
	if err != nil {
		return Result{}, err
	}
	l, err := m.Language(ctx, id)
	if err != nil {
		return Result{}, err
	}
	label := l.Name + " " + l.Label(act.Version.Number)
	if act.NotInPath {
		return Result{
			Message: label + " is linked, but new terminals do not load lman. Add ~/.lman/bin to PATH in the shell profile.",
			Warning: true,
		}, nil
	}
	if act.ShadowedBy != "" {
		return Result{
			Message: fmt.Sprintf("%s is linked, but %s comes first in PATH. Move it after lman in the shell profile.", label, act.ShadowedBy),
			Warning: true,
		}, nil
	}
	return Result{Message: label + " is active. Open a new terminal to use it."}, nil
}

// RemoveVersion deletes the version at path.
func (a *App) RemoveVersion(id, path string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), changeTimeout)
	defer cancel()
	m, err := a.current(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := m.RemoveVersion(ctx, id, path); err != nil {
		return Result{}, err
	}
	return Result{Message: "The version is removed."}, nil
}

// RemoveLanguage deletes every version of the language.
func (a *App) RemoveLanguage(id string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), changeTimeout)
	defer cancel()
	m, err := a.current(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := m.RemoveLanguage(ctx, id); err != nil {
		return Result{}, err
	}
	return Result{Message: "The language is removed."}, nil
}

// SetTheme stores the theme and returns its name.
func (a *App) SetTheme(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()
	theme, err := a.settings.SetTheme(ctx, name)
	return string(theme), err
}

func (a *App) current(ctx context.Context) (Manager, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager != nil {
		return a.manager, nil
	}
	m, closeFn, err := a.open(ctx, "")
	if err != nil {
		return nil, err
	}
	a.manager, a.closeFn = m, closeFn
	return m, nil
}

func (a *App) close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closeFn == nil {
		return nil
	}
	err := a.closeFn()
	a.manager, a.closeFn = nil, nil
	return err
}

func envDTOs(vars []domain.EnvVar) []EnvVarDTO {
	out := make([]EnvVarDTO, 0, len(vars))
	for _, v := range vars {
		out = append(out, EnvVarDTO{Name: v.Name, Value: v.Value, Managed: v.Managed, Default: v.Default})
	}
	return out
}
