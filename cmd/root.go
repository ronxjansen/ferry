package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	prettyconsole "github.com/thessem/zap-prettyconsole"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"

	ferry "github.com/ronxjansen/ferry/internal"
)

var configFilePath string
var dockerFilePath string
var dockerContext string
var envFilePath string
var domain string
var certResolver string
var appName string
var imageName string

var logger = prettyconsole.NewLogger(zap.DebugLevel)

var rootCmd = &cobra.Command{
	Use:   "ferry",
	Short: "CLI to self-host all your apps on a sinlge VPS without vendor locking",
	Long:  `With Ferry you can deploy any number of applications to a single VPS, connect multiple domains and much more.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Render the cobra default help menu
		cmd.Help()
	},
}

func buildConfig() ferry.Config {
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		logger.Fatal("Failed to read config file", zap.Error(err))
	}

	var config ferry.Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		logger.Fatal("Failed to parse config", zap.Error(err))
	}

	// Merge file config with command-line arguments
	if dockerFilePath != "" {
		config.DockerFile = dockerFilePath
	}
	if dockerContext != "" {
		config.DockerContext = dockerContext
	}
	if envFilePath != "" {
		config.EnvFile = envFilePath
	}
	if appName != "" {
		config.ContainerName = appName
	}
	if imageName != "" {
		config.Image = imageName
	}
	if domain != "" {
		config.Domain = domain
	}
	if certResolver != "" {
		config.CertResolver = certResolver
	}

	// Integer defaults are not correctly set by yaml unmarshal
	if config.Port == 0 {
		config.Port = 8080
	}
	if config.HealthCheck.SuccessStatusCode == 0 {
		config.HealthCheck.SuccessStatusCode = 200
	}
	for i, server := range config.Servers {
		if server.Port == 0 {
			config.Servers[i].Port = 22
		}
		if server.AppDir == "" {
			config.Servers[i].AppDir = fmt.Sprintf("$HOME/%s", config.ContainerName)
		}
	}

	return config
}

func Execute() {
	rootCmd.PersistentFlags().StringVarP(&configFilePath, "config", "c", "./ferry.yaml", "Path to your ferry.yaml config file")
	rootCmd.PersistentFlags().StringVarP(&dockerFilePath, "docker-file", "f", "./Dockerfile", "Path to your Dockerfile")
	rootCmd.PersistentFlags().StringVarP(&dockerContext, "docker-context", "x", "./", "Path to the context of your Dockerfile")
	rootCmd.PersistentFlags().StringVarP(&envFilePath, "env-file", "e", "./.env.production", "Path to your environment variables file")
	rootCmd.PersistentFlags().StringVarP(&imageName, "image", "i", "", "Docker image to use for your application")
	rootCmd.PersistentFlags().StringVarP(&domain, "domain", "d", "", "Domain to use for your application")
	rootCmd.PersistentFlags().StringVarP(&certResolver, "cert-resolver", "r", "", "Cert resolver to use for your application")
	rootCmd.PersistentFlags().StringVarP(&appName, "app-name", "a", "", "Name of your application container")
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
