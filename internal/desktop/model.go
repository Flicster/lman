package desktop

// Overview is the application context shown in the header and the status bar.
type Overview struct {
	Host    string    `json:"host"`
	Remote  bool      `json:"remote"`
	Servers []string  `json:"servers"`
	Theme   string    `json:"theme"`
	Version string    `json:"version"`
	System  SystemDTO `json:"system"`
}

type SystemDTO struct {
	Hostname  string  `json:"hostname"`
	OS        string  `json:"os"`
	OSVersion string  `json:"osVersion"`
	Arch      string  `json:"arch"`
	CPU       string  `json:"cpu"`
	Cores     int     `json:"cores"`
	MemoryGiB float64 `json:"memoryGiB"`
}

// KnownLanguage is a language Lman knows how to detect, installed or not.
type KnownLanguage struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

// LanguageCard is a tile of the language list.
type LanguageCard struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Active   string `json:"active"`
	Versions int    `json:"versions"`
}

// LanguageDetail is the second screen: everything about one language.
type LanguageDetail struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Versions []VersionDTO `json:"versions"`
	Hints    []HintDTO    `json:"hints"`
	Env      []EnvVarDTO  `json:"env"`
	DocsURL  string       `json:"docsUrl"`
}

type VersionDTO struct {
	Label    string `json:"label"`
	Path     string `json:"path"`
	RealPath string `json:"realPath"`
	Method   string `json:"method"`
	// Installed is an ISO date, empty when unknown.
	Installed string `json:"installed"`
	Active    bool   `json:"active"`
}

type HintDTO struct {
	Tool    string `json:"tool"`
	Purpose string `json:"purpose"`
}

type EnvVarDTO struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Managed bool   `json:"managed"`
	Default bool   `json:"default"`
}

type ManualDTO struct {
	Text    string `json:"text"`
	DocsURL string `json:"docsUrl"`
}

// Result is the outcome of an action, shown as a notice.
type Result struct {
	Message string `json:"message"`
	Warning bool   `json:"warning"`
}
