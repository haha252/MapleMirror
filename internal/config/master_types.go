package config

type RequestID struct {
	ResponseHeader string `yaml:"response_header"`
	ParentHeader   string `yaml:"parent_header"`
}
