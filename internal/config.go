package ferry

import (
	prettyconsole "github.com/thessem/zap-prettyconsole"
	"go.uber.org/zap"
)

var logger = prettyconsole.NewLogger(zap.DebugLevel)

type HealthCheck struct {
	Path              string `yaml:"path" default:"/health"`
	Interval          string `yaml:"interval" default:"30s"`
	Timeout           string `yaml:"timeout" default:"5s"`
	SuccessStatusCode int    `yaml:"success_status_code" default:"200"`
}

type Config struct {
	Domain        string      `yaml:"domain"`
	CertResolver  string      `yaml:"cert_resolver"`
	Health        HealthCheck `yaml:"health"`
	Image         string      `yaml:"image"`
	Servers       []Server    `yaml:"servers"`
	ContainerName string      `yaml:"container_name" default:"app"`
	EnvFile       string      `yaml:"env_file" default:"./.env"`
	DockerFile    string      `yaml:"docker_file" default:"./Dockerfile"`
	DockerContext string      `yaml:"docker_context" default:"./"`
	Port          int         `yaml:"port" default:"3000"`
	Type          string      `yaml:"type" default:"app"`
	Networks      []string    `yaml:"networks" default:"traefik-public"`
}

type Server struct {
	Host    string   `yaml:"host"`
	User    string   `yaml:"user" default:"root"`
	Port    int      `yaml:"port" default:"22"`
	KeyFile string   `yaml:"key_file"`
	AppDir  string   `yaml:"app_dir"`
	Volumes []string `yaml:"volumes"`
}

type Volume struct {
	HostPath      string `yaml:"host_path"`
	ContainerPath string `yaml:"container_path"`
}

var Logger = prettyconsole.NewLogger(zap.DebugLevel)
