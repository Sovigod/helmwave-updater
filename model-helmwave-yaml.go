package main

// Структуры для десериализации helmwave yaml (helmwave.yml.tpl)
// Поля снабжены тегами `yaml` для корректного распарсивания.

// Helmwave представляет корневой объект файла.
type Helmwave struct {
	Releases []Release `yaml:"releases,omitempty"`
}

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
