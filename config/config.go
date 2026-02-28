package config

// Config contains environment-driven runtime settings for the web server and analyzer.
type Config struct {
	// HttpListenAddress is the bind address used by the HTTP server (for example ":8080").
	HttpListenAddress       string `split_words:"true" default:":8080"`
	// RequestTimeoutSeconds is the overall timeout for fetching and analyzing a target page.
	RequestTimeoutSeconds   int    `split_words:"true" default:"20"`
	// LinkCheckTimeoutSeconds is the per-link timeout used while checking discovered links.
	LinkCheckTimeoutSeconds int    `split_words:"true" default:"5"`
	// MaxCheckedLinks caps how many unique links are actively checked for accessibility.
	MaxCheckedLinks         int    `split_words:"true" default:"150"`
}
