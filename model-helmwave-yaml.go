package main

// Структуры для десериализации helmwave yaml (helmwave.yml.tpl)
// Поля снабжены тегами `yaml` для корректного распарсивания.

// Helmwave представляет корневой объект файла.
type Helmwave struct {
	Registries   []Registry   `yaml:"registries,omitempty"`
	Repositories []Repository `yaml:"repositories,omitempty"`
	Releases     []Release    `yaml:"releases,omitempty"`
}

// Registry представляет запись в списке registries.
type Registry struct {
	Host string `yaml:"host"`
}

// Repository представляет запись в списке repositories.
type Repository struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// Options содержит общие опции, которые в шаблоне задаются через якорь &options
type Options struct {
	Force                  bool   `yaml:"force,omitempty"`
	Atomic                 bool   `yaml:"atomic,omitempty"`
	MaxHistory             int    `yaml:"max_history,omitempty"`
	CreateNamespace        bool   `yaml:"create_namespace,omitempty"`
	ResetValues            bool   `yaml:"reset_values,omitempty"`
	PendingReleaseStrategy string `yaml:"pending_release_strategy,omitempty"`
	Context                string `yaml:"context,omitempty"`
}

// Release описывает один релиз в списке releases.
// Встраиваем Options с тегом inline, чтобы поддерживать оператор слияния YAML (<<: *options)
type Release struct {
	Name      string        `yaml:"name"`
	Chart     Chart         `yaml:"chart"`
	Namespace string        `yaml:"namespace,omitempty"`
	Tags      []string      `yaml:"tags,omitempty"`
	Values    []interface{} `yaml:"values,omitempty"`

	// Inline captures any additional merged keys (for example from <<: *options)
	Inline map[string]interface{} `yaml:",inline"`
}

// Chart описывает информацию о чарте для релиза.
type Chart struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version,omitempty"`
	// capture additional arbitrary chart keys (e.g. insecureskiptlsverify)
	Other map[string]interface{} `yaml:",inline"`
}
