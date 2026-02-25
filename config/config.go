package config

type Config struct {
	HttpListenAddress       string `split_words:"true" default:":8080"`
	RequestTimeoutSeconds   int    `split_words:"true" default:"20"`
	LinkCheckTimeoutSeconds int    `split_words:"true" default:"5"`
	MaxCheckedLinks         int    `split_words:"true" default:"150"`
}
