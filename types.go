package resolver

type ExtensionDefinition struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   ExtensionMetadata `yaml:"metadata" json:"metadata"`
	Spec       ExtensionSpec     `yaml:"spec" json:"spec"`
}

type ExtensionMetadata struct {
	ID string `yaml:"id" json:"id"`
}

type ExtensionSpec struct {
	Dependencies []string `yaml:"dependencies" json:"dependencies"`

	Kinds          []KindDef          `yaml:"kinds" json:"kinds"`
	InterfaceTypes []InterfaceTypeDef `yaml:"interfaceTypes" json:"interfaceTypes"`
	Schemas        []SchemaDef        `yaml:"schemas" json:"schemas"`
}

type KindDef struct {
	Name string `yaml:"name" json:"name"`
}

type InterfaceTypeDef struct {
	Name       string `yaml:"name" json:"name"`
	TargetKind string `yaml:"targetKind" json:"targetKind"`
}

type SchemaDef struct {
	ID                     string         `yaml:"id" json:"id"`
	AppliesToKind          string         `yaml:"appliesToKind" json:"appliesToKind"`
	AppliesToInterfaceType string         `yaml:"appliesToInterfaceType" json:"appliesToInterfaceType"`
	Description            string         `yaml:"description" json:"description"`
	Schema                 map[string]any `yaml:"schema" json:"schema"`
}
