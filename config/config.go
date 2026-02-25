package config

type Config struct {
	HttpListenAddress string `required:"true" split_words:"true"`
}
