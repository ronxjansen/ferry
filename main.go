package main

import (
	"os"

	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type HealthCheck struct {
	Path              string `yaml:"path"`
	Interval          string `yaml:"interval"`
	Timeout           string `yaml:"timeout"`
	SuccessStatusCode int    `yaml:"success_status_code"`
}

type Config struct {
	Domain        string      `yaml:"domain"`
	CertResolver  string      `yaml:"cert_resolver"`
	HealthCheck   HealthCheck `yaml:"health_check"`
	Image         string      `yaml:"image"`
	Servers       []Server    `yaml:"servers"`
	ContainerName string      `yaml:"container_name"`
	EnvFile       string      `yaml:"env_file"`
	Port          int         `yaml:"port"`
}

type Server struct {
	Host    string `yaml:"host"`
	User    string `yaml:"user"`
	Port    int    `yaml:"port"`
	KeyFile string `yaml:"key_file"`
	AppDir  string `yaml:"app_dir"`
}

func loadConfig(file string) (*Config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func main() {
	config, err := loadConfig("config.yaml")
	if err != nil {
		logger.Fatal("Failed to load config", zap.Error(err))
	}

	manager := NewCommandManager(*config)

	err = manager.Run(setup)
	if err != nil {
		logger.Fatal("Failed to run commands", zap.Error(err))
	}
}

var setup = []Role{
	&InstallDockerRole{},
	&InstallSopsRole{},
}

var deploy = []Role{
	&BootstrapAppDirRole{},
	&SyncEnvVarsRole{},
	&PullDockerImageRole{},
	&StopTraefikServiceRole{},
	&DeployTraefikServiceRole{},
}
