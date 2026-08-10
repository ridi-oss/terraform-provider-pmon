package pmonmcp

// MaskFn is a masking function a column classification can point at. Cedar decides only
// unmasked, masked, or deny; which mask a masked column actually gets is this.
type MaskFn struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// ColumnTag is one classified column, as list_column_tags reports it.
type ColumnTag struct {
	Schema     string   `json:"schema"`
	Table      string   `json:"table"`
	Column     string   `json:"column"`
	Tags       []string `json:"tags"`
	MaskFnName *string  `json:"maskFnName"`
}
