package config

// Config holds the runtime configuration
type Config struct {
	Host        string
	User        string
	Password    string
	Port        int
	OutputPath  string
	Debug       bool
	BHURL       string
	BHKeyID     string
	BHKeySecret string
}
