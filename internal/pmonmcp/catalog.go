package pmonmcp

// CatalogColumn is one row of browse_catalog. Despite the tool's name the response is flat: a row
// per column, not schemas nested over tables nested over columns.
type CatalogColumn struct {
	Catalog        string          `json:"catalog"`
	Schema         string          `json:"schema"`
	Table          string          `json:"table"`
	Column         string          `json:"column"`
	DataType       string          `json:"dataType"`
	SQLType        string          `json:"sqlType"`
	Ordinal        int64           `json:"ordinal"`
	Nullable       bool            `json:"nullable"`
	IsTemp         bool            `json:"isTemp"`
	Classification *Classification `json:"classification"`
}

// Classification is the tag set on a column, as it appears nested inside a catalog row or a table
// detail. It is absent on an unclassified column.
type Classification struct {
	Schema string   `json:"schema"`
	Table  string   `json:"table"`
	Column string   `json:"column"`
	Tags   []string `json:"tags"`
}

// TableDetail is get_table_detail: the live shape of one table plus whatever pmon knows about it.
type TableDetail struct {
	Schema       string         `json:"schema"`
	Table        string         `json:"table"`
	Columns      []DetailColumn `json:"columns"`
	Indexes      []TableIndex   `json:"indexes"`
	ForeignKeys  []ForeignKey   `json:"foreignKeys"`
	ReferencedBy []ForeignKey   `json:"referencedBy"`
	Metadata     *TableMetadata `json:"metadata"`
}

// DetailColumn is one column of a table detail. The numeric and length fields are engine
// dependent and absent for types they do not apply to.
type DetailColumn struct {
	Name                   string          `json:"name"`
	DataType               string          `json:"dataType"`
	Ordinal                int64           `json:"ordinal"`
	Nullable               bool            `json:"nullable"`
	DefaultValue           *string         `json:"defaultValue"`
	NumericPrecision       *int64          `json:"numericPrecision"`
	NumericScale           *int64          `json:"numericScale"`
	CharacterMaximumLength *int64          `json:"characterMaximumLength"`
	PartOfIndex            bool            `json:"partOfIndex"`
	AutoIncrement          bool            `json:"autoIncrement"`
	Comment                string          `json:"comment"`
	Charset                *string         `json:"charset"`
	Collation              *string         `json:"collation"`
	Classification         *Classification `json:"classification"`
}

// TableIndex is one index on a table.
type TableIndex struct {
	Name    string        `json:"name"`
	Columns []IndexColumn `json:"columns"`
	Unique  bool          `json:"unique"`
	Type    string        `json:"type"`
}

// IndexColumn is one column's part in an index.
type IndexColumn struct {
	Name      string `json:"name"`
	Position  int64  `json:"position"`
	Direction string `json:"direction"`
}

// ForeignKey is a relationship in either direction: outgoing under foreignKeys, incoming under
// referencedBy.
type ForeignKey struct {
	Name          string   `json:"name"`
	SourceSchema  string   `json:"sourceSchema"`
	SourceTable   string   `json:"sourceTable"`
	SourceColumns []string `json:"sourceColumns"`
	TargetSchema  string   `json:"targetSchema"`
	TargetTable   string   `json:"targetTable"`
	TargetColumns []string `json:"targetColumns"`
	OnUpdate      string   `json:"onUpdate"`
	OnDelete      string   `json:"onDelete"`
}

// TableMetadata is the storage engine's own view of a table. EstimatedRows is exactly that: an
// estimate the engine keeps, not a count.
type TableMetadata struct {
	Engine        string `json:"engine"`
	EstimatedRows int64  `json:"estimatedRows"`
	RowFormat     string `json:"rowFormat"`
	OnDiskBytes   int64  `json:"onDiskBytes"`
	Collation     string `json:"collation"`
	Comment       string `json:"comment"`
}

// DatasourceLiveness is get_datasource_liveness: whether a proxy is currently attached.
type DatasourceLiveness struct {
	Datasource      string  `json:"datasource"`
	Attached        bool    `json:"attached"`
	CatalogSyncedAt *string `json:"catalogSyncedAt"`
	LastSeenAt      *string `json:"lastSeenAt"`
}
