package schema

type Schema struct {
	Tables []Table `json:"tables"`
}

type Table struct {
	Schema       string       `json:"schema"`
	Name         string       `json:"name"`
	PrimaryKey   []string     `json:"primary_key"`
	Columns      []Column     `json:"columns"`
	ForeignKeys  []ForeignKey `json:"foreign_keys"`
	ReferencedBy []ForeignKey `json:"referenced_by"`
	Indexes      []Index      `json:"indexes"`
}

type Column struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Nullable    bool    `json:"nullable"`
	Default     *string `json:"default,omitempty"`
	IsGenerated bool    `json:"is_generated,omitempty"`
	IsIdentity  bool    `json:"is_identity,omitempty"`
	Comment     string  `json:"comment,omitempty"`
}

type ForeignKey struct {
	Table      string   `json:"table"`
	Columns    []string `json:"columns"`
	References []string `json:"references"`
}

type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}
