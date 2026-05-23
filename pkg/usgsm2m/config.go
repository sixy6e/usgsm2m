package usgsm2m

type Config struct {
	Auth     AuthConfig    `mapstructure:"auth"`
	Defaults DefaultParams `mapstructure:"defaults"`
}

type AuthConfig struct {
	Username string `mapstructure:"username"`
	Token    string `mapstructure:"token"`
}

type DefaultParams struct {
	OutputDir   string `mapstructure:"output_dir"`
	Concurrency int    `mapstructure:"concurrency"`
	Dataset     string `mapstructure:"dataset"`
}
