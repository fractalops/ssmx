package config

// Config is the full contents of ~/.ssmx/config.yaml.
type Config struct {
	DefaultProfile string            `mapstructure:"default_profile" yaml:"default_profile,omitempty"`
	DefaultRegion  string            `mapstructure:"default_region"  yaml:"default_region,omitempty"`
	SSHKeyPath     string            `mapstructure:"ssh_key_path"    yaml:"ssh_key_path,omitempty"`
	Aliases        map[string]string `mapstructure:"aliases"         yaml:"aliases,omitempty"`
	DocAliases     map[string]string `mapstructure:"doc_aliases"     yaml:"doc_aliases,omitempty"`
}

// DefaultDocAliases are the built-in SSM document aliases.
var DefaultDocAliases = map[string]string{
	"patch":          "AWS-PatchInstanceWithRollback",
	"install":        "AWS-ConfigureAWSPackage",
	"ansible":        "AWS-RunAnsiblePlaybook",
	"update-windows": "AWS-InstallWindowsUpdates",
}
