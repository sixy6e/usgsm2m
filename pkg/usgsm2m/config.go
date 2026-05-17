package usgsm2m

type Config struct {
	Username    string `mapstructure:"username"`
	Token       string `mapstructure:"token"`
	OutputDir   string `mapstructure:"output_dir"`
	Concurrency int    `mapstructure:"concurrency"`
	Dataset     string `mapstructure:"dataset"`
}
